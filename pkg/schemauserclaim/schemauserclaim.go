package schemauserclaim

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/go-logr/logr"
	_ "github.com/lib/pq"
	"github.com/spf13/viper"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	v1 "github.com/infobloxopen/db-controller/api/v1"
	basefun "github.com/infobloxopen/db-controller/pkg/basefunctions"
	"github.com/infobloxopen/db-controller/pkg/dbclient"
	"github.com/infobloxopen/db-controller/pkg/dbuser"
	"github.com/infobloxopen/db-controller/pkg/metrics"
	"github.com/infobloxopen/db-controller/pkg/rdsauth"
	"k8s.io/apimachinery/pkg/types"

	// FIXME: upgrade kubebuilder so this package will be removed
	"k8s.io/apimachinery/pkg/api/errors"
)

type SchemaUserConfig struct {
	Viper                 *viper.Viper
	MasterAuth            *rdsauth.MasterAuth
	DbIdentifierPrefix    string
	Class                 string
	MetricsDepYamlPath    string
	MetricsConfigYamlPath string
}

// SchemaUserReconciler reconciles a DatabaseClaim object
type SchemaUserClaimReconciler struct {
	client.Client
	Config *SchemaUserConfig
}

func Reconcile(r *SchemaUserClaimReconciler, ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return r.Reconcile(ctx, req)
}

func (r *SchemaUserClaimReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx).WithValues("UserSchemaClaim", req.NamespacedName)

	//fetch schemauserclaim
	var schemaUserClaim v1.SchemaUserClaim
	if err := r.Get(ctx, req.NamespacedName, &schemaUserClaim); err != nil {
		log.Error(err, "unable to fetch SchemaUser")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	//check if class is allowed
	if permitted := basefun.IsClassPermitted(r.Config.Class, *schemaUserClaim.Spec.Class); !permitted {
		log.Info("ignoring this claim as this controller does not own this class", "claimClass", *schemaUserClaim.Spec.Class, "controllerClass", r.Config.Class)
		return ctrl.Result{}, nil
	}

	//basic validation
	if len(schemaUserClaim.Spec.Schemas) == 0 {
		log.Info("At least one schema has to be provided.")
		return ctrl.Result{}, nil
	}

	// get dbclaim
	var dbClaim v1.DatabaseClaim

	if err := r.Get(ctx, types.NamespacedName{Namespace: schemaUserClaim.Spec.SourceDatabaseClaim.Namespace, Name: schemaUserClaim.Spec.SourceDatabaseClaim.Name}, &dbClaim); err != nil {
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
	dbClient, err := basefun.GetClientForExistingDB(existingDBConnInfo, &log)
	if err != nil {
		log.Error(err, "creating database client error.")
		return ctrl.Result{}, err
	}
	defer dbClient.Close()

	if schemaUserClaim.Status.Schemas == nil {
		schemaUserClaim.Status.Schemas = []v1.SchemaStatus{}
	}

	for schemaName, schema := range schemaUserClaim.Spec.Schemas {
		schemaName = strings.ToLower(schemaName)

		//if schema doesn't exist, create it
		err := createSchema(dbClient, schemaName, log)
		if err != nil {
			return ctrl.Result{}, err
		}

		//add schema to the status section
		currentSchemaStatus := getOrCreateSchemaStatus(&schemaUserClaim, schemaName)

		//create user and assign role
		for _, user := range schema {
			roleName := strings.ToLower(schemaName + "_" + string(user.Permission))
			// check if role exists, if not: create it
			if err = createRole(roleName, dbClient, &log, existingDBConnInfo.DatabaseName, schemaName); err != nil {
				return ctrl.Result{}, err
			}

			dbu := dbuser.NewDBUser(user.UserName)
			rotationTime := r.getPasswordRotationTime()

			//add user to the status section
			currentUserStatus := getOrCreateUserStatus(currentSchemaStatus, user.UserName)

			//rotate user password if it's older than 'rotationTime'
			if currentUserStatus.UserUpdatedAt == nil || time.Since(currentUserStatus.UserUpdatedAt.Time) > rotationTime {
				log.Info("rotating user " + currentUserStatus.UserName)

				userPassword, err := basefun.GeneratePassword(r.Config.Viper)
				if err != nil {
					return ctrl.Result{}, err
				}

				nextUser := dbu.NextUser(currentUserStatus.UserName)
				currentUserStatus.UserName = nextUser
				created, err := dbClient.CreateUser(nextUser, roleName, userPassword)
				if err != nil {
					metrics.PasswordRotatedErrors.WithLabelValues("create error").Inc()
					return ctrl.Result{}, err
				}

				if !created {
					if err := dbClient.UpdatePassword(nextUser, userPassword); err != nil {
						return ctrl.Result{}, err
					}
					currentUserStatus.UserStatus = "password rotated"
				} else {
					currentUserStatus.UserStatus = "created"
				}

				timeNow := metav1.Now()
				currentUserStatus.UserUpdatedAt = &timeNow
			}
		}

		//update obj in k8s
		if err = r.updateClientStatus(ctx, &schemaUserClaim); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func createSchema(dbClient dbclient.Client, schemaName string, log logr.Logger) error {
	schemaExists, err := dbClient.SchemaExists(schemaName)
	if err != nil {
		log.Error(err, "checking if schema ["+schemaName+"] exists error.")
		return err
	}
	if !schemaExists {
		createSchema, err := dbClient.CreateSchema(schemaName)
		if err != nil || !createSchema {
			log.Error(err, "creating schema ["+schemaName+"] error.")
			return err
		}
	}
	return nil
}

func createRole(roleName string, dbClient dbclient.Client, log *logr.Logger, databaseName string, schemaName string) error {
	roleExists, err := dbClient.RoleExists(roleName)
	if err != nil {
		log.Error(err, "checking if role ["+roleName+"] exists error.")
		return err
	}
	if !roleExists {
		var err error = nil
		if strings.HasSuffix(roleName, strings.ToLower(string(v1.Admin))) {
			_, err = dbClient.CreateAdminRole(strings.ToLower(databaseName), strings.ToLower(roleName), strings.ToLower(schemaName))

		} else if strings.HasSuffix(roleName, strings.ToLower(string(v1.Regular))) {
			_, err = dbClient.CreateRegularRole(strings.ToLower(databaseName), strings.ToLower(roleName), strings.ToLower(schemaName))

		} else if strings.HasSuffix(roleName, strings.ToLower(string(v1.ReadOnly))) {
			_, err = dbClient.CreateReadOnlyRole(strings.ToLower(databaseName), strings.ToLower(roleName), strings.ToLower(schemaName))
		}

		if err != nil {
			log.Error(err, "creating role ["+roleName+"] error.")
			return err
		}

	}
	return nil
}

func getOrCreateSchemaStatus(schemaUserClaim *v1.SchemaUserClaim, schemaName string) *v1.SchemaStatus {
	schemaIdx := slices.IndexFunc(schemaUserClaim.Status.Schemas, func(c v1.SchemaStatus) bool {
		return c.Name == schemaName
	})
	if schemaIdx == -1 {
		aux := &v1.SchemaStatus{
			Name:        schemaName,
			Status:      "created",
			UsersStatus: []v1.UserStatusType{},
		}
		schemaUserClaim.Status.Schemas = append(schemaUserClaim.Status.Schemas, *aux)
		schemaIdx = slices.IndexFunc(schemaUserClaim.Status.Schemas, func(c v1.SchemaStatus) bool {
			return c.Name == schemaName
		})
	}
	return &schemaUserClaim.Status.Schemas[schemaIdx]
}

func getOrCreateUserStatus(currentSchemaStatus *v1.SchemaStatus, userName string) *v1.UserStatusType {
	userIdx := slices.IndexFunc(currentSchemaStatus.UsersStatus, func(c v1.UserStatusType) bool {
		return c.UserName == userName+dbuser.SuffixA || c.UserName == userName+dbuser.SuffixB
	})
	if userIdx == -1 {
		aux := &v1.UserStatusType{
			UserName:   userName,
			UserStatus: "created",
		}
		currentSchemaStatus.UsersStatus = append(currentSchemaStatus.UsersStatus, *aux)
		userIdx = slices.IndexFunc(currentSchemaStatus.UsersStatus, func(c v1.UserStatusType) bool {
			return c.UserName == userName
		})
	}
	return &currentSchemaStatus.UsersStatus[userIdx]
}

func (r *SchemaUserClaimReconciler) getMasterPassword(ctx context.Context, dbClaim *v1.DatabaseClaim) (string, error) {
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
	prt := time.Duration(r.Config.Viper.GetInt("passwordconfig::passwordRotationPeriod")) * time.Minute

	if prt < basefun.GetMinRotationTime() || prt > basefun.GetMaxRotationTime() {
		log.Log.Info("password rotation time is out of range, should be between 60 and 1440 min, use the default")
		return basefun.GetMinRotationTime()
	}

	return prt
}

func (r *SchemaUserClaimReconciler) updateClientStatus(ctx context.Context, schemaUserClaim *v1.SchemaUserClaim) error {

	err := r.Client.Status().Update(ctx, schemaUserClaim)
	if err != nil {
		// Ignore conflicts, resource might just be outdated.
		if errors.IsConflict(err) {
			return nil
		}
		return err
	}
	return nil
}
