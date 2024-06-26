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
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
	"github.com/spf13/viper"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type SchemaUserClaimReconciler struct {
	client.Client
	Class    string
	Config   *viper.Viper
	Log      logr.Logger
	Scheme   *runtime.Scheme
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

	//parse connstring to database
	existingDBConnInfo, err := persistancev1.ParseUri(schemaUserClaim.Spec.Database.DSN)
	if err != nil {
		return ctrl.Result{}, err
	}

	//retrieve dB master password
	masterPassword, err := r.getMasterPasswordForExistingDB(ctx, &schemaUserClaim)
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
		//check if role exists, if not: create it
		roleExists, err := dbClient.RoleExists(schema.Name)
		if err != nil {
			log.Error(err, "checking if role ["+schema.Name+"] exists error.")
			return ctrl.Result{}, err
		}
		if !roleExists {
			roleCreated, err := dbClient.CreateRole(existingDBConnInfo.DatabaseName, schema.Name, schema.Name)
			//TODO: "|| !roleCreated" might be a problem if the role exists already
			if err != nil || !roleCreated {
				log.Error(err, "creating schema ["+schema.Name+"] error.")
				return ctrl.Result{}, err
			}

		}
		//create user and assign role
		for _, user := range schema.Users {
			userPassword, err := GeneratePassword(r.Config)
			if err != nil {
				return ctrl.Result{}, err
			}

			fmt.Printf("User: %s Password: %s", strings.ToLower(user.UserName), userPassword)

			userCreated, err := dbClient.CreateUser(strings.ToLower(user.UserName), strings.ToLower(schema.Name), userPassword) //second param is the ROLE name

			//TODO: "|| !userCreated" might be a problem if the user exists already
			if err != nil || !userCreated {
				log.Error(err, "creating user ["+user.UserName+"].")
				return ctrl.Result{}, err
			}
		}
	}

	return ctrl.Result{}, nil
}

// TODO: check if it can be transfered to a UTILS class
func (r *SchemaUserClaimReconciler) getMasterPasswordForExistingDB(ctx context.Context, suClaim *persistancev1.SchemaUserClaim) (string, error) {
	secretKey := "password"
	gs := &corev1.Secret{}

	ns := suClaim.Spec.Database.SecretRef.Namespace
	if ns == "" {
		ns = suClaim.Namespace
	}
	err := r.Client.Get(ctx, client.ObjectKey{
		Namespace: ns,
		Name:      suClaim.Spec.Database.SecretRef.Name,
	}, gs)

	if err != nil {
		return "", err
	}

	password := string(gs.Data[secretKey])

	if password == "" {
		return "", fmt.Errorf("invalid credentials (password)")
	}
	return password, nil
}
