/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/go-logr/logr"
	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
	"github.com/infobloxopen/db-controller/pkg/dbclient"
	"github.com/infobloxopen/db-controller/pkg/dbuser"
	"github.com/infobloxopen/db-controller/pkg/metrics"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type SchemaUserClaimReconciler struct {
	client.Client
	BaseReconciler
	Recorder record.EventRecorder
}

//+kubebuilder:rbac:groups=persistance.atlas.infoblox.com,resources=schemauserclaims,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=persistance.atlas.infoblox.com,resources=schemauserclaims/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=persistance.atlas.infoblox.com,resources=schemauserclaims/finalizers,verbs=update
//+kubebuilder:rbac:groups=persistance.atlas.infoblox.com,resources=databaseclaims/finalizers,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch

func (r *SchemaUserClaimReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("UserSchemaClaim", req.NamespacedName).V(InfoLevel)

	//fetch schemauserclaim
	var schemaUserClaim persistancev1.SchemaUserClaim
	if err := r.Get(ctx, req.NamespacedName, &schemaUserClaim); err != nil {
		log.Error(err, "unable to fetch SchemaUser")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	//check if class is allowed
	if permitted := IsClassPermitted(r.Class, *schemaUserClaim.Spec.Class); !permitted {
		log.Info("ignoring this claim as this controller does not own this class", "claimClass", *schemaUserClaim.Spec.Class, "controllerClass", r.Class)
		return ctrl.Result{}, nil
	}

	//basic validation
	if len(schemaUserClaim.Spec.Schemas) == 0 {
		log.Info("At least one schema has to be provided.")
		return ctrl.Result{}, nil
	}

	// get dbclaim
	var dbClaim persistancev1.DatabaseClaim

	if err := r.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: schemaUserClaim.Spec.DBClaimName}, &dbClaim); err != nil {
		if client.IgnoreNotFound(err) != nil {
			log.Error(err, "unable to fetch DatabaseClaim")
			return ctrl.Result{}, err
		} else {
			log.Info("DatabaseClaim not found. Ignoring since object might have been deleted")
			return ctrl.Result{}, nil
		}
	}

	//get db conn details
	existingDBConnInfo := dbClaim.Status.ActiveDB.ConnectionInfo

	//retrieve dB master password
	masterPassword, err := r.getMasterPassword(ctx, &dbClaim)
	if err != nil {
		log.Error(err, "get master password for existing db error.")
		return ctrl.Result{}, err
	}
	existingDBConnInfo.Password = masterPassword

	//get client to DB
	dbClient, err := GetClientForExistingDB(existingDBConnInfo, &r.Log)
	if err != nil {
		log.Error(err, "creating database client error.")
		return ctrl.Result{}, err
	}
	defer dbClient.Close()

	if schemaUserClaim.Status.Schemas == nil {
		schemaUserClaim.Status.Schemas = []persistancev1.SchemaStatus{}
	}

	for _, schema := range schemaUserClaim.Spec.Schemas {
		schema.Name = strings.ToLower(schema.Name)

		//if schema doesn't exist, create it
		schemaExists, err := dbClient.SchemaExists(schema.Name)
		if err != nil {
			log.Error(err, "checking if schema ["+schema.Name+"] exists error.")
			return ctrl.Result{}, err
		}
		if !schemaExists {
			createSchema, err := dbClient.CreateSchema(schema.Name)
			if err != nil || !createSchema {
				log.Error(err, "creating schema ["+schema.Name+"] error.")
				return ctrl.Result{}, err
			}
		}
		//create user and assign role
		for _, user := range schema.Users {
			roleName := strings.ToLower(user.UserName + "_" + string(user.Permission))
			// check if role exists, if not: create it
			err := createRole(roleName, dbClient, &log, existingDBConnInfo.DatabaseName, schema.Name)
			if err != nil {
				return ctrl.Result{}, err
			}

			dbu := dbuser.NewDBUser(user.UserName)
			rotationTime := r.getPasswordRotationTime()

			//add schema to the status section
			currentSchemaStatus := getOrCreateSchemaStatus(schemaUserClaim, schema.Name)

			//add user to the status section
			currentUserStatus := getOrCreateUserStatus(*currentSchemaStatus, user.UserName)

			//rotate user password if it's older than 'rotationTime'
			if currentUserStatus.UserUpdatedAt == nil || time.Since(currentUserStatus.UserUpdatedAt.Time) > rotationTime {
				log.Info("rotating user " + currentUserStatus.UserName)

				userPassword, err := GeneratePassword(r.Config)
				if err != nil {
					return ctrl.Result{}, err
				}

				nextUser := dbu.NextUser(currentUserStatus.UserName)
				created, err := dbClient.CreateUser(nextUser, roleName, userPassword)
				if err != nil {
					metrics.PasswordRotatedErrors.WithLabelValues("create error").Inc()
					return ctrl.Result{}, err
				}

				if !created {
					if err := dbClient.UpdatePassword(nextUser, userPassword); err != nil {
						return ctrl.Result{}, err
					}
				}

				currentUserStatus.UserStatus = "password rotated"
				timeNow := metav1.Now()
				currentUserStatus.UserUpdatedAt = &timeNow
			}
		}

		//update obj in k8s
		//TODO: uncomment before check in
		// if err = r.updateClientStatus(ctx, &schemaUserClaim); err != nil {
		// 	return ctrl.Result{}, err
		// }
	}

	return ctrl.Result{}, nil
}

func createRole(roleName string, dbClient dbclient.Client, log *logr.Logger, databaseName string, schemaName string) error {
	roleExists, err := dbClient.RoleExists(roleName)
	if err != nil {
		log.Error(err, "checking if role ["+roleName+"] exists error.")
		return err
	}
	if !roleExists {
		_, err := dbClient.CreateRole(strings.ToLower(databaseName), strings.ToLower(roleName), strings.ToLower(schemaName))
		if err != nil {
			log.Error(err, "creating role ["+roleName+"] error.")
			return err
		}

	}
	return nil
}

func getOrCreateSchemaStatus(schemaUserClaim persistancev1.SchemaUserClaim, schemaName string) *persistancev1.SchemaStatus {
	schemaIdx := slices.IndexFunc(schemaUserClaim.Status.Schemas, func(c persistancev1.SchemaStatus) bool {
		return c.Name == schemaName
	})
	if schemaIdx == -1 {
		aux := &persistancev1.SchemaStatus{
			Name:        schemaName,
			Status:      "created",
			UsersStatus: []persistancev1.UserStatusType{},
		}
		schemaUserClaim.Status.Schemas = append(schemaUserClaim.Status.Schemas, *aux)
		schemaIdx = slices.IndexFunc(schemaUserClaim.Status.Schemas, func(c persistancev1.SchemaStatus) bool {
			return c.Name == schemaName
		})
	}
	return &schemaUserClaim.Status.Schemas[schemaIdx]
}

func getOrCreateUserStatus(currentSchemaStatus persistancev1.SchemaStatus, userName string) *persistancev1.UserStatusType {
	userIdx := slices.IndexFunc(currentSchemaStatus.UsersStatus, func(c persistancev1.UserStatusType) bool {
		return c.UserName == userName
	})
	if userIdx == -1 {
		aux := &persistancev1.UserStatusType{
			UserName:   userName,
			UserStatus: "created",
		}
		currentSchemaStatus.UsersStatus = append(currentSchemaStatus.UsersStatus, *aux)
		userIdx = slices.IndexFunc(currentSchemaStatus.UsersStatus, func(c persistancev1.UserStatusType) bool {
			return c.UserName == userName
		})
	}
	return &currentSchemaStatus.UsersStatus[userIdx]
}

func (r *SchemaUserClaimReconciler) getMasterPassword(ctx context.Context, dbClaim *persistancev1.DatabaseClaim) (string, error) {
	secretKey := "password"
	gs := &corev1.Secret{}

	ns := dbClaim.Namespace
	err := r.Client.Get(ctx, client.ObjectKey{
		Namespace: ns,
		Name:      dbClaim.Spec.SecretName,
	}, gs)
	if err == nil {
		return string(gs.Data[secretKey]), nil
	}
	if !errors.IsNotFound(err) {
		return "", err
	}

	return "", nil
}

func (r *SchemaUserClaimReconciler) getPasswordRotationTime() time.Duration {
	prt := time.Duration(r.Config.GetInt("passwordconfig::passwordRotationPeriod")) * time.Minute

	if prt < minRotationTime || prt > maxRotationTime {
		r.Log.Info("password rotation time is out of range, should be between 60 and 1440 min, use the default")
		return minRotationTime
	}

	return prt
}

func (r *SchemaUserClaimReconciler) updateClientStatus(ctx context.Context, dbClaim *persistancev1.SchemaUserClaim) error {

	err := r.Client.Status().Update(ctx, dbClaim)
	if err != nil {
		// Ignore conflicts, resource might just be outdated.
		if errors.IsConflict(err) {
			return nil
		}
		return err
	}
	return nil
}
