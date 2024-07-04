package roleclaim

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	_ "github.com/lib/pq"
	"github.com/spf13/viper"
	corev1 "k8s.io/api/core/v1"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	v1 "github.com/infobloxopen/db-controller/api/v1"
	basefun "github.com/infobloxopen/db-controller/pkg/basefunctions"
	"github.com/infobloxopen/db-controller/pkg/dbclient"
	"github.com/infobloxopen/db-controller/pkg/dbuser"
	"github.com/infobloxopen/db-controller/pkg/metrics"
	"github.com/infobloxopen/db-controller/pkg/rdsauth"

	"k8s.io/apimachinery/pkg/types"
)

type RoleConfig struct {
	Viper              *viper.Viper
	MasterAuth         *rdsauth.MasterAuth
	DbIdentifierPrefix string
	Class              string
}

const (
	dbClaimField  = ".spec.sourceDatabaseClaim.name"
	finalizerName = "dbroleclaims.persistance.atlas.infoblox.com/finalizer"
)

// RoleReconciler reconciles a DatabaseClaim object
type DbRoleClaimReconciler struct {
	client.Client
	Config *RoleConfig
}

func Reconcile(r *DbRoleClaimReconciler, ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	return r.Reconcile(ctx, req)
}

func (r *DbRoleClaimReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// FIXME: dont shadow log package
	log := log.FromContext(ctx).WithValues("databaserole", req.NamespacedName)
	var dbRoleClaim v1.DbRoleClaim
	if err := r.Get(ctx, req.NamespacedName, &dbRoleClaim); err != nil {
		log.Error(err, "unable to fetch DatabaseClaim")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if permitted := basefun.IsClassPermitted(r.Config.Class, *dbRoleClaim.Spec.Class); !permitted {
		log.Info("ignoring this claim as this controller does not own this class", "claimClass", *dbRoleClaim.Spec.Class, "controllerClas", r.Config.Class)
		return ctrl.Result{}, nil
	}

	isObjectDeleted, err := r.deleteWorkflow(ctx, &dbRoleClaim)
	if err != nil {
		log.Error(err, "error in delete workflow")
		return ctrl.Result{}, err
	}
	if isObjectDeleted {
		return ctrl.Result{}, nil
	}

	if dbRoleClaim.Spec.SourceDatabaseClaim.Name == "" {
		log.Error(fmt.Errorf("sourcedatabaseclaim cannot be nil"), "invalid_spec_source_database_claim_name")
		return r.manageError(ctx, &dbRoleClaim, fmt.Errorf("sourcedatabaseclaim cannot be nil"))
	}

	// #region find DBClaim: sourceDbClaim
	dbclaimName := dbRoleClaim.Spec.SourceDatabaseClaim.Name
	dbclaimNamespace := dbRoleClaim.Spec.SourceDatabaseClaim.Namespace
	sourceDbClaim := &v1.DatabaseClaim{}
	err = r.Get(ctx, types.NamespacedName{Name: dbclaimName, Namespace: dbclaimNamespace}, sourceDbClaim)
	if err != nil {
		log.Error(err, "specified dbclaim not found", "dbclaimname", dbclaimName, "dbclaimNamespace", dbclaimNamespace)
		//r.Config.Recorder.Event(&dbRoleClaim, "Warning", "Not found", fmt.Sprintf("DatabaseClaim %s/%s", dbclaimNamespace, dbclaimName))
		dbRoleClaim.Status.MatchedSourceClaim = ""
		return r.manageError(ctx, &dbRoleClaim, fmt.Errorf("%s dbclaim not found", dbclaimName))
	}
	log.Info("found dbclaim", "secretName", sourceDbClaim.Spec.SecretName)

	dbRoleClaim.Status.MatchedSourceClaim = sourceDbClaim.Namespace + "/" + sourceDbClaim.Name
	//r.Config.Recorder.Event(&dbRoleClaim, "Normal", "Found", fmt.Sprintf("DatabaseClaim %s/%s", dbclaimNamespace, dbclaimName))
	// #endregion

	// #region find secret linked to DBClaim: sourceSecret
	sourceSecret := &corev1.Secret{}
	err = r.Get(ctx, types.NamespacedName{Name: sourceDbClaim.Spec.SecretName, Namespace: dbclaimNamespace}, sourceSecret)
	if err != nil {
		log.Error(err, "dbclaim_secret_not_found", "secret_name", sourceDbClaim.Spec.SecretName, "secret_namespace", dbclaimNamespace)
		//r.Config.Recorder.Event(&dbRoleClaim, "Warning", "Not found", fmt.Sprintf("Secret %s/%s", dbclaimNamespace, sourceDbClaim.Spec.SecretName))
		dbRoleClaim.Status.SourceSecret = ""
		return r.manageError(ctx, &dbRoleClaim, fmt.Errorf("%s source secret not found", sourceDbClaim.Spec.SecretName))
	}
	log.V(1).Info("dbclaim_secret", "secret_name", sourceDbClaim.Spec.SecretName, "secret_namespace", dbclaimNamespace)

	dbRoleClaim.Status.SourceSecret = sourceSecret.Namespace + "/" + sourceSecret.Name
	//r.Config.Recorder.Event(&dbRoleClaim, "Normal", "Found", fmt.Sprintf("Secret %s/%s", dbclaimNamespace, sourceDbClaim.Spec.SecretName))
	// #endregion find secret

	//either create users and schemas OR fallout to ELSE: copy existing secret
	if dbRoleClaim.Spec.UserName != "" || len(dbRoleClaim.Spec.SchemaRoleMap) > 0 {
		//validations for user, schema and roles
		var missingParam bool = false
		if dbRoleClaim.Spec.UserName == "" {
			missingParam = true
		}
		for schema, role := range dbRoleClaim.Spec.SchemaRoleMap {
			if schema == "" || role == "" {
				missingParam = true
				break
			}
		}
		if missingParam {
			log.Info("username, schema and role are mandatory when one of these fields are provided")
			return ctrl.Result{}, errors.New("username, schema and role are mandatory when one of these fields are provided")
		}

		//get db conn details
		existingDBConnInfo := sourceDbClaim.Status.ActiveDB.ConnectionInfo

		// retrieve dB master password
		existingDBConnInfo.Password = string(sourceSecret.Data["password"])

		// get client to DB
		dbClient, err := basefun.GetClientForExistingDB(existingDBConnInfo, &log)
		if err != nil {
			log.Error(err, "creating database client error.")
			return ctrl.Result{}, err
		}
		defer dbClient.Close()

		if dbRoleClaim.Status.SchemaRoleStatus.SchemaStatus == nil {
			dbRoleClaim.Status.SchemaRoleStatus.SchemaStatus = make(map[string]string)
		}
		if dbRoleClaim.Status.SchemaRoleStatus.RoleStatus == nil {
			dbRoleClaim.Status.SchemaRoleStatus.RoleStatus = make(map[string]string)
		}
		if dbRoleClaim.Status.Username == "" {
			dbRoleClaim.Status.Username = dbRoleClaim.Spec.UserName
		}

		for schemaName, role := range dbRoleClaim.Spec.SchemaRoleMap {
			schemaName = strings.ToLower(schemaName)

			//if schema doesn't exist, create it
			err := createSchema(dbClient, schemaName, log)
			if err != nil {
				return ctrl.Result{}, err
			}
			//update schema status
			dbRoleClaim.Status.SchemaRoleStatus.SchemaStatus[schemaName] = "valid"

			//create user and assign role
			roleName := strings.ToLower(schemaName + "_" + strings.ToLower(string(role)))
			// check if role exists, if not: create it
			if err = createRole(roleName, dbClient, &log, existingDBConnInfo.DatabaseName, schemaName); err != nil {
				return ctrl.Result{}, err
			}
			//update role status
			dbRoleClaim.Status.SchemaRoleStatus.RoleStatus[roleName] = "valid"

			dbu := dbuser.NewDBUser(dbRoleClaim.Spec.UserName)
			rotationTime := r.getPasswordRotationTime()

			//create users if it's first time processing OR rotate user & password if it's older than 'rotationTime'
			if dbRoleClaim.Status.SchemasRolesUpdatedAt == nil || time.Since(dbRoleClaim.Status.SchemasRolesUpdatedAt.Time) > rotationTime {
				log.Info("rotating users")

				userPassword, err := basefun.GeneratePassword(r.Config.Viper)
				if err != nil {
					return ctrl.Result{}, err
				}

				nextUser := dbu.NextUser(dbRoleClaim.Status.Username)
				dbRoleClaim.Status.Username = nextUser
				created, err := dbClient.CreateUser(nextUser, roleName, userPassword)
				if err != nil {
					metrics.PasswordRotatedErrors.WithLabelValues("create error").Inc()
					return ctrl.Result{}, err
				}

				if !created {
					//user exists already, so assign role to it
					if err := dbClient.AssignRoleToUser(nextUser, roleName); err != nil {
						metrics.UsersUpdated.Inc()
						return ctrl.Result{}, err
					}

					if err := dbClient.UpdatePassword(nextUser, userPassword); err != nil {
						return ctrl.Result{}, err
					}
				}
			}
		}
		timeNow := metav1.Now()
		dbRoleClaim.Status.SchemasRolesUpdatedAt = &timeNow

		//update obj in k8s
		if err = r.updateClientStatus(ctx, &dbRoleClaim); err != nil {
			return ctrl.Result{}, err
		}

		return r.manageSuccess(ctx, &dbRoleClaim)
	}
	//ELSE: username is not provided, so we just copy the current secret

	if sourceSecret.GetResourceVersion() == dbRoleClaim.Status.SourceSecretResourceVersion {
		log.Info("source secret has not changed, update not called",
			"sourceVersion", sourceSecret.GetResourceVersion(),
			"statusVersion", dbRoleClaim.Status.SourceSecretResourceVersion)
		return r.manageSuccess(ctx, &dbRoleClaim)
	}

	//copy source secret and change name
	if err = r.copySourceSecret(ctx, sourceSecret, &dbRoleClaim); err != nil {
		log.Error(err, "failed_copy_source_secret")
		//r.Config.Recorder.Event(&dbRoleClaim, "Warning", "Update Failed", fmt.Sprintf("Secret %s/%s", dbRoleClaim.Namespace, dbRoleClaim.Spec.SecretName))
		return r.manageError(ctx, &dbRoleClaim, err)
	}

	dbRoleClaim.Status.SourceSecretResourceVersion = sourceSecret.GetResourceVersion()
	timeNow := metav1.Now()
	dbRoleClaim.Status.SecretUpdatedAt = &timeNow
	//r.Config.Recorder.Event(&dbRoleClaim, "Normal", "Updated", fmt.Sprintf("Secret %s/%s", dbRoleClaim.Namespace, dbRoleClaim.Spec.SecretName))

	return r.manageSuccess(ctx, &dbRoleClaim)

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

// func getOrCreateSchemaStatus(schemaUserClaim *v1.DbRoleClaim, schemaName string) *v1.SchemaStatus {
// 	schemaIdx := slices.IndexFunc(schemaUserClaim.Status.Schemas, func(c v1.SchemaStatus) bool {
// 		return c.Name == schemaName
// 	})
// 	if schemaIdx == -1 {
// 		aux := &v1.SchemaStatus{
// 			Name:        schemaName,
// 			Status:      "created",
// 			UsersStatus: []v1.UserStatusType{},
// 		}
// 		schemaUserClaim.Status.Schemas = append(schemaUserClaim.Status.Schemas, *aux)
// 		schemaIdx = slices.IndexFunc(schemaUserClaim.Status.Schemas, func(c v1.SchemaStatus) bool {
// 			return c.Name == schemaName
// 		})
// 	}
// 	return &schemaUserClaim.Status.Schemas[schemaIdx]
// }

// func getOrCreateUserStatus(currentSchemaStatus *v1.SchemaStatus, userName string) *v1.UserStatusType {
// 	userIdx := slices.IndexFunc(currentSchemaStatus.UsersStatus, func(c v1.UserStatusType) bool {
// 		return c.UserName == userName+dbuser.SuffixA || c.UserName == userName+dbuser.SuffixB
// 	})
// 	if userIdx == -1 {
// 		aux := &v1.UserStatusType{
// 			UserName:   userName,
// 			UserStatus: "created",
// 		}
// 		currentSchemaStatus.UsersStatus = append(currentSchemaStatus.UsersStatus, *aux)
// 		userIdx = slices.IndexFunc(currentSchemaStatus.UsersStatus, func(c v1.UserStatusType) bool {
// 			return c.UserName == userName
// 		})
// 	}
// 	return &currentSchemaStatus.UsersStatus[userIdx]
// }

// func (r *DbRoleClaimReconciler) getMasterPassword(ctx context.Context, dbClaim *v1.DatabaseClaim) (string, error) {
// 	secretKey := "password"
// 	gs := &corev1.Secret{}

// 	ns := dbClaim.Namespace
// 	err := r.Client.Get(ctx, client.ObjectKey{
// 		Namespace: ns,
// 		Name:      dbClaim.Spec.SecretName,
// 	}, gs)
// 	if err == nil {
// 		return string(gs.Data[secretKey]), nil
// 	}
// 	if !errors.IsNotFound(err) {
// 		return "", err
// 	}

// 	return "", nil
// }

func (r *DbRoleClaimReconciler) getPasswordRotationTime() time.Duration {
	prt := time.Duration(r.Config.Viper.GetInt("passwordconfig::passwordRotationPeriod")) * time.Minute

	if prt < basefun.GetMinRotationTime() || prt > basefun.GetMaxRotationTime() {
		log.Log.Info("password rotation time is out of range, should be between 60 and 1440 min, use the default")
		return basefun.GetMinRotationTime()
	}

	return prt
}

func (r *DbRoleClaimReconciler) updateClientStatus(ctx context.Context, schemaUserClaim *v1.DbRoleClaim) error {

	err := r.Client.Status().Update(ctx, schemaUserClaim)
	if err != nil {
		// Ignore conflicts, resource might just be outdated.
		if apierrors.IsConflict(err) {
			return nil
		}
		return err
	}
	return nil
}

func (r *DbRoleClaimReconciler) deleteWorkflow(ctx context.Context, dbRoleClaim *v1.DbRoleClaim) (bool, error) {

	// examine DeletionTimestamp to determine if object is under deletion
	if dbRoleClaim.ObjectMeta.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(dbRoleClaim, finalizerName) {
			controllerutil.AddFinalizer(dbRoleClaim, finalizerName)
			if err := r.Update(ctx, dbRoleClaim); err != nil {
				return false, err
			}
		}
		return false, nil
	}
	// The object is being deleted
	if controllerutil.ContainsFinalizer(dbRoleClaim, finalizerName) {
		// remove our finalizer from the list and update it.
		controllerutil.RemoveFinalizer(dbRoleClaim, finalizerName)
		if err := r.Update(ctx, dbRoleClaim); err != nil {
			return false, err
		}
	}
	// Stop reconciliation as the item is being deleted
	return true, nil
}

func (r *DbRoleClaimReconciler) manageError(ctx context.Context, dbRoleClaim *v1.DbRoleClaim, inErr error) (ctrl.Result, error) {

	if dbRoleClaim == nil {
		return ctrl.Result{}, fmt.Errorf("dbroleclaim_is_nil: %w", inErr)
	}

	dbRoleClaim.Status.Error = inErr.Error()

	err := r.Client.Status().Update(ctx, dbRoleClaim)
	if err != nil {
		// Ignore conflicts, resource might just be outdated.
		if apierrors.IsConflict(err) {
			err = nil
		}
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, inErr
}

func (r *DbRoleClaimReconciler) manageSuccess(ctx context.Context, dbRoleClaim *v1.DbRoleClaim) (ctrl.Result, error) {

	if dbRoleClaim == nil {
		return ctrl.Result{}, fmt.Errorf("dbroleclaim_is_nil")
	}

	dbRoleClaim.Status.Error = ""

	err := r.Client.Status().Update(ctx, dbRoleClaim)
	if err != nil {
		// Ignore conflicts, resource might just be outdated.
		if apierrors.IsConflict(err) {
			err = nil
		}
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *DbRoleClaimReconciler) copySourceSecret(ctx context.Context, sourceSecret *corev1.Secret, dbRoleClaim *v1.DbRoleClaim) error {
	log := log.FromContext(ctx).WithValues("databaserole", "copySourceSecret")

	secretName := dbRoleClaim.Spec.SecretName
	sourceSecretData := sourceSecret.Data
	role_secret := &corev1.Secret{}

	//find SECRET
	err := r.Client.Get(ctx, client.ObjectKey{
		Namespace: dbRoleClaim.Namespace,
		Name:      secretName,
	}, role_secret)
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return err
		}
		role_secret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: dbRoleClaim.Namespace,
				Name:      secretName,
				Labels:    map[string]string{"app.kubernetes.io/managed-by": "dbrole-controller"},
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion:         "persistance.atlas.infoblox.com/v1",
						Kind:               "DbRoleClaim",
						Name:               dbRoleClaim.Name,
						UID:                dbRoleClaim.UID,
						Controller:         ptr.To(true),
						BlockOwnerDeletion: ptr.To(true),
					},
				},
			},
			Data: sourceSecretData,
		}
		log.Info("creating secret", "secret", secretName, "namespace", dbRoleClaim.Namespace)
		return r.Client.Create(ctx, role_secret)
	}

	role_secret.Data = sourceSecretData
	log.Info("updating secret", "secret", secretName, "namespace", dbRoleClaim.Namespace)
	return r.Client.Update(ctx, role_secret)

}
