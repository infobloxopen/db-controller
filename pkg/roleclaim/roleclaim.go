package roleclaim

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
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
	timeNow := metav1.Now()

	var dbRoleClaim v1.DbRoleClaim
	if err := r.Get(ctx, req.NamespacedName, &dbRoleClaim); err != nil {
		if client.IgnoreNotFound(err) != nil {
			log.Error(err, "unable to fetch DatabaseRoleClaim")
			return ctrl.Result{}, err
		} else {
			log.Error(err, "databaseRoleClaim not found")
			return ctrl.Result{}, nil
		}
	}

	if permitted := basefun.IsClassPermitted(r.Config.Class, *dbRoleClaim.Spec.Class); !permitted {
		log.Info("ignoring this claim as this controller does not own this class", "claimClass", *dbRoleClaim.Spec.Class, "controllerClass", r.Config.Class)
		return ctrl.Result{}, nil
	}

	isObjectDeleted, err := r.deleteWorkflow(ctx, &dbRoleClaim, &log)
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
	sourceDbClaim, err := r.getSourceDBClaim(ctx, &dbRoleClaim, &log)
	if err != nil {
		return r.manageError(ctx, &dbRoleClaim, fmt.Errorf("%s dbclaim not found", dbRoleClaim.Spec.SourceDatabaseClaim.Name))
	}
	// #endregion

	// #region find secret linked to DBClaim: sourceSecret
	sourceSecret, err := r.getSourceSecret(ctx, sourceDbClaim.Spec.SecretName, &dbRoleClaim, &log)
	if err != nil {
		return r.manageError(ctx, &dbRoleClaim, fmt.Errorf("%s source secret not found", sourceDbClaim.Spec.SecretName))
	}
	// #endregion find secret

	//either create users and schemas OR fallout to ELSE: copy existing secret
	if len(dbRoleClaim.Spec.SchemaRoleMap) > 0 {
		//validations for user, schema and roles
		var missingParam bool = false
		for schema, role := range dbRoleClaim.Spec.SchemaRoleMap {
			if schema == "" || role == "" {
				missingParam = true
				break
			}
		}
		if missingParam {
			log.Info("schema and role are mandatory when one of these fields are provided")
			return ctrl.Result{}, errors.New("schema and role are mandatory when one of these fields are provided")
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

		initStatusValues(&dbRoleClaim)

		rotationTime := r.getPasswordRotationTime()

		//create users if it's first time processing OR rotate user & password if it's older than 'rotationTime'
		if dbRoleClaim.Status.SchemasRolesUpdatedAt == nil || time.Since(dbRoleClaim.Status.SchemasRolesUpdatedAt.Time) > rotationTime {
			if dbRoleClaim.Status.SchemasRolesUpdatedAt == nil {
				log.Info("creating user, schemas and roles")
			} else {
				log.Info("rotating user")
			}

			//create schemas and roles
			for schemaName, role := range dbRoleClaim.Spec.SchemaRoleMap {
				schemaName = strings.ToLower(schemaName)

				//if schema doesn't exist, create it
				err := createSchema(dbClient, schemaName, log)
				if err != nil {
					return ctrl.Result{}, err
				}
				//update schema status
				dbRoleClaim.Status.SchemaRoleStatus.SchemaStatus[schemaName] = "valid"

				//create role
				roleName := strings.ToLower(schemaName + "_" + strings.ToLower(string(role)))
				// check if role exists, if not: create it
				if err = createRole(roleName, dbClient, &log, existingDBConnInfo.DatabaseName, schemaName); err != nil {
					return ctrl.Result{}, err
				}
				//update role status
				dbRoleClaim.Status.SchemaRoleStatus.RoleStatus[roleName] = "valid"

			}

			//create user
			dbu := dbuser.NewDBUser(strings.ToLower(dbRoleClaim.Name) + "_user")
			userPassword, err := basefun.GeneratePassword(r.Config.Viper)
			if err != nil {
				return ctrl.Result{}, err
			}

			nextUser := dbu.NextUser(dbRoleClaim.Status.Username)
			dbRoleClaim.Status.Username = nextUser
			created, err := dbClient.CreateUser(nextUser, "", userPassword) //role will be assigned later on
			if err != nil {
				metrics.PasswordRotatedErrors.WithLabelValues("create error").Inc()
				return ctrl.Result{}, err
			}

			//existing user, so update password
			if !created {
				if err := dbClient.UpdatePassword(nextUser, userPassword); err != nil {
					return ctrl.Result{}, err
				}
			}

			//assign user to schema and roles
			for schemaName, role := range dbRoleClaim.Spec.SchemaRoleMap {
				schemaName = strings.ToLower(schemaName)
				roleName := strings.ToLower(schemaName + "_" + strings.ToLower(string(role)))

				//user already exists, so assign role to it
				if err := dbClient.AssignRoleToUser(nextUser, roleName); err != nil {
					metrics.UsersUpdated.Inc()
					return ctrl.Result{}, err
				}
			}

			//copy source secret, change name, username and password
			if err = r.copySourceSecret(ctx, sourceSecret, &dbRoleClaim, dbRoleClaim.Status.Username, userPassword); err != nil {
				log.Error(err, "failure copying source secret")
				return r.manageError(ctx, &dbRoleClaim, err)
			}

			dbRoleClaim.Status.SecretUpdatedAt = &timeNow

			dbRoleClaim.Status.SchemasRolesUpdatedAt = &timeNow
		}

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
	if err = r.copySourceSecret(ctx, sourceSecret, &dbRoleClaim, "", ""); err != nil {
		log.Error(err, "failure copying source secret")
		return r.manageError(ctx, &dbRoleClaim, err)
	}

	dbRoleClaim.Status.SourceSecretResourceVersion = sourceSecret.GetResourceVersion()
	dbRoleClaim.Status.SecretUpdatedAt = &timeNow

	return r.manageSuccess(ctx, &dbRoleClaim)

}

func (r *DbRoleClaimReconciler) getSourceSecret(ctx context.Context, secretName string, dbRoleClaim *v1.DbRoleClaim, log *logr.Logger) (*corev1.Secret, error) {
	sourceSecret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Name: secretName, Namespace: dbRoleClaim.Spec.SourceDatabaseClaim.Namespace}, sourceSecret); err != nil {
		if client.IgnoreNotFound(err) != nil {
			log.Error(err, "unable to fetch secret", "secret_name", secretName, "secret namespace", dbRoleClaim.Spec.SourceDatabaseClaim.Namespace)
			return nil, err
		} else {
			log.Error(err, "dbClaim secret not found", "secret_name", secretName, "secret namespace", dbRoleClaim.Spec.SourceDatabaseClaim.Namespace)
			return nil, err
		}
	}
	log.V(1).Info("dbclaim secret", "secret name", secretName, "secret namespace", dbRoleClaim.Spec.SourceDatabaseClaim.Namespace)

	dbRoleClaim.Status.SourceSecret = sourceSecret.Namespace + "/" + sourceSecret.Name
	return sourceSecret, nil
}

func (r *DbRoleClaimReconciler) getSourceDBClaim(ctx context.Context, dbRoleClaim *v1.DbRoleClaim, log *logr.Logger) (*v1.DatabaseClaim, error) {
	dbclaimName := dbRoleClaim.Spec.SourceDatabaseClaim.Name
	dbclaimNamespace := dbRoleClaim.Spec.SourceDatabaseClaim.Namespace
	sourceDbClaim := &v1.DatabaseClaim{}
	if err := r.Get(ctx, types.NamespacedName{Name: dbclaimName, Namespace: dbclaimNamespace}, sourceDbClaim); err != nil {
		if client.IgnoreNotFound(err) != nil {
			log.Error(err, "unable to fetch DBClaim", "dbclaimname", dbclaimName, "dbclaimNamespace", dbclaimNamespace)
			return nil, err
		} else {
			log.Error(err, "specified dbclaim not found", "dbclaimname", dbclaimName, "dbclaimNamespace", dbclaimNamespace)
			return nil, err
		}
	}
	log.Info("found dbclaim", "secretName", sourceDbClaim.Spec.SecretName)

	dbRoleClaim.Status.MatchedSourceClaim = sourceDbClaim.Namespace + "/" + sourceDbClaim.Name
	return sourceDbClaim, nil
}

func initStatusValues(dbRoleClaim *v1.DbRoleClaim) {
	if dbRoleClaim.Status.SchemaRoleStatus.SchemaStatus == nil {
		dbRoleClaim.Status.SchemaRoleStatus.SchemaStatus = make(map[string]string)
	}
	if dbRoleClaim.Status.SchemaRoleStatus.RoleStatus == nil {
		dbRoleClaim.Status.SchemaRoleStatus.RoleStatus = make(map[string]string)
	}
	if dbRoleClaim.Status.Username == "" {
		dbRoleClaim.Status.Username = dbRoleClaim.Name
	}
}

func createSchema(dbClient dbclient.Clienter, schemaName string, log logr.Logger) error {
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

func createRole(roleName string, dbClient dbclient.Clienter, log *logr.Logger, databaseName string, schemaName string) error {
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

func (r *DbRoleClaimReconciler) deleteWorkflow(ctx context.Context, dbRoleClaim *v1.DbRoleClaim, log *logr.Logger) (bool, error) {

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
		// our finalizer is present, so lets handle any external dependency
		if err := r.deleteExternalResources(ctx, dbRoleClaim, log); err != nil {
			// if fail to delete the external dependency here, return with error
			// so that it can be retried
			return false, err
		}
		// remove our finalizer from the list and update it.
		controllerutil.RemoveFinalizer(dbRoleClaim, finalizerName)
		if err := r.Update(ctx, dbRoleClaim); err != nil {
			return false, err
		}
	}
	// Stop reconciliation as the item is being deleted
	return true, nil
}

func (r *DbRoleClaimReconciler) deleteExternalResources(ctx context.Context, dbRoleClaim *v1.DbRoleClaim, log *logr.Logger) error {
	// delete any external resources associated with the dbRoleClaim
	// #region find DBClaim: sourceDbClaim
	sourceDbClaim, err := r.getSourceDBClaim(ctx, dbRoleClaim, log)
	if err != nil {
		return err
	}
	// #endregion

	// #region find secret linked to DBClaim: sourceSecret
	sourceSecret, err := r.getSourceSecret(ctx, sourceDbClaim.Spec.SecretName, dbRoleClaim, log)
	if err != nil {
		return err
	}
	// #endregion find secret

	//get db conn details
	existingDBConnInfo := sourceDbClaim.Status.ActiveDB.ConnectionInfo

	// retrieve dB master password
	existingDBConnInfo.Password = string(sourceSecret.Data["password"])

	// get client to DB
	dbClient, err := basefun.GetClientForExistingDB(existingDBConnInfo, log)
	if err != nil {
		log.Error(err, "creating database client error.")
		return err
	}
	defer dbClient.Close()

	//delete users linked to this DBRoleClaim
	if err = dbClient.DeleteUser(dbRoleClaim.Name+"_user"+dbuser.SuffixA, existingDBConnInfo.Username); err != nil {
		log.Error(err, "droping user: "+dbRoleClaim.Name+"_user"+dbuser.SuffixA)
		return err
	}

	if err = dbClient.DeleteUser(dbRoleClaim.Name+"_user"+dbuser.SuffixB, existingDBConnInfo.Username); err != nil {
		log.Error(err, "droping user: "+dbRoleClaim.Name+"_user"+dbuser.SuffixB)
		return err
	}

	return nil
}

func (r *DbRoleClaimReconciler) manageError(ctx context.Context, dbRoleClaim *v1.DbRoleClaim, inErr error) (ctrl.Result, error) {

	if dbRoleClaim == nil {
		return ctrl.Result{}, fmt.Errorf("dbroleclaim is nil: %w", inErr)
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
		return ctrl.Result{}, fmt.Errorf("dbroleclaim is nil")
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

func (r *DbRoleClaimReconciler) copySourceSecret(ctx context.Context, sourceSecret *corev1.Secret, dbRoleClaim *v1.DbRoleClaim, newUser, newPassword string) error {
	log := log.FromContext(ctx).WithValues("databaserole", "copySourceSecret")

	secretName := dbRoleClaim.Spec.SecretName
	sourceSecretData := sourceSecret.Data

	if newUser != "" {
		sourceSecretData["username"] = []byte(newUser)
	}
	if newPassword != "" {
		sourceSecretData["password"] = []byte(newPassword)
	}

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
