package databaseclaim

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/go-logr/logr"
	_ "github.com/lib/pq"
	gopassword "github.com/sethvargo/go-password/password"
	"github.com/spf13/viper"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1 "github.com/infobloxopen/db-controller/api/v1"
	"github.com/infobloxopen/db-controller/pkg/auth"
	basefun "github.com/infobloxopen/db-controller/pkg/basefunctions"
	"github.com/infobloxopen/db-controller/pkg/dbclient"
	"github.com/infobloxopen/db-controller/pkg/dbuser"
	"github.com/infobloxopen/db-controller/pkg/hostparams"
	"github.com/infobloxopen/db-controller/pkg/kctlutils"
	"github.com/infobloxopen/db-controller/pkg/metrics"
	"github.com/infobloxopen/db-controller/pkg/pgctl"
	exporter "github.com/infobloxopen/db-controller/pkg/postgres-exporter"
	"github.com/infobloxopen/db-controller/pkg/rdsauth"

	"k8s.io/apimachinery/pkg/api/errors"
)

var (
	maxNameLen                      = 44 // max length of dbclaim name
	serviceNamespaceEnvVar          = "SERVICE_NAMESPACE"
	defaultRestoreFromSource        = "Snapshot"
	defaultBackupPolicyKey          = "Backup"
	tempTargetPassword              = "targetPassword"
	tempSourceDsn                   = "sourceDsn"
	tempTargetDsn                   = "targetDsn"
	cachedMasterPasswdForExistingDB = "cachedMasterPasswdForExistingDB"
	masterSecretSuffix              = "-master"
	masterPasswordKey               = "password"
	// debugLevel is used to set V level to 1 as suggested by official docs
	// https://github.com/kubernetes-sigs/controller-runtime/blob/main/TMP-LOGGING.md
	debugLevel = 1

	// FIXME: remove references to private variables
	OperationalStatusTagKey        string = "operational-status"
	OperationalStatusInactiveValue string = "inactive"
	OperationalStatusActiveValue   string = "active"
)

var (
	ErrMaxNameLen     = fmt.Errorf("dbclaim name is too long. max length is 44 characters")
	ErrInvalidDSNName = fmt.Errorf("dsn name must be: %s", v1.DSNKey)
)

// DatabaseClaimConfig is the configuration for the DatabaseClaimReconciler.
type DatabaseClaimConfig struct {
	Viper                 *viper.Viper
	MasterAuth            *rdsauth.MasterAuth
	Class                 string
	Namespace             string
	MetricsEnabled        bool
	MetricsDepYamlPath    string
	MetricsConfigYamlPath string
}

// DatabaseClaimReconciler reconciles a DatabaseClaim object
type DatabaseClaimReconciler struct {
	client.Client
	Config        *DatabaseClaimConfig
	kctl          *kctlutils.Client
	statusManager *StatusManager
}

// New returns a configured databaseclaim reconciler
func New(cli client.Client, cfg *DatabaseClaimConfig) *DatabaseClaimReconciler {
	return &DatabaseClaimReconciler{
		Client:        cli,
		Config:        cfg,
		kctl:          kctlutils.New(cli, cfg.Viper.GetString("SERVICE_NAMESPACE")),
		statusManager: NewStatusManager(cli, cfg.Viper),
	}
}

// isClassPermitted can not modify the claim class as it can
// cause k8s updates in cluster
func isClassPermitted(ctrlClass string, ptrClaimClass *string) bool {

	var claimClass string
	if ptrClaimClass == nil {
		claimClass = "default"
	} else {
		claimClass = *ptrClaimClass
	}

	return claimClass == ctrlClass
}

// validateDBClaim should validate deprecated and or unsupported values in a claim object
func validateDBClaim(dbClaim *v1.DatabaseClaim) error {
	// envtest will send default values as empty strings, provide an in-process
	// update to ensure downstream code works
	if dbClaim.Spec.DSNName == "" {
		dbClaim.Spec.DSNName = v1.DSNKey
	}
	if dbClaim.Spec.DSNName != v1.DSNKey {
		return ErrInvalidDSNName
	}
	return nil
}

// Reconcile is the main reconciliation function for the DatabaseClaimReconciler.
func (r *DatabaseClaimReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logr := log.FromContext(ctx)

	var dbClaim v1.DatabaseClaim
	if err := r.Get(ctx, req.NamespacedName, &dbClaim); err != nil {
		if client.IgnoreNotFound(err) != nil {
			logr.Error(err, "unable to fetch DatabaseClaim")
			return ctrl.Result{}, err
		} else {
			logr.Info("DatabaseClaim not found. Ignoring since object might have been deleted")
			return ctrl.Result{}, nil
		}
	}

	// Avoid updates to the claim until we know we should be looking at it

	if !isClassPermitted(r.Config.Class, dbClaim.Spec.Class) {
		logr.V(debugLevel).Info("class_not_owned", "class", dbClaim.Spec.Class, "expected", r.Config.Class)
		return ctrl.Result{}, nil
	}

	if err := validateDBClaim(&dbClaim); err != nil {
		res, err := r.statusManager.SetErrorStatus(ctx, &dbClaim, err)
		// TerminalError, do not requeue
		return res, reconcile.TerminalError(err)
	}

	if dbClaim.Spec.Class == nil {
		dbClaim.Spec.Class = ptr.To("default")
	}

	logr.Info("reconcile")

	if dbClaim.Status.ActiveDB.ConnectionInfo == nil {
		dbClaim.Status.ActiveDB.ConnectionInfo = new(v1.DatabaseClaimConnectionInfo)
	}
	if dbClaim.Status.NewDB.ConnectionInfo == nil {
		dbClaim.Status.NewDB.ConnectionInfo = new(v1.DatabaseClaimConnectionInfo)
	}

	reqInfo, err := NewRequestInfo(ctx, r.Config.Viper, &dbClaim)
	if err != nil {
		return r.statusManager.SetErrorStatus(ctx, &dbClaim, err)
	}

	// name of our custom finalizer
	dbFinalizerName := "databaseclaims.persistance.atlas.infoblox.com/finalizer"

	if !dbClaim.ObjectMeta.DeletionTimestamp.IsZero() {

		// The object is being deleted
		if controllerutil.ContainsFinalizer(&dbClaim, dbFinalizerName) {
			logr.Info("clean_up_finalizer")
			r.statusManager.SetStatusCondition(ctx, &dbClaim, DeletingCondition())
			// check if the claim is in the middle of rds migration, if so, wait for it to complete
			if dbClaim.Status.MigrationState != "" && dbClaim.Status.MigrationState != pgctl.S_Completed.String() {
				logr.Info("migration is in progress. object cannot be deleted")
				dbClaim.Status.Error = "dbc cannot be deleted while migration is in progress"
				err := r.Client.Status().Update(ctx, &dbClaim)
				if err != nil {
					logr.Error(err, "unable to update status. ignoring this error")
				}
				//ignore delete request, continue to process rds migration
				return r.executeDbClaimRequest(ctx, &reqInfo, &dbClaim)
			}
			if basefun.GetCloud(r.Config.Viper) == "aws" {
				// our finalizer is present, so lets handle any external dependency
				if err := r.deleteExternalResourcesAWS(ctx, &reqInfo, &dbClaim); err != nil {
					// if fail to delete the external dependency here, return with error
					// so that it can be retried
					return ctrl.Result{}, err
				}
			} else {
				// our finalizer is present, so lets handle any external dependency
				if err := r.deleteExternalResourcesGCP(ctx, &reqInfo, &dbClaim); err != nil {
					// if fail to delete the external dependency here, return with error
					// so that it can be retried
					return ctrl.Result{}, err
				}
			}
			// remove our finalizer from the list and update it.
			controllerutil.RemoveFinalizer(&dbClaim, dbFinalizerName)
			if err := r.Update(ctx, &dbClaim); err != nil {
				return ctrl.Result{}, err
			}
		}
		// Stop reconciliation as the item is being deleted
		return ctrl.Result{}, nil
	}
	// The object is not being deleted, so if it does not have our finalizer,
	// then lets add the finalizer and update the object. This is equivalent
	// registering our finalizer.
	if !controllerutil.ContainsFinalizer(&dbClaim, dbFinalizerName) {
		controllerutil.AddFinalizer(&dbClaim, dbFinalizerName)
		if err := r.Update(ctx, &dbClaim); err != nil {
			return ctrl.Result{}, err
		}
	}

	if r.Config.MetricsEnabled {
		if err := r.createMetricsDeployment(ctx, dbClaim); err != nil {
			return ctrl.Result{}, err
		}
	}
	res, err := r.executeDbClaimRequest(ctx, &reqInfo, &dbClaim)
	if err != nil {
		return res, err
	}

	return res, nil
}

func (r *DatabaseClaimReconciler) createMetricsDeployment(ctx context.Context, dbClaim v1.DatabaseClaim) error {
	cfg := exporter.NewConfig()
	cfg.Name = dbClaim.ObjectMeta.Name
	cfg.Namespace = dbClaim.ObjectMeta.Namespace
	cfg.DBClaimOwnerRef = string(dbClaim.ObjectMeta.UID)
	cfg.DepYamlPath = r.Config.MetricsDepYamlPath
	cfg.ConfigYamlPath = r.Config.MetricsConfigYamlPath
	cfg.DatasourceSecretName = dbClaim.Spec.SecretName
	cfg.DatasourceFileName = v1.DSNKey
	return exporter.Apply(ctx, r.Client, cfg)
}

func (r *DatabaseClaimReconciler) postMigrationInProgress(ctx context.Context, dbClaim *v1.DatabaseClaim) (ctrl.Result, error) {

	logr := log.FromContext(ctx).WithValues("databaseclaim", dbClaim.Namespace+"/"+dbClaim.Name)

	logr.Info("post migration is in progress")

	// get name of DBInstance from connectionInfo
	dbInstanceName := strings.Split(dbClaim.Status.OldDB.ConnectionInfo.Host, ".")[0]

	var dbParamGroupName string
	// get name of DBParamGroup from connectionInfo
	if dbClaim.Status.OldDB.Type == v1.AuroraPostgres {
		dbParamGroupName = dbInstanceName + "-a-" + (strings.Split(dbClaim.Status.OldDB.DBVersion, "."))[0]
	} else {
		dbParamGroupName = dbInstanceName + "-" + (strings.Split(dbClaim.Status.OldDB.DBVersion, "."))[0]
	}

	TagsVerified, err := r.manageOperationalTagging(ctx, logr, dbInstanceName, dbParamGroupName)

	// Even though we get error in updating tags, we log the error
	// and go ahead with deleting resources
	if err != nil || TagsVerified {

		if err != nil {
			logr.Error(err, "Failed updating or verifying operational tags")
		}

		if err = r.deleteCloudDatabaseAWS(dbInstanceName, ctx); err != nil {
			logr.Error(err, "Could not delete crossplane DBInstance/DBCLluster")
		}
		if err = r.deleteParameterGroupAWS(ctx, dbParamGroupName); err != nil {
			logr.Error(err, "Could not delete crossplane DBParamGroup/DBClusterParamGroup")
		}

		dbClaim.Status.OldDB = v1.StatusForOldDB{}
	} else if time.Since(dbClaim.Status.OldDB.PostMigrationActionStartedAt.Time).Minutes() > 10 {
		// Lets keep the state of old as it is for defined time to wait and verify tags before actually deleting resources
		logr.Info("defined wait time is over to verify operational tags on AWS resources. Moving ahead to delete associated crossplane resources anyway")

		if err = r.deleteCloudDatabaseAWS(dbInstanceName, ctx); err != nil {
			logr.Error(err, "Could not delete crossplane  DBInstance/DBCLluster")
		}
		if err = r.deleteParameterGroupAWS(ctx, dbParamGroupName); err != nil {
			logr.Error(err, "Could not delete crossplane  DBParamGroup/DBClusterParamGroup")
		}

		dbClaim.Status.OldDB = v1.StatusForOldDB{}
	}

	dbClaim.Status.Error = ""
	if err = r.statusManager.UpdateStatus(ctx, dbClaim); err != nil {
		return ctrl.Result{}, err
	}
	if !dbClaim.ObjectMeta.DeletionTimestamp.IsZero() {
		return ctrl.Result{Requeue: true}, nil
	}
	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

// Create, migrate or upgrade database
func (r *DatabaseClaimReconciler) executeDbClaimRequest(ctx context.Context, reqInfo *requestInfo, dbClaim *v1.DatabaseClaim) (ctrl.Result, error) {

	logr := log.FromContext(ctx).WithValues("databaseclaim", dbClaim.Namespace+"/"+dbClaim.Name)

	operationMode := r.getMode(ctx, reqInfo, dbClaim)
	if operationMode == M_NotSupported {
		return r.statusManager.SetErrorStatus(ctx, dbClaim, fmt.Errorf("unsupported operation requested"))
	}

	err := r.statusManager.UpdateStatus(ctx, dbClaim)
	if err != nil {
		return r.statusManager.SetErrorStatus(ctx, dbClaim, err)
	}

	if operationMode == M_PostMigrationInProgress {
		result, err := r.postMigrationInProgress(ctx, dbClaim)
		if err != nil {
			r.statusManager.SetErrorStatus(ctx, dbClaim, err)
		} 
		return result, err 
	}

	//when using an existing db, this is the first status, then it moves to M_MigrateExistingToNewDB and falls into the condition below
	if operationMode == M_UseExistingDB {
		logr.Info("existing db reconcile started")

		err := r.reconcileUseExistingDB(ctx, reqInfo, dbClaim, operationMode)
		if err != nil {
			return r.statusManager.SetErrorStatus(ctx, dbClaim, err)
		}

		newDBCopy := dbClaim.Status.NewDB.DeepCopy()
		dbClaim.Status.ActiveDB = *newDBCopy
		dbClaim.Status.NewDB = v1.Status{}

		if dbClaim.Status.ActiveDB.ConnectionInfo == nil {
			return r.statusManager.SetErrorStatus(ctx, dbClaim, fmt.Errorf("invalid new db connection"))
		}

		return r.statusManager.SuccessAndUpdateCondition(ctx, dbClaim)
	}
	if operationMode == M_MigrateExistingToNewDB {
		logr.Info("migrate to new  db reconcile started")
		//check if existingDB has been already reconciled, else reconcileUseExistingDB
		existingDbConn, err := v1.ParseUri(dbClaim.Spec.SourceDataFrom.Database.DSN)
		logr.V(debugLevel).Info("M_MigrateExistingToNewDB", "dsn", basefun.SanitizeDsn(dbClaim.Spec.SourceDataFrom.Database.DSN))
		if err != nil {
			return r.statusManager.SetErrorStatus(ctx, dbClaim, err)
		}

		activeConn := dbClaim.Status.ActiveDB.ConnectionInfo

		if (activeConn.DatabaseName != existingDbConn.DatabaseName) ||
			(activeConn.Host != existingDbConn.Host && activeConn.Port != existingDbConn.Port) {

			logr.Info("existing db was not reconciled, calling reconcileUseExistingDB before reconcileUseExistingDB")

			err := r.reconcileUseExistingDB(ctx, reqInfo, dbClaim, operationMode)
			if err != nil {
				return r.statusManager.SetErrorStatus(ctx, dbClaim, err)
			}
			dbClaim.Status.ActiveDB = *dbClaim.Status.NewDB.DeepCopy()
			dbClaim.Status.NewDB = v1.Status{ConnectionInfo: &v1.DatabaseClaimConnectionInfo{}}
		}

		return r.reconcileMigrateToNewDB(ctx, reqInfo, dbClaim, operationMode)
	}
	if operationMode == M_InitiateDBUpgrade {
		logr.Info("upgrade db initiated")

		return r.reconcileMigrateToNewDB(ctx, reqInfo, dbClaim, operationMode)

	}
	if operationMode == M_MigrationInProgress || operationMode == M_UpgradeDBInProgress {
		return r.reconcileMigrationInProgress(ctx, reqInfo, dbClaim, operationMode)
	}
	if operationMode == M_UseNewDB {
		result, err := r.reconcileNewDB(ctx, reqInfo, dbClaim, operationMode)
		if err != nil {
			return r.statusManager.SetErrorStatus(ctx, dbClaim, err)
		}
		if result.RequeueAfter > 0 {
			return result, nil
		}
		if reqInfo.TempSecret != "" {
			newDBConnInfo := dbClaim.Status.NewDB.ConnectionInfo.DeepCopy()
			newDBConnInfo.Password = reqInfo.TempSecret

			if err := r.createOrUpdateSecret(ctx, dbClaim, newDBConnInfo, basefun.GetCloud(r.Config.Viper)); err != nil {
				return r.statusManager.SetErrorStatus(ctx, dbClaim, err)
			}
		}
		dbClaim.Status.ActiveDB = *dbClaim.Status.NewDB.DeepCopy()
		if reqInfo.SharedDBHost {
			dbClaim.Status.ActiveDB.DbState = v1.UsingSharedHost
		} else {
			dbClaim.Status.ActiveDB.DbState = v1.Ready
		}
		dbClaim.Status.NewDB = v1.Status{}

		return r.statusManager.SuccessAndUpdateCondition(ctx, dbClaim)
	}

	logr.Error(fmt.Errorf("unhandled mode: %v", operationMode), "unhandled mode")
	return r.statusManager.SetErrorStatus(ctx, dbClaim, fmt.Errorf("unhandled mode"))

}

// reconcileUseExistingDB reconciles the existing db
// bool indicates that object status should be updated
func (r *DatabaseClaimReconciler) reconcileUseExistingDB(ctx context.Context, reqInfo *requestInfo, dbClaim *v1.DatabaseClaim, operationalMode ModeEnum) error {
	logr := log.FromContext(ctx).WithValues("databaseclaim", dbClaim.Namespace+"/"+dbClaim.Name)

	activeDB := dbClaim.Status.ActiveDB
	// Exit immediately if we have nothing to do
	if activeDB.UserUpdatedAt != nil {
		lastUpdateSince := time.Since(activeDB.UserUpdatedAt.Time)
		if lastUpdateSince < r.getPasswordRotationTime() {
			logr.Info("user updated recently, skipping reconcile", "elapsed", lastUpdateSince)
			return ErrDoNotUpdateStatus
		}
	}

	logr.Info("status_block", "status", dbClaim.Status)

	sourceDSN, err := auth.GetSourceDataFromDSN(ctx, r.Client, dbClaim)
	if err != nil {
		logr.Error(err, "unable to populate creds")
		return err
	}

	// Existing source creds that become master creds
	existingDBConnInfo, err := v1.ParseUri(sourceDSN)
	if err != nil {
		logr.Error(err, "unable to parse source dsn")
		return err
	}

	if dbClaim.Status.ActiveDB.DbState == v1.UsingExistingDB {
		// Verify These databases are not the same
		if dbClaim.Status.ActiveDB.ConnectionInfo.Host == existingDBConnInfo.Host &&
			dbClaim.Status.ActiveDB.ConnectionInfo.Port == existingDBConnInfo.Port {
			dbClaim.Status.NewDB = *dbClaim.Status.ActiveDB.DeepCopy()
		}
	}
	dbClaim.Status.NewDB.DbState = v1.UsingExistingDB
	dbClaim.Status.NewDB.SourceDataFrom = dbClaim.Spec.SourceDataFrom.DeepCopy()

	masterPassword := existingDBConnInfo.Password

	//cache master password for existing db
	err = r.setMasterPasswordInTempSecret(ctx, masterPassword, dbClaim)
	if err != nil {
		logr.Error(err, "cache master password error")
		return err
	}
	dbClient, err := r.getClientForExistingDB(ctx, dbClaim, existingDBConnInfo)
	if err != nil {
		logr.Error(err, "creating database client error")
		return err
	}
	defer dbClient.Close()

	logr.Info(fmt.Sprintf("processing DBClaim: %s namespace: %s AppID: %s", dbClaim.Name, dbClaim.Namespace, dbClaim.Spec.AppID))

	dbName := existingDBConnInfo.DatabaseName
	r.statusManager.UpdateDBStatus(&dbClaim.Status.NewDB, dbName)

	err = r.manageUserAndExtensions(ctx, reqInfo, logr, dbClient, dbClaim, operationalMode)
	if err != nil {
		logr.Error(err, "unable to update users, user credents not persisted to status object")
		return err
	}
	if err = r.statusManager.UpdateStatus(ctx, dbClaim); err != nil {
		return err
	}
	if reqInfo.TempSecret != "" {
		newDBConnInfo := dbClaim.Status.NewDB.ConnectionInfo.DeepCopy()
		newDBConnInfo.Password = reqInfo.TempSecret

		if err := r.createOrUpdateSecret(ctx, dbClaim, newDBConnInfo, basefun.GetCloud(r.Config.Viper)); err != nil {
			return err
		}
		logr.V(1).Info("password reset, created secret", "secret", dbClaim.Spec.SecretName)
	}
	err = dbClient.ManageSystemFunctions(dbName, basefun.GetSystemFunctions(r.Config.Viper))
	if err != nil {
		return err
	}

	return nil
}

func (r *DatabaseClaimReconciler) reconcileNewDB(ctx context.Context, reqInfo *requestInfo, dbClaim *v1.DatabaseClaim, operationalMode ModeEnum) (ctrl.Result, error) {
	logr := log.FromContext(ctx).WithValues("databaseclaim", dbClaim.Namespace+"/"+dbClaim.Name, "func", "reconcileNewDB")
	logr.Info("reconcileNewDB", "r.Input", reqInfo)

	r.statusManager.SetStatusCondition(ctx, dbClaim, ProvisioningCondition())

	cloud := basefun.GetCloud(r.Config.Viper)

	isReady := false
	var err error
	if cloud == "aws" {
		isReady, err = r.manageCloudHostAWS(ctx, reqInfo, dbClaim, operationalMode)
		if err != nil {
			logr.Error(err, "manage_cloud_host_AWS")
			return ctrl.Result{}, err
		}
	} else if cloud == "gcp" {
		isReady, err = r.manageCloudHostGCP(ctx, reqInfo, dbClaim)
		if err != nil {
			logr.Error(err, "manage_cloud_host_GCP")
			return ctrl.Result{}, err
		}
	} else {
		return r.statusManager.SetErrorStatus(ctx, dbClaim, fmt.Errorf("cloud not supported, check .Values.cloud"))
	}
	if dbClaim.Status.Error != "" {
		dbClaim.Status.Error = ""
		if err := r.statusManager.UpdateStatus(ctx, dbClaim); err != nil {
			logr.Error(err, "update_client_status")
			return ctrl.Result{}, err
		}
	}

	dbHostIdentifier := r.getDynamicHostName(reqInfo.HostParams.Hash(), dbClaim)

	if !isReady {
		logr.Info("cloud instance provisioning is in progress", "instance name", dbHostIdentifier, "next-step", "requeueing")
		return ctrl.Result{RequeueAfter: basefun.GetDynamicHostWaitTime(r.Config.Viper)}, nil
	}

	logr.V(1).Info("cloud instance ready. reading generated master secret", "instance", dbHostIdentifier)
	connInfo, err := r.kctl.GetMasterCredsDeprecated(ctx, dbHostIdentifier, dbClaim.Spec.DatabaseName, basefun.GetDefaultSSLMode(r.Config.Viper))
	if err != nil {
		logr.Error(err, "error retrieving master credentials", "operationMode", operationalMode)
		return ctrl.Result{RequeueAfter: basefun.GetDynamicHostWaitTime(r.Config.Viper)}, nil
	}
	reqInfo.MasterConnInfo.Host = connInfo.Host
	reqInfo.MasterConnInfo.Password = connInfo.Password
	reqInfo.MasterConnInfo.Port = connInfo.Port
	reqInfo.MasterConnInfo.Username = connInfo.Username
	reqInfo.MasterConnInfo.DatabaseName = connInfo.DatabaseName
	reqInfo.MasterConnInfo.SSLMode = connInfo.SSLMode

	dbClient, err := dbclient.New(dbclient.Config{
		Log:    log.FromContext(ctx).WithValues("caller", "reconcileNewDB"),
		DBType: "postgres",
		DSN:    connInfo.Uri(),
	})
	if err != nil {
		logr.Error(err, "unable_dbclient")
		return ctrl.Result{}, err
	}
	defer dbClient.Close()

	// Series of partial updates to the status newb, get ready

	// Update connection info to object
	r.statusManager.UpdateHostPortStatus(&dbClaim.Status.NewDB, connInfo.Host, connInfo.Port, connInfo.SSLMode)

	// Setup status.NewDB object with desired hostparams used to create it
	r.statusManager.UpdateClusterStatus(&dbClaim.Status.NewDB, &reqInfo.HostParams)

	// Updates the database name
	if err := r.createDatabaseAndExtensions(ctx, reqInfo, dbClient, &dbClaim.Status.NewDB, operationalMode); err != nil {
		logr.Error(err, "unable to create database and extensions")
		return r.statusManager.SetErrorStatus(ctx, dbClaim, err)
	}

	// Updates the connection info username
	err = r.manageUserAndExtensions(ctx, reqInfo, logr, dbClient, dbClaim, operationalMode)
	if err != nil {
		logr.Error(err, "unable to update users, user credentials not persisted to status object")
		return r.statusManager.SetErrorStatus(ctx, dbClaim, err)
	}

	// TODO: I don't know what purpose these serve
	// ref: https://github.com/infobloxopen/db-controller/pull/193
	err = dbClient.ManageSystemFunctions(dbClaim.Spec.DatabaseName, basefun.GetSystemFunctions(r.Config.Viper))
	if err != nil {
		return r.statusManager.SetErrorStatus(ctx, dbClaim, err)
	}
	logr.V(debugLevel).Info("populated_newdb_information", "newdb", dbClaim.Status.NewDB)
	return ctrl.Result{}, nil
}

func (r *DatabaseClaimReconciler) reconcileMigrateToNewDB(ctx context.Context, reqInfo *requestInfo, dbClaim *v1.DatabaseClaim, operationalMode ModeEnum) (ctrl.Result, error) {

	logr := log.FromContext(ctx)

	if dbClaim.Status.MigrationState == "" {
		dbClaim.Status.MigrationState = pgctl.S_Initial.String()
		if err := r.statusManager.UpdateStatus(ctx, dbClaim); err != nil {
			logr.Error(err, "could not update db claim")
			return r.statusManager.SetErrorStatus(ctx, dbClaim, err)
		}
	}
	result, err := r.reconcileNewDB(ctx, reqInfo, dbClaim, operationalMode)
	if err != nil {
		logr.Error(err, "reconcile_new_db")
		return r.statusManager.SetErrorStatus(ctx, dbClaim, err)
	}
	if result.RequeueAfter > 0 {
		return result, nil
	}

	// store a temp secret to be used by migration process
	// removing the practice of storing the secret in status
	if reqInfo.TempSecret != "" {
		if err := r.setTargetPasswordInTempSecret(ctx, reqInfo.TempSecret, dbClaim); err != nil {
			return r.statusManager.SetErrorStatus(ctx, dbClaim, err)
		}
	}

	// Store the source DSN, otherwise it will be lost
	sourceDSN, err := r.getSrcAppDsnFromSecret(ctx, dbClaim)
	if err != nil {
		return r.statusManager.SetErrorStatus(ctx, dbClaim, err)
	}

	err = r.setSourceDsnInTempSecret(ctx, sourceDSN, dbClaim)
	if err != nil {
		return r.statusManager.SetErrorStatus(ctx, dbClaim, err)
	}

	// Preserve credentials to temp secret or less we risk losing state

	// Update migration state to one of these
	// M_MigrationInProgress, M_UpgradeDBInProgress
	return r.statusManager.MigrationInProgressStatus(ctx, dbClaim)
}

func (r *DatabaseClaimReconciler) reconcileMigrationInProgress(ctx context.Context, reqInfo *requestInfo, dbClaim *v1.DatabaseClaim, operationalMode ModeEnum) (ctrl.Result, error) {

	logr := log.FromContext(ctx).WithValues("databaseclaim", dbClaim.Namespace+"/"+dbClaim.Name, "func", "reconcileMigrationInProgress")

	migrationState := dbClaim.Status.MigrationState

	if dbClaim.Status.NewDB.ConnectionInfo == nil ||
		dbClaim.Status.NewDB.ConnectionInfo.Host == "" {
		err := fmt.Errorf("status.newdb is empty")
		logr.Error(err, "unable_to_migrate_no_newdb")
		return r.statusManager.SetErrorStatus(ctx, dbClaim, err)
	}

	logr.Info("Migration in progress", "state", migrationState)

	dbHostIdentifier := r.getDynamicHostName(reqInfo.HostParams.Hash(), dbClaim)

	connInfo, err := r.kctl.GetMasterCredsDeprecated(ctx, dbHostIdentifier, dbClaim.Spec.DatabaseName, basefun.GetDefaultSSLMode(r.Config.Viper))
	if err != nil {
		logr.Error(err, "unable to read the complete secret. requeueing")
		return ctrl.Result{RequeueAfter: basefun.GetDynamicHostWaitTime(r.Config.Viper)}, nil
	}
	logr.Info("cloud instance ready", "newdb", dbClaim.Status.NewDB)
	// FIXME: this is terrible logic, why is this even a thing?
	reqInfo.MasterConnInfo = *connInfo.DeepCopy()

	targetMasterDsn := reqInfo.MasterConnInfo.Uri()

	logr.Info("Status", "status", dbClaim.Status)

	newInfo := dbClaim.Status.NewDB.ConnectionInfo
	activeInfo := dbClaim.Status.ActiveDB.ConnectionInfo
	// FIXME: remove this

	if newInfo.Host == activeInfo.Host && newInfo.Port == activeInfo.Port {
		return r.statusManager.SetErrorStatus(ctx, dbClaim, fmt.Errorf("active and new database can not be the same"))
	}

	targetAppConn := dbClaim.Status.NewDB.ConnectionInfo.DeepCopy()

	targetAppConn.Password, err = r.getTargetPasswordFromTempSecret(ctx, dbClaim)
	if err != nil {
		return r.statusManager.SetErrorStatus(ctx, dbClaim, err)
	}
	if err := targetAppConn.Validate(); err != nil {
		logr.Error(err, "target_user_connection_is_invalid")
		return r.statusManager.SetErrorStatus(ctx, dbClaim, err)
	}

	sourceAppDsn, err := r.getSrcAppDsnFromSecret(ctx, dbClaim)
	if err != nil {
		return r.statusManager.SetErrorStatus(ctx, dbClaim, err)
	}

	// Maybe the credentials are in a temp secret
	if sourceAppDsn == "" {
		sourceAppDsn, err = r.getSourceDsnFromTempSecret(ctx, dbClaim)
		if err != nil {
			logr.Error(err, "unable to retrieve source dsn")
			return r.statusManager.SetErrorStatus(ctx, dbClaim, err)
		}
	}
	if sourceAppDsn == "" {
		return r.statusManager.SetErrorStatus(ctx, dbClaim, fmt.Errorf("unable_to_find_source_user_dsn"))
	}

	// FIXME: replace misc auth fetching with PopulateCreds
	if _, err := auth.PopulateCreds(ctx, r.Client, dbClaim, r.Config.Viper.GetString("SERVICE_NAMESPACE")); err != nil {
		return r.statusManager.SetErrorStatus(ctx, dbClaim, fmt.Errorf("unable to populate credentials"))
	}

	var sourceMasterConn *v1.DatabaseClaimConnectionInfo

	// Parsing source database credentials
	if operationalMode == M_MigrationInProgress || operationalMode == M_MigrateExistingToNewDB {
		if dbClaim.Spec.SourceDataFrom == nil {
			return r.statusManager.SetErrorStatus(ctx, dbClaim, fmt.Errorf("sourceDataFrom is nil"))
		}
		if dbClaim.Spec.SourceDataFrom.Database == nil {
			return r.statusManager.SetErrorStatus(ctx, dbClaim, fmt.Errorf("sourceDataFrom.Database is nil"))
		}

		dsn, err := auth.GetSourceDataFromDSN(ctx, r.Client, dbClaim)
		if err != nil {
			return r.statusManager.SetErrorStatus(ctx, dbClaim, err)
		}
		sourceMasterConn, err = v1.ParseUri(dsn)
		if err != nil {
			return r.statusManager.SetErrorStatus(ctx, dbClaim, err)
		}

	} else if operationalMode == M_UpgradeDBInProgress || operationalMode == M_InitiateDBUpgrade {
		activeHost, _, _ := strings.Cut(dbClaim.Status.ActiveDB.ConnectionInfo.Host, ".")

		activeConnInfo, err := r.kctl.GetMasterCredsDeprecated(ctx, activeHost, dbClaim.Spec.DatabaseName, dbClaim.Status.ActiveDB.ConnectionInfo.SSLMode)
		if err != nil {
			logr.Error(err, "error retrieving master credentials", "operationMode", operationalMode)
			return r.statusManager.SetErrorStatus(ctx, dbClaim, err)
		}
		//copy over source app connection and replace userid and password with master userid and password
		sourceMasterConn, err = v1.ParseUri(sourceAppDsn)
		if err != nil {
			return r.statusManager.SetErrorStatus(ctx, dbClaim, err)
		}
		sourceMasterConn.Username = activeConnInfo.Username
		sourceMasterConn.Password = activeConnInfo.Password

	} else {
		err := fmt.Errorf("unsupported operational mode %v", operationalMode)
		return r.statusManager.SetErrorStatus(ctx, dbClaim, err)
	}

	config := pgctl.Config{
		Cloud:            basefun.GetCloud(r.Config.Viper),
		Log:              log.FromContext(ctx),
		SourceDBAdminDsn: sourceMasterConn.Uri(),
		SourceDBUserDsn:  sourceAppDsn,
		TargetDBUserDsn:  targetAppConn.Uri(),
		TargetDBAdminDsn: targetMasterDsn,
		ExportFilePath:   basefun.GetPgTempFolder(r.Config.Viper),
	}

	logr.V(debugLevel).Info("pctl_dsn", "config", config)

	s, err := pgctl.GetReplicatorState(migrationState, config)
	if err != nil {
		return r.statusManager.SetErrorStatus(ctx, dbClaim, err)
	}

	// Check source user creds and update
	if err := validateAndUpdateCredsDSN(ctx, config.SourceDBAdminDsn, config.SourceDBUserDsn); err != nil {
		logr.Error(err, "source_dsn_validate_failed")
		return r.statusManager.SetErrorStatus(ctx, dbClaim, err)
	}
	// Check Target user creds and update
	if err := validateAndUpdateCredsDSN(ctx, config.TargetDBAdminDsn, config.TargetDBUserDsn); err != nil {
		logr.Error(err, "target_dsn_validate_failed")
		return r.statusManager.SetErrorStatus(ctx, dbClaim, err)
	}

loop:
	for {
		next, err := s.Execute()
		if err != nil {
			return r.statusManager.SetErrorStatus(ctx, dbClaim, err)
		}
		switch next.Id() {
		case pgctl.S_Completed:
			logr.Info("completed migration")

			//PTEUDO1051: if migration was successfull, get the list of DBRoleClaims attached to the old DBClaim and recreate
			// them pointing to the new DBClaim
			dbRoleClaims := &v1.DbRoleClaimList{}
			if err := r.Client.List(ctx, dbRoleClaims, client.InNamespace(dbClaim.Namespace)); err != nil {
				return r.statusManager.SetErrorStatus(ctx, dbClaim, err)
			}
			logr.Info("copying dbroleclaims to new dbclaim")
			for _, dbrc := range dbRoleClaims.Items {
				if strings.HasSuffix(dbrc.Name, "-"+dbClaim.Name) {
					continue
				}

				err = r.Client.Get(ctx, client.ObjectKey{
					Name:      dbrc.Name + "-" + dbClaim.Name,
					Namespace: dbrc.Namespace,
				}, &v1.DbRoleClaim{})

				if errors.IsNotFound(err) {
					dbrc.Name = dbrc.Name + "-" + dbClaim.Name
					dbrc.Spec.SecretName = dbrc.Spec.SecretName + "-" + dbClaim.Name
					dbrc.Spec.SourceDatabaseClaim.Name = dbClaim.Name
					dbrc.Status = v1.DbRoleClaimStatus{}
					dbrc.ResourceVersion = ""
					dbrc.Generation = 0

					if err = r.Client.Create(ctx, &dbrc); err != nil {
						logr.Error(err, "could not update db role claim")
						return r.statusManager.SetErrorStatus(ctx, dbClaim, err)
					}
					logr.Info("dbroleclaim copied: " + dbrc.Name)
				} else {
					logr.Info("dbroleclaim [" + dbrc.Name + "] already exists.")
				}
			}

			break loop

		case pgctl.S_Retry:
			logr.Info("retry called")
			return ctrl.Result{RequeueAfter: 60 * time.Second}, nil

		case pgctl.S_WaitToDisableSource:
			s = next
			dbClaim.Status.MigrationState = s.String()
			if err = r.statusManager.UpdateStatus(ctx, dbClaim); err != nil {
				logr.Error(err, "could not update db claim")
				return r.statusManager.SetErrorStatus(ctx, dbClaim, err)
			}
			logr.Info("Requeue, waiting to disable source")
			// TODO: alter this for tests
			return ctrl.Result{RequeueAfter: 60 * time.Second}, nil

		case pgctl.S_RerouteTargetSecret:
			// FIXME: this is no longer called and should be removed
			logr.Info("reroute target secret")
			if err = r.rerouteTargetSecret(ctx, sourceAppDsn, targetAppConn, dbClaim); err != nil {
				return r.statusManager.SetErrorStatus(ctx, dbClaim, err)
			}
			s = next
			dbClaim.Status.MigrationState = s.String()
			if err = r.statusManager.UpdateStatus(ctx, dbClaim); err != nil {
				logr.Error(err, "could not update db claim")
				return r.statusManager.SetErrorStatus(ctx, dbClaim, err)
			}
		default:
			s = next
			dbClaim.Status.MigrationState = s.String()
			if err = r.statusManager.UpdateStatus(ctx, dbClaim); err != nil {
				logr.Error(err, "could not update db claim")
				return r.statusManager.SetErrorStatus(ctx, dbClaim, err)
			}
		}
	}
	dbClaim.Status.MigrationState = pgctl.S_Completed.String()

	if dbClaim.Status.ActiveDB.DbState != v1.UsingExistingDB {
		timenow := metav1.Now()
		dbClaim.Status.OldDB = v1.StatusForOldDB{ConnectionInfo: &v1.DatabaseClaimConnectionInfo{}}

		MakeDeepCopyToOldDB(&dbClaim.Status.OldDB, &dbClaim.Status.ActiveDB)
		dbClaim.Status.OldDB.DbState = v1.PostMigrationInProgress
		dbClaim.Status.OldDB.PostMigrationActionStartedAt = &timenow
	}

	//done with migration- switch active server to newDB
	logr.V(debugLevel).Info("migration complete updating activedb")
	dbClaim.Status.ActiveDB = *dbClaim.Status.NewDB.DeepCopy()
	dbClaim.Status.ActiveDB.DbState = v1.Ready
	dbClaim.Status.NewDB = v1.Status{ConnectionInfo: &v1.DatabaseClaimConnectionInfo{}}

	if err = r.statusManager.UpdateStatus(ctx, dbClaim); err != nil {
		logr.Error(err, "could not update db claim")
		return r.statusManager.SetErrorStatus(ctx, dbClaim, err)
	}

	err = r.deleteTempSecret(ctx, dbClaim)
	if err != nil {
		logr.Error(err, "ignoring delete temp secret error")
	}
	//create connection info secret
	logr.Info("migration complete")

	return r.statusManager.SuccessAndUpdateCondition(ctx, dbClaim)
}

func MakeDeepCopyToOldDB(to *v1.StatusForOldDB, from *v1.Status) {
	to.ConnectionInfo = from.ConnectionInfo.DeepCopy()
	to.DBVersion = from.DBVersion
	to.Shape = from.Shape
	to.Type = from.Type
}

// ManageOperationalTagging: Will update operational tags on old DBInstance, DBCluster, DBClusterParamGroup and DBParamGroup.
// It does not return error for DBCluster, DBClusterParamGroup and DBParamGroup if they fail to update tags. Such error is only logged, but not returned.
// In case of successful updation, It  does not to verify whether those tags got updated.
//
// Unlike other resources,
// It returns error just for  DBinstance failling to update tags.
// It also verifies whether DBinstance got updated with the tag, and return the signal as boolean.
//
//	true: operational tag is updated and verfied.
//	false: operational tag is updated but could not be verified yet.
func (r *DatabaseClaimReconciler) ManageOperationalTagging(ctx context.Context, logr logr.Logger, dbInstanceName, dbParamGroupName string) (bool, error) {
	return r.manageOperationalTagging(ctx, logr, dbInstanceName, dbParamGroupName)
}

func (r *DatabaseClaimReconciler) manageOperationalTagging(ctx context.Context, logr logr.Logger, dbInstanceName, dbParamGroupName string) (bool, error) {

	err := r.operationalTaggingForDbClusterParamGroup(ctx, logr, dbParamGroupName)
	if err != nil {
		return false, err
	}
	err = r.operationalTaggingForDbParamGroup(ctx, logr, dbParamGroupName)
	if err != nil {
		return false, err
	}
	err = r.operationalTaggingForDbCluster(ctx, logr, dbInstanceName)
	if err != nil {
		return false, err
	}

	// unlike other resources above, verifying tags updation and handling errors if any just for "DBInstance" resource
	isVerfied, err := r.operationalTaggingForDbInstance(ctx, logr, dbInstanceName)

	if basefun.GetMultiAZEnabled(r.Config.Viper) {
		isVerfiedforMultiAZ, errMultiAZ := r.operationalTaggingForDbInstance(ctx, logr, dbInstanceName+"-2")
		if err != nil {
			return false, err
		} else if errMultiAZ != nil {
			return false, errMultiAZ
		} else if !isVerfied || !isVerfiedforMultiAZ {
			return false, nil
		} else {
			return true, nil
		}

	} else {
		if err != nil {
			return false, err
		} else {
			return isVerfied, nil
		}
	}

}

func (r *DatabaseClaimReconciler) getClientForExistingDB(ctx context.Context, dbClaim *v1.DatabaseClaim, connInfo *v1.DatabaseClaimConnectionInfo) (dbclient.Clienter, error) {

	if connInfo == nil {
		return nil, fmt.Errorf("invalid connection info")
	}

	if connInfo.Host == "" {
		return nil, fmt.Errorf("invalid host name")
	}

	if connInfo.Port == "" {
		return nil, fmt.Errorf("cannot get master port")
	}

	if connInfo.Username == "" {
		return nil, fmt.Errorf("invalid credentials (username)")
	}

	if connInfo.SSLMode == "" {
		return nil, fmt.Errorf("invalid sslMode")
	}

	if connInfo.Password == "" {
		return nil, fmt.Errorf("invalid credentials (password)")
	}

	r.statusManager.UpdateHostPortStatus(&dbClaim.Status.NewDB, connInfo.Host, connInfo.Port, connInfo.SSLMode)

	//log.Log.V(DebugLevel).Info("GET CLIENT FOR EXISTING DB> Full URI: " + connInfo.Uri())

	return dbclient.New(dbclient.Config{Log: log.FromContext(ctx), DBType: "postgres", DSN: connInfo.Uri()})
}

func (r *DatabaseClaimReconciler) getDBClient(ctx context.Context, reqInfo *requestInfo, dbClaim *v1.DatabaseClaim) (dbclient.Clienter, error) {
	logr := log.FromContext(ctx).WithValues("databaseclaim", dbClaim.Namespace+"/"+dbClaim.Name, "func", "getDBClient")

	logr.V(debugLevel).Info("GET DBCLIENT", "DSN", basefun.SanitizeDsn(r.getMasterDefaultDsn(reqInfo)))
	r.statusManager.UpdateHostPortStatus(&dbClaim.Status.NewDB, reqInfo.MasterConnInfo.Host, reqInfo.MasterConnInfo.Port, reqInfo.MasterConnInfo.SSLMode)
	return dbclient.New(dbclient.Config{Log: log.FromContext(ctx), DBType: "postgres", DSN: r.getMasterDefaultDsn(reqInfo)})
}

func (r *DatabaseClaimReconciler) getMasterDefaultDsn(reqInfo *requestInfo) string {
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s", url.QueryEscape(reqInfo.MasterConnInfo.Username), url.QueryEscape(reqInfo.MasterConnInfo.Password), reqInfo.MasterConnInfo.Host, reqInfo.MasterConnInfo.Port, "postgres", reqInfo.MasterConnInfo.SSLMode)
}

func (r *DatabaseClaimReconciler) generatePassword() (string, error) {
	var pass string
	var err error
	minPasswordLength := basefun.GetMinPasswordLength(r.Config.Viper)
	complEnabled := basefun.GetIsPasswordComplexity(r.Config.Viper)

	// Customize the list of symbols.
	// Removed \ ` @ ! from the default list as the encoding/decoding was treating it as an escape character
	// In some cases downstream application was not able to handle it
	gen, err := gopassword.NewGenerator(&gopassword.GeneratorInput{
		Symbols: "~#%^&*()_+-={}|[]:<>?,.",
	})
	if err != nil {
		return "", err
	}

	if complEnabled {
		count := minPasswordLength / 4
		pass, err = gen.Generate(minPasswordLength, count, count, false, false)
		if err != nil {
			return "", err
		}
	} else {
		pass, err = gen.Generate(basefun.GetDefaultPassLen(), basefun.GetDefaultNumDig(), basefun.GetDefaultNumSimb(), false, false)
		if err != nil {
			return "", err
		}
	}

	return pass, nil
}

func generateMasterPassword() (string, error) {
	var pass string
	var err error
	minPasswordLength := 30

	pass, err = gopassword.Generate(minPasswordLength, 3, 0, false, true)
	if err != nil {
		return "", err
	}
	return pass, nil
}

func (r *DatabaseClaimReconciler) getPasswordRotationTime() time.Duration {
	return basefun.GetPasswordRotationPeriod(r.Config.Viper)
}

// FindStatusCondition finds the conditionType in conditions.
func (r *DatabaseClaimReconciler) isResourceReady(typ, name string, resourceStatus xpv1.ResourceStatus) (bool, error) {
	if ok, err := isResourceReady(resourceStatus); err != nil {
		return ok, fmt.Errorf("%s %s: %w", typ, name, err)
	}
	return true, nil
}

func isResourceReady(resourceStatus xpv1.ResourceStatus) (bool, error) {
	ready := xpv1.TypeReady
	conditionTrue := corev1.ConditionTrue
	for _, condition := range resourceStatus.Conditions {
		if condition.Type == ready && condition.Status == conditionTrue {
			return true, nil
		}
		if condition.Reason == "ReconcileError" {
			//handle the following error and provide a specific message to the user
			//create failed: cannot create DBInstance in AWS: InvalidParameterCombination:
			//Cannot find version 15.3 for postgres\n\tstatus code: 400, request id:
			if strings.Contains(condition.Message, "InvalidParameterCombination: Cannot find version") {
				// extract the version from the message and update the dbClaim
				return false, fmt.Errorf("%w: requested version %s", v1.ErrInvalidDBVersion, extractVersion(condition.Message))
			}
			return false, fmt.Errorf("resource is not ready: %s", condition.Message)
		}
	}
	return false, nil
}

func extractVersion(message string) string {
	versionStr := ""
	splitMessage := strings.Split(message, " ")
	for i, word := range splitMessage {
		if word == "version" {
			versionStr = splitMessage[i+1] // This should be of format "15.3"
			break
		}
	}
	return versionStr
}

// getDynamicHostName returns a dynamic hostname based on the hash and dbClaim.
func (r *DatabaseClaimReconciler) getDynamicHostName(hash string, dbClaim *v1.DatabaseClaim) string {
	var prefix string
	suffix := "-" + hash

	if basefun.GetDBIdentifierPrefix(r.Config.Viper) != "" {
		prefix = basefun.GetDBIdentifierPrefix(r.Config.Viper) + "-"
	}
	return prefix + dbClaim.Name + suffix
}

func (r *DatabaseClaimReconciler) getParameterGroupName(hostParams *hostparams.HostParams, dbClaim *v1.DatabaseClaim, dbType v1.DatabaseType) string {
	hostName := r.getDynamicHostName(hostParams.Hash(), dbClaim)

	switch dbType {
	case v1.Postgres:
		return hostName + "-" + (strings.Split(hostParams.DBVersion, "."))[0]
	case v1.AuroraPostgres:
		return hostName + "-a-" + (strings.Split(hostParams.DBVersion, "."))[0]
	default:
		return hostName + "-" + (strings.Split(hostParams.DBVersion, "."))[0]
	}
}

func (r *DatabaseClaimReconciler) createDatabaseAndExtensions(ctx context.Context, reqInfo *requestInfo, dbClient dbclient.Creater, status *v1.Status, operationalMode ModeEnum) error {
	logr := log.FromContext(ctx)

	dbName := reqInfo.MasterConnInfo.DatabaseName
	created, err := dbClient.CreateDatabase(dbName)
	if err != nil {
		return err
	}
	if created && operationalMode == M_UseNewDB {
		//the migrations usecase takes care of copying extensions
		//only in newDB workflow they need to be created explicitly
		err = dbClient.CreateDefaultExtensions(dbName)
		if err != nil {
			msg := fmt.Sprintf("error creating default extensions for %s", dbName)
			logr.Error(err, msg)
			return err
		}
	}
	if created || status.ConnectionInfo.DatabaseName == "" {
		r.statusManager.UpdateDBStatus(status, dbName)
	}
	return nil
}

func (r *DatabaseClaimReconciler) manageUserAndExtensions(ctx context.Context, reqInfo *requestInfo, logger logr.Logger, dbClient dbclient.Clienter, dbClaim *v1.DatabaseClaim, operationalMode ModeEnum) error {

	status := dbClaim.Status.NewDB
	dbName := dbClaim.Spec.DatabaseName
	baseUsername := dbClaim.Spec.Username

	dbu := dbuser.NewDBUser(baseUsername)
	rotationTime := r.getPasswordRotationTime()

	// create role
	roleCreated, err := dbClient.CreateRole(dbName, baseUsername, "public")
	if err != nil {
		return err
	}

	if roleCreated && operationalMode == M_UseNewDB {
		// This only runs on new databases, and perhaps should not even be run there
		// take care of special extensions related to the user
		err = dbClient.CreateSpecialExtensions(dbName, baseUsername)
		if err != nil {
			return err
		}
	}

	if status.ConnectionInfo == nil {
		return fmt.Errorf("connection info is nil")
	}

	userName := status.ConnectionInfo.Username

	if dbu.IsUserChanged(userName) {
		oldUsername := dbuser.TrimUserSuffix(userName)
		if err := dbClient.RenameUser(oldUsername, baseUsername); err != nil {
			return err
		}
		// updating user a
		userPassword, err := r.generatePassword()
		if err != nil {
			return err
		}
		if err := dbClient.UpdateUser(oldUsername+dbuser.SuffixA, dbu.GetUserA(), baseUsername, userPassword); err != nil {
			return err
		}
		r.statusManager.UpdateUserStatus(&status, reqInfo, dbu.GetUserA(), userPassword)
		// updating user b
		userPassword, err = r.generatePassword()
		if err != nil {
			return err
		}
		if err := dbClient.UpdateUser(oldUsername+dbuser.SuffixB, dbu.GetUserB(), baseUsername, userPassword); err != nil {
			return err
		}
	}

	if status.UserUpdatedAt == nil || time.Since(status.UserUpdatedAt.Time) > rotationTime {
		logger.V(1).Info("rotating_users", "rotationTime", rotationTime)

		r.statusManager.SetStatusCondition(ctx, dbClaim, PwdRotationCondition())

		userPassword, err := r.generatePassword()
		if err != nil {
			return err
		}

		nextUser := dbu.NextUser(status.ConnectionInfo.Username)
		created, err := dbClient.CreateUser(nextUser, baseUsername, userPassword)
		if err != nil {
			metrics.PasswordRotatedErrors.WithLabelValues("create error").Inc()
			return err
		}
		_ = created

		if err := dbClient.UpdatePassword(nextUser, userPassword); err != nil {
			return err
		}

		r.statusManager.UpdateUserStatus(&status, reqInfo, nextUser, userPassword)
	}

	// baseUsername = myuser
	// user_a = myuser_a
	// user_b = myuser_b
	err = dbClient.ManageSuperUserRole(baseUsername, reqInfo.EnableSuperUser)
	// Ignore errors revoking a grouprole from a userrole. This may
	// fail on non-cloud databases
	if err != nil && reqInfo.EnableSuperUser {
		return fmt.Errorf("managesuperuserrole on role %s: %w", baseUsername, err)
	}
	err = dbClient.ManageCreateRole(baseUsername, reqInfo.EnableSuperUser)
	if err != nil {
		return fmt.Errorf("managecreaterole on role %s: %w", baseUsername, err)
	}
	// TODO: change this to modify the the role ie. baseUsername
	err = dbClient.ManageReplicationRole(status.ConnectionInfo.Username, reqInfo.EnableReplicationRole)
	// Ignore errors when revoking replication role from a userrole. This may
	// fail on non-cloud databases
	if err != nil && reqInfo.EnableReplicationRole {
		return fmt.Errorf("managereplicationrole on role %s: %w", status.ConnectionInfo.Username, err)
	}
	err = dbClient.ManageReplicationRole(dbu.NextUser(status.ConnectionInfo.Username), reqInfo.EnableReplicationRole)
	// Ignore errors when revoking replication role from a userrole. This may
	// fail on non-cloud databases
	if err != nil && reqInfo.EnableReplicationRole {
		return fmt.Errorf("managereplicationrole on role %s: %w", dbu.NextUser(status.ConnectionInfo.Username), err)
	}

	return nil
}

func (r *DatabaseClaimReconciler) configureBackupPolicy(backupPolicy string, tags []v1.Tag) []v1.Tag {

	for _, tag := range tags {
		if tag.Key == defaultBackupPolicyKey {
			if tag.Value != backupPolicy {
				if backupPolicy == "" {
					tag.Value = basefun.GetDefaultBackupPolicy(r.Config.Viper)
				} else {
					tag.Value = backupPolicy
				}
			}
			return tags
		}
	}

	if backupPolicy == "" {
		tags = append(tags, v1.Tag{Key: defaultBackupPolicyKey, Value: basefun.GetDefaultBackupPolicy(r.Config.Viper)})
	} else {
		tags = append(tags, v1.Tag{Key: defaultBackupPolicyKey, Value: backupPolicy})
	}
	return tags
}

func (r *DatabaseClaimReconciler) rerouteTargetSecret(ctx context.Context, sourceDsn string, targetAppConn *v1.DatabaseClaimConnectionInfo, dbClaim *v1.DatabaseClaim) error {

	//store source dsn before overwriting secret
	err := r.setSourceDsnInTempSecret(ctx, sourceDsn, dbClaim)
	if err != nil {
		return err
	}
	err = r.createOrUpdateSecret(ctx, dbClaim, targetAppConn, basefun.GetCloud(r.Config.Viper))
	if err != nil {
		return err
	}

	return nil
}

func (r *DatabaseClaimReconciler) getServiceNamespace() (string, error) {
	if r.Config.Namespace == "" {
		return "", fmt.Errorf("service namespace env %s must be set", serviceNamespaceEnvVar)
	}

	return r.Config.Namespace, nil
}
