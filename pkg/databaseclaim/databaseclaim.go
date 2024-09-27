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
	basefun "github.com/infobloxopen/db-controller/pkg/basefunctions"
	"github.com/infobloxopen/db-controller/pkg/dbclient"
	"github.com/infobloxopen/db-controller/pkg/dbuser"
	"github.com/infobloxopen/db-controller/pkg/hostparams"
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

type ModeEnum int

const (
	M_NotSupported ModeEnum = iota
	M_UseExistingDB
	M_MigrateExistingToNewDB
	M_MigrationInProgress
	M_UseNewDB
	M_InitiateDBUpgrade
	M_UpgradeDBInProgress
	M_PostMigrationInProgress
)

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
	Config *DatabaseClaimConfig
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

// getMode determines the mode of operation for the database claim.
func (r *DatabaseClaimReconciler) getMode(ctx context.Context, reqInfo *requestInfo, dbClaim *v1.DatabaseClaim) ModeEnum {
	// Shadow variable
	log := log.FromContext(ctx).WithValues("databaseclaim", dbClaim.Namespace+"/"+dbClaim.Name, "func", "getMode")
	//default mode is M_UseNewDB. any non supported combination needs to be identified and set to M_NotSupported

	if dbClaim.Status.OldDB.DbState == v1.PostMigrationInProgress {
		if dbClaim.Status.OldDB.ConnectionInfo == nil || dbClaim.Status.ActiveDB.DbState != v1.Ready ||
			reqInfo.SharedDBHost {
			return M_NotSupported
		}
	}

	if dbClaim.Status.OldDB.DbState == v1.PostMigrationInProgress && dbClaim.Status.ActiveDB.DbState == v1.Ready {
		return M_PostMigrationInProgress
	}

	if reqInfo.SharedDBHost {
		if dbClaim.Status.ActiveDB.DbState == v1.UsingSharedHost {
			activeHostParams := hostparams.GetActiveHostParams(dbClaim)
			if reqInfo.HostParams.IsUpgradeRequested(activeHostParams) {
				log.Info("upgrade requested for a shared host. shared host upgrades are not supported. ignoring upgrade request")
			}
		}
		log.V(debugLevel).Info("selected mode for shared db host", "dbclaim", dbClaim.Spec, "selected mode", "M_UseNewDB")

		return M_UseNewDB
	}

	// use existing is true
	if *dbClaim.Spec.UseExistingSource {
		if dbClaim.Spec.SourceDataFrom != nil && dbClaim.Spec.SourceDataFrom.Type == "database" {
			log.V(debugLevel).Info("selected mode for", "dbclaim", dbClaim.Spec, "selected mode", "use existing db")
			return M_UseExistingDB
		} else {
			return M_NotSupported
		}
	}
	// use existing is false // source data is present
	if dbClaim.Spec.SourceDataFrom != nil {
		if dbClaim.Spec.SourceDataFrom.Type == "database" {
			if dbClaim.Status.ActiveDB.DbState == v1.UsingExistingDB {
				if dbClaim.Status.MigrationState == "" || dbClaim.Status.MigrationState == pgctl.S_Initial.String() {
					log.V(debugLevel).Info("selected mode for", "dbclaim", dbClaim.Spec, "selected mode", "M_MigrateExistingToNewDB")
					return M_MigrateExistingToNewDB
				} else if dbClaim.Status.MigrationState != pgctl.S_Completed.String() {
					log.V(debugLevel).Info("selected mode for", "dbclaim", dbClaim.Spec, "selected mode", "M_MigrationInProgress")
					return M_MigrationInProgress
				}
			}
		} else {
			return M_NotSupported
		}
	}
	// use existing is false // source data is not present
	if dbClaim.Spec.SourceDataFrom == nil {
		if dbClaim.Status.ActiveDB.DbState == v1.UsingExistingDB {
			//make sure status contains all the requires sourceDataFrom info
			if dbClaim.Status.ActiveDB.SourceDataFrom != nil {
				dbClaim.Spec.SourceDataFrom = dbClaim.Status.ActiveDB.SourceDataFrom.DeepCopy()
				if dbClaim.Status.MigrationState == "" || dbClaim.Status.MigrationState == pgctl.S_Initial.String() {
					log.V(debugLevel).Info("selected mode for", "dbclaim", dbClaim.Spec, "selected mode", "M_MigrateExistingToNewDB")
					return M_MigrateExistingToNewDB
				} else if dbClaim.Status.MigrationState != pgctl.S_Completed.String() {
					log.V(debugLevel).Info("selected mode for", "dbclaim", dbClaim.Spec, "selected mode", "M_MigrationInProgress")
					return M_MigrationInProgress
				}
			} else {
				log.Info("something is wrong. use existing is false // source data is not present. sourceDataFrom is not present in status")
				return M_NotSupported
			}
		}
	}

	// use existing is false; source data is not present ; active status is using-existing-db or ready
	// activeDB does not have sourceDataFrom info
	if dbClaim.Status.ActiveDB.DbState == v1.Ready {
		activeHostParams := hostparams.GetActiveHostParams(dbClaim)
		if reqInfo.HostParams.IsUpgradeRequested(activeHostParams) {
			if dbClaim.Status.NewDB.DbState == "" {
				dbClaim.Status.NewDB.DbState = v1.InProgress
				dbClaim.Status.MigrationState = ""
			}
			if dbClaim.Status.MigrationState == "" || dbClaim.Status.MigrationState == pgctl.S_Initial.String() {
				log.V(debugLevel).Info("selected mode for", "dbclaim", dbClaim.Spec, "selected mode", "M_InitiateDBUpgrade")
				return M_InitiateDBUpgrade
			} else if dbClaim.Status.MigrationState != pgctl.S_Completed.String() {
				log.V(debugLevel).Info("selected mode for", "dbclaim", dbClaim.Spec, "selected mode", "M_UpgradeDBInProgress")
				return M_UpgradeDBInProgress

			}
		}
	}

	log.V(debugLevel).Info("selected mode for", "dbclaim", dbClaim.Spec, "selected mode", "M_UseNewDB")

	return M_UseNewDB
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
		logr.V(debugLevel).Info("class_not_owned", "class", dbClaim.Spec.Class)
		return ctrl.Result{}, nil
	}

	if err := validateDBClaim(&dbClaim); err != nil {
		res, err := r.manageError(ctx, &dbClaim, err)
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
		return r.manageError(ctx, &dbClaim, err)
	}

	// name of our custom finalizer
	dbFinalizerName := "databaseclaims.persistance.atlas.infoblox.com/finalizer"

	if !dbClaim.ObjectMeta.DeletionTimestamp.IsZero() {
		// The object is being deleted
		if controllerutil.ContainsFinalizer(&dbClaim, dbFinalizerName) {
			logr.Info("clean_up_finalizer")
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
		return r.manageError(ctx, &dbClaim, err)
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
	if err = r.updateClientStatus(ctx, dbClaim); err != nil {
		return r.manageError(ctx, dbClaim, err)
	}
	if !dbClaim.ObjectMeta.DeletionTimestamp.IsZero() {
		return ctrl.Result{Requeue: true}, nil
	}
	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

// Create, migrate or upgrade database
func (r *DatabaseClaimReconciler) executeDbClaimRequest(ctx context.Context, reqInfo *requestInfo, dbClaim *v1.DatabaseClaim) (ctrl.Result, error) {

	logr := log.FromContext(ctx).WithValues("databaseclaim", dbClaim.Namespace+"/"+dbClaim.Name)

	if dbClaim.Status.ActiveDB.ConnectionInfo == nil {
		dbClaim.Status.ActiveDB.ConnectionInfo = new(v1.DatabaseClaimConnectionInfo)
	}
	if dbClaim.Status.NewDB.ConnectionInfo == nil {
		dbClaim.Status.NewDB.ConnectionInfo = new(v1.DatabaseClaimConnectionInfo)
	}

	operationMode := r.getMode(ctx, reqInfo, dbClaim)
	if operationMode == M_PostMigrationInProgress {
		return r.postMigrationInProgress(ctx, dbClaim)
	}
	//when using an existing db, this is the first status, then it moves to M_MigrateExistingToNewDB and falls into the condition below
	if operationMode == M_UseExistingDB {
		logr.Info("existing db reconcile started")
		err := r.reconcileUseExistingDB(ctx, reqInfo, dbClaim, operationMode)
		if err != nil {
			return r.manageError(ctx, dbClaim, err)
		}

		dbClaim.Status.ActiveDB = *dbClaim.Status.NewDB.DeepCopy()
		dbClaim.Status.NewDB = v1.Status{ConnectionInfo: &v1.DatabaseClaimConnectionInfo{}}

		logr.Info("existing db reconcile complete")
		return r.manageSuccess(ctx, dbClaim)
	}
	if operationMode == M_MigrateExistingToNewDB {
		logr.Info("migrate to new  db reconcile started")
		//check if existingDB has been already reconciled, else reconcileUseExistingDB
		existing_db_conn, err := v1.ParseUri(dbClaim.Spec.SourceDataFrom.Database.DSN)
		logr.V(debugLevel).Info("DSN", "M_MigrateExistingToNewDB", basefun.SanitizeDsn(dbClaim.Spec.SourceDataFrom.Database.DSN))
		if err != nil {
			return r.manageError(ctx, dbClaim, err)
		}
		if (dbClaim.Status.ActiveDB.ConnectionInfo.DatabaseName != existing_db_conn.DatabaseName) ||
			(dbClaim.Status.ActiveDB.ConnectionInfo.Host != existing_db_conn.Host) {

			logr.Info("existing db was not reconciled, calling reconcileUseExistingDB before reconcileUseExistingDB")

			err := r.reconcileUseExistingDB(ctx, reqInfo, dbClaim, operationMode)
			if err != nil {
				return r.manageError(ctx, dbClaim, err)
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

		logr.Info("Use new DB")
		result, err := r.reconcileNewDB(ctx, reqInfo, dbClaim, operationMode)
		if err != nil {
			return r.manageError(ctx, dbClaim, err)
		}
		if result.RequeueAfter > 0 {
			logr.Info("requeuing request")
			return result, nil
		}
		if reqInfo.TempSecret != "" {
			newDBConnInfo := dbClaim.Status.NewDB.ConnectionInfo.DeepCopy()
			newDBConnInfo.Password = reqInfo.TempSecret

			if err := r.createOrUpdateSecret(ctx, dbClaim, newDBConnInfo, basefun.GetCloud(r.Config.Viper)); err != nil {
				return r.manageError(ctx, dbClaim, err)
			}
		}
		dbClaim.Status.ActiveDB = *dbClaim.Status.NewDB.DeepCopy()
		if reqInfo.SharedDBHost {
			dbClaim.Status.ActiveDB.DbState = v1.UsingSharedHost
		} else {
			dbClaim.Status.ActiveDB.DbState = v1.Ready
		}
		dbClaim.Status.NewDB = v1.Status{ConnectionInfo: &v1.DatabaseClaimConnectionInfo{}}

		return r.manageSuccess(ctx, dbClaim)
	}

	logr.Info("unhandled mode")
	return r.manageError(ctx, dbClaim, fmt.Errorf("unhandled mode"))

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

	existingDBConnInfo, err := getExistingDSN(ctx, r.Client, dbClaim)
	if err != nil {
		return err
	}

	if dbClaim.Status.ActiveDB.DbState == v1.UsingExistingDB {
		if dbClaim.Status.ActiveDB.ConnectionInfo.Host == existingDBConnInfo.Host {
			logr.Info("requested existing db host is same as active db host. reusing existing db host")
			dbClaim.Status.NewDB = *dbClaim.Status.ActiveDB.DeepCopy()
		}
	}
	dbClaim.Status.NewDB.DbState = v1.UsingExistingDB
	dbClaim.Status.NewDB.SourceDataFrom = dbClaim.Spec.SourceDataFrom.DeepCopy()

	logr.Info("creating database client")
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
	updateDBStatus(&dbClaim.Status.NewDB, dbName)

	err = r.manageUserAndExtensions(reqInfo, logr, dbClient, &dbClaim.Status.NewDB, dbName, dbClaim.Spec.Username, operationalMode)
	if err != nil {
		return err
	}
	if err = r.updateClientStatus(ctx, dbClaim); err != nil {
		return err
	}
	if reqInfo.TempSecret != "" {
		logr.Info("password reset. updating secret")
		newDBConnInfo := dbClaim.Status.NewDB.ConnectionInfo.DeepCopy()
		newDBConnInfo.Password = reqInfo.TempSecret

		if err := r.createOrUpdateSecret(ctx, dbClaim, newDBConnInfo, basefun.GetCloud(r.Config.Viper)); err != nil {
			return err
		}
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

	cloud := basefun.GetCloud(r.Config.Viper)

	isReady := false
	var err error
	if cloud == "aws" {
		isReady, err = r.manageCloudHostAWS(ctx, reqInfo, dbClaim, operationalMode)
		if err != nil {
			logr.Error(err, "manage_cloud_host_AWS")
			return ctrl.Result{}, err
		}
	} else {
		isReady, err = r.manageCloudHostGCP(ctx, reqInfo, dbClaim)
		if err != nil {
			logr.Error(err, "manage_cloud_host_GCP")
			return ctrl.Result{}, err
		}
	}
	// Clear existing error
	if dbClaim.Status.Error != "" {
		//resetting error
		dbClaim.Status.Error = ""
		if err := r.updateClientStatus(ctx, dbClaim); err != nil {
			return ctrl.Result{}, err
		}
	}

	dbHostIdentifier := r.getDynamicHostName(reqInfo.HostParams.Hash(), dbClaim)

	if !isReady {
		logr.Info("cloud instance provisioning is in progress", "instance name", dbHostIdentifier, "next-step", "requeueing")
		return ctrl.Result{RequeueAfter: basefun.GetDynamicHostWaitTime(r.Config.Viper)}, nil
	}

	logr.Info("cloud instance ready. reading generated master secret")
	connInfo, err := r.readResourceSecret(ctx, dbHostIdentifier)
	if err != nil {
		logr.Error(err, "unable to read the complete secret. requeueing")
		return ctrl.Result{RequeueAfter: basefun.GetDynamicHostWaitTime(r.Config.Viper)}, nil
	}
	reqInfo.MasterConnInfo.Host = connInfo.Host
	reqInfo.MasterConnInfo.Password = connInfo.Password
	reqInfo.MasterConnInfo.Port = connInfo.Port
	reqInfo.MasterConnInfo.Username = connInfo.Username
	reqInfo.MasterConnInfo.DatabaseName = dbClaim.Spec.DatabaseName
	reqInfo.MasterConnInfo.SSLMode = basefun.GetDefaultSSLMode(r.Config.Viper)

	dbClient, err := r.getDBClient(ctx, reqInfo, dbClaim)
	if err != nil {
		logr.Error(err, "creating database client error")
		return ctrl.Result{}, err
	}
	defer dbClient.Close()

	if reqInfo.MasterConnInfo.Host == dbClaim.Status.ActiveDB.ConnectionInfo.Host {
		dbClaim.Status.NewDB = *dbClaim.Status.ActiveDB.DeepCopy()
		if dbClaim.Status.NewDB.MinStorageGB != reqInfo.HostParams.MinStorageGB {
			dbClaim.Status.NewDB.MinStorageGB = reqInfo.HostParams.MinStorageGB
		}
		if reqInfo.HostParams.Engine == string(v1.Postgres) && int(dbClaim.Status.NewDB.MaxStorageGB) != int(reqInfo.HostParams.MaxStorageGB) {
			dbClaim.Status.NewDB.MaxStorageGB = reqInfo.HostParams.MaxStorageGB
		}
	} else {
		updateClusterStatus(&dbClaim.Status.NewDB, &reqInfo.HostParams)
	}
	if err := r.createDatabaseAndExtensions(ctx, reqInfo, dbClient, &dbClaim.Status.NewDB, operationalMode); err != nil {
		return ctrl.Result{}, err

	}
	err = r.manageUserAndExtensions(reqInfo, logr, dbClient, &dbClaim.Status.NewDB, dbClaim.Spec.DatabaseName, dbClaim.Spec.Username, operationalMode)
	if err != nil {
		return ctrl.Result{}, err
	}
	err = dbClient.ManageSystemFunctions(dbClaim.Spec.DatabaseName, basefun.GetSystemFunctions(r.Config.Viper))
	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *DatabaseClaimReconciler) reconcileMigrateToNewDB(ctx context.Context, reqInfo *requestInfo, dbClaim *v1.DatabaseClaim, operationalMode ModeEnum) (ctrl.Result, error) {

	logr := log.FromContext(ctx)

	if dbClaim.Status.MigrationState == "" {
		dbClaim.Status.MigrationState = pgctl.S_Initial.String()
		if err := r.updateClientStatus(ctx, dbClaim); err != nil {
			logr.Error(err, "could not update db claim")
			return r.manageError(ctx, dbClaim, err)
		}
	}
	result, err := r.reconcileNewDB(ctx, reqInfo, dbClaim, operationalMode)
	if err != nil {
		return r.manageError(ctx, dbClaim, err)
	}
	if result.RequeueAfter > 0 {
		return result, nil
	}
	//store a temp secret to be used by migration process
	//removing the practice of storing the secret in status
	if reqInfo.TempSecret != "" {
		r.setTargetPasswordInTempSecret(ctx, reqInfo.TempSecret, dbClaim)
	}

	return r.reconcileMigrationInProgress(ctx, reqInfo, dbClaim, operationalMode)
}

func (r *DatabaseClaimReconciler) reconcileMigrationInProgress(ctx context.Context, reqInfo *requestInfo, dbClaim *v1.DatabaseClaim, operationalMode ModeEnum) (ctrl.Result, error) {

	logr := log.FromContext(ctx).WithValues("databaseclaim", dbClaim.Namespace+"/"+dbClaim.Name, "func", "reconcileMigrationInProgress")

	migrationState := dbClaim.Status.MigrationState

	logr.Info("Migration in progress", "state", migrationState)

	dbHostIdentifier := r.getDynamicHostName(reqInfo.HostParams.Hash(), dbClaim)

	logr.Info("cloud instance ready. reading generated master secret")
	connInfo, err := r.readResourceSecret(ctx, dbHostIdentifier)
	if err != nil {
		logr.Error(err, "unable to read the complete secret. requeueing")
		return ctrl.Result{RequeueAfter: basefun.GetDynamicHostWaitTime(r.Config.Viper)}, nil
	}
	reqInfo.MasterConnInfo.Host = connInfo.Host
	reqInfo.MasterConnInfo.Password = connInfo.Password
	reqInfo.MasterConnInfo.Port = connInfo.Port
	reqInfo.MasterConnInfo.Username = connInfo.Username

	targetMasterDsn := reqInfo.MasterConnInfo.Uri()
	targetAppConn := dbClaim.Status.NewDB.ConnectionInfo.DeepCopy()
	targetAppConn.Password, err = r.getTargetPasswordFromTempSecret(ctx, dbClaim)
	if err != nil {
		return r.manageError(ctx, dbClaim, err)
	}

	sourceAppDsn, err := r.getSrcAppDsnFromSecret(ctx, dbClaim)
	if err != nil {
		return r.manageError(ctx, dbClaim, err)
	}
	var sourceMasterConn *v1.DatabaseClaimConnectionInfo

	if operationalMode == M_MigrationInProgress || operationalMode == M_MigrateExistingToNewDB {
		if dbClaim.Spec.SourceDataFrom == nil {
			return r.manageError(ctx, dbClaim, fmt.Errorf("sourceDataFrom is nil"))
		}
		if dbClaim.Spec.SourceDataFrom.Database == nil {
			return r.manageError(ctx, dbClaim, fmt.Errorf("sourceDataFrom.Database is nil"))
		}
		sourceMasterConn, err = v1.ParseUri(dbClaim.Spec.SourceDataFrom.Database.DSN)
		if err != nil {
			return r.manageError(ctx, dbClaim, err)
		}
		sourceMasterConn.Password, err = r.getSrcAdminPasswdFromSecret(ctx, dbClaim)
		if err != nil {
			logr.Error(err, "source master secret and cached master secret not found")
			return r.manageError(ctx, dbClaim, err)
		}
	} else if operationalMode == M_UpgradeDBInProgress || operationalMode == M_InitiateDBUpgrade {
		activeHost, _, _ := strings.Cut(dbClaim.Status.ActiveDB.ConnectionInfo.Host, ".")

		activeConnInfo, err := r.readResourceSecret(ctx, activeHost)
		if err != nil {
			logr.Error(err, "unable to read the complete secret. requeueing")
			return r.manageError(ctx, dbClaim, err)
		}
		//copy over source app connection and replace userid and password with master userid and password
		sourceMasterConn, err = v1.ParseUri(sourceAppDsn)
		if err != nil {
			return r.manageError(ctx, dbClaim, err)
		}
		sourceMasterConn.Username = activeConnInfo.Username
		sourceMasterConn.Password = activeConnInfo.Password

	} else {
		err := fmt.Errorf("unsupported operational mode %v", operationalMode)
		return r.manageError(ctx, dbClaim, err)
	}
	logr.V(debugLevel).Info("DSN", "sourceAppDsn", sourceAppDsn)
	logr.V(debugLevel).Info("DSN", "sourceMasterConn", sourceMasterConn)

	config := pgctl.Config{
		Log:              log.FromContext(ctx),
		SourceDBAdminDsn: sourceMasterConn.Uri(),
		SourceDBUserDsn:  sourceAppDsn,
		TargetDBUserDsn:  targetAppConn.Uri(),
		TargetDBAdminDsn: targetMasterDsn,
		ExportFilePath:   basefun.GetPgTempFolder(r.Config.Viper),
	}

	logr.V(debugLevel).Info("DSN", "config", config)

	s, err := pgctl.GetReplicatorState(migrationState, config)
	if err != nil {
		return r.manageError(ctx, dbClaim, err)
	}

loop:
	for {
		next, err := s.Execute()
		if err != nil {
			return r.manageError(ctx, dbClaim, err)
		}
		switch next.Id() {
		case pgctl.S_Completed:
			logr.Info("completed migration")

			//PTEUDO1051: if migration was successfull, get the list of DBRoleClaims attached to the old DBClaim and recreate
			// them pointing to the new DBClaim
			dbRoleClaims := &v1.DbRoleClaimList{}
			if err := r.Client.List(ctx, dbRoleClaims, client.InNamespace(dbClaim.Namespace)); err != nil {
				return r.manageError(ctx, dbClaim, err)
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
						return r.manageError(ctx, dbClaim, err)
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
			logr.Info("wait called")
			s = next
			dbClaim.Status.MigrationState = s.String()
			if err = r.updateClientStatus(ctx, dbClaim); err != nil {
				logr.Error(err, "could not update db claim")
				return r.manageError(ctx, dbClaim, err)
			}
			return ctrl.Result{RequeueAfter: 60 * time.Second}, nil

		case pgctl.S_RerouteTargetSecret:
			if err = r.rerouteTargetSecret(ctx, sourceAppDsn, targetAppConn, dbClaim); err != nil {
				return r.manageError(ctx, dbClaim, err)
			}
			s = next
			dbClaim.Status.MigrationState = s.String()
			if err = r.updateClientStatus(ctx, dbClaim); err != nil {
				logr.Error(err, "could not update db claim")
				return r.manageError(ctx, dbClaim, err)
			}
		default:
			s = next
			dbClaim.Status.MigrationState = s.String()
			if err = r.updateClientStatus(ctx, dbClaim); err != nil {
				logr.Error(err, "could not update db claim")
				return r.manageError(ctx, dbClaim, err)
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
	dbClaim.Status.ActiveDB = *dbClaim.Status.NewDB.DeepCopy()
	dbClaim.Status.ActiveDB.DbState = v1.Ready
	dbClaim.Status.NewDB = v1.Status{ConnectionInfo: &v1.DatabaseClaimConnectionInfo{}}

	if err = r.updateClientStatus(ctx, dbClaim); err != nil {
		logr.Error(err, "could not update db claim")
		return r.manageError(ctx, dbClaim, err)
	}

	err = r.deleteTempSecret(ctx, dbClaim)
	if err != nil {
		logr.Error(err, "ignoring delete temp secret error")
	}
	//create connection info secret
	logr.Info("migration complete")

	return r.manageSuccess(ctx, dbClaim)
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
	updateHostPortStatus(&dbClaim.Status.NewDB, connInfo.Host, connInfo.Port, connInfo.SSLMode)

	//log.Log.V(DebugLevel).Info("GET CLIENT FOR EXISTING DB> Full URI: " + connInfo.Uri())

	return dbclient.New(dbclient.Config{Log: log.FromContext(ctx), DBType: "postgres", DSN: connInfo.Uri()})
}

func (r *DatabaseClaimReconciler) getDBClient(ctx context.Context, reqInfo *requestInfo, dbClaim *v1.DatabaseClaim) (dbclient.Clienter, error) {
	logr := log.FromContext(ctx).WithValues("databaseclaim", dbClaim.Namespace+"/"+dbClaim.Name, "func", "getDBClient")

	logr.V(debugLevel).Info("GET DBCLIENT", "DSN", basefun.SanitizeDsn(r.getMasterDefaultDsn(reqInfo)))
	updateHostPortStatus(&dbClaim.Status.NewDB, reqInfo.MasterConnInfo.Host, reqInfo.MasterConnInfo.Port, reqInfo.MasterConnInfo.SSLMode)
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
func (r *DatabaseClaimReconciler) isResourceReady(resourceStatus xpv1.ResourceStatus) (bool, error) {
	return isResourceReady(resourceStatus)
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

func (r *DatabaseClaimReconciler) readResourceSecret(ctx context.Context, secretName string) (v1.DatabaseClaimConnectionInfo, error) {
	rs := &corev1.Secret{}
	connInfo := v1.DatabaseClaimConnectionInfo{}

	serviceNS, _ := r.getServiceNamespace()

	err := r.Client.Get(ctx, client.ObjectKey{
		Namespace: serviceNS,
		Name:      secretName,
	}, rs)
	//TODO handle not found vs other errors here
	if err != nil {
		return connInfo, err
	}

	connInfo.Host = string(rs.Data["endpoint"])
	connInfo.Port = string(rs.Data["port"])
	connInfo.Username = string(rs.Data["username"])
	connInfo.Password = string(rs.Data["password"])
	connInfo.SSLMode = basefun.GetDefaultSSLMode(r.Config.Viper)

	if connInfo.Host == "" ||
		connInfo.Port == "" ||
		connInfo.Username == "" ||
		connInfo.Password == "" {
		return connInfo, fmt.Errorf("generated secret is incomplete")
	}

	return connInfo, nil
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
		return hostName + "-" + (strings.Split(hostParams.EngineVersion, "."))[0]
	case v1.AuroraPostgres:
		return hostName + "-a-" + (strings.Split(hostParams.EngineVersion, "."))[0]
	default:
		return hostName + "-" + (strings.Split(hostParams.EngineVersion, "."))[0]
	}
}

func (r *DatabaseClaimReconciler) createDatabaseAndExtensions(ctx context.Context, reqInfo *requestInfo, dbClient dbclient.Creater, status *v1.Status, operationalMode ModeEnum) error {
	logr := log.FromContext(ctx)

	dbName := reqInfo.MasterConnInfo.DatabaseName
	created, err := dbClient.CreateDatabase(dbName)
	if err != nil {
		msg := fmt.Sprintf("error creating database postgresURI %s using %s", dbName, reqInfo.MasterConnInfo.Uri())
		logr.Error(err, msg)
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
		updateDBStatus(status, dbName)
	}
	return nil
}

func (r *DatabaseClaimReconciler) manageUserAndExtensions(reqInfo *requestInfo, logger logr.Logger, dbClient dbclient.Clienter, status *v1.Status, dbName string, baseUsername string, operationalMode ModeEnum) error {

	if status == nil {
		return fmt.Errorf("status is nil")
	}

	// baseUsername := dbClaim.Spec.Username
	dbu := dbuser.NewDBUser(baseUsername)
	rotationTime := r.getPasswordRotationTime()

	// create role
	roleCreated, err := dbClient.CreateRole(dbName, baseUsername, "public")
	if err != nil {
		return err
	}
	if roleCreated && operationalMode != M_MigrateExistingToNewDB && operationalMode != M_MigrationInProgress {
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
		r.updateUserStatus(status, reqInfo, dbu.GetUserA(), userPassword)
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
		logger.Info("rotating_users", "rotationTime", rotationTime)

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

		if !created {
			if err := dbClient.UpdatePassword(nextUser, userPassword); err != nil {
				return err
			}
		}

		r.updateUserStatus(status, reqInfo, nextUser, userPassword)
	}
	err = dbClient.ManageSuperUserRole(baseUsername, reqInfo.EnableSuperUser)
	if err != nil {
		return err
	}
	err = dbClient.ManageCreateRole(baseUsername, reqInfo.EnableSuperUser)
	if err != nil {
		return err
	}
	err = dbClient.ManageReplicationRole(status.ConnectionInfo.Username, reqInfo.EnableReplicationRole)
	if err != nil {
		return err
	}
	err = dbClient.ManageReplicationRole(dbu.NextUser(status.ConnectionInfo.Username), reqInfo.EnableReplicationRole)
	if err != nil {
		return err
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

func (r *DatabaseClaimReconciler) manageMasterPassword(ctx context.Context, secret *xpv1.SecretKeySelector) error {
	logr := log.FromContext(ctx).WithValues("func", "manageMasterPassword")
	masterSecret := &corev1.Secret{}
	password, err := generateMasterPassword()
	if err != nil {
		return err
	}
	err = r.Client.Get(ctx, client.ObjectKey{
		Name:      secret.SecretReference.Name,
		Namespace: secret.SecretReference.Namespace,
	}, masterSecret)
	if err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
		//master secret not found, create it
		masterSecret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: secret.SecretReference.Namespace,
				Name:      secret.SecretReference.Name,
			},
			Data: map[string][]byte{
				secret.Key: []byte(password),
			},
		}
		logr.Info("creating master secret", "name", secret.Name, "namespace", secret.Namespace)
		return r.Client.Create(ctx, masterSecret)
	}
	logr.Info("master secret exists")
	return nil
}

func (r *DatabaseClaimReconciler) rerouteTargetSecret(ctx context.Context, sourceDsn string,
	targetAppConn *v1.DatabaseClaimConnectionInfo, dbClaim *v1.DatabaseClaim) error {

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
func (r *DatabaseClaimReconciler) updateUserStatus(status *v1.Status, reqInfo *requestInfo, userName, userPassword string) {
	timeNow := metav1.Now()
	status.UserUpdatedAt = &timeNow
	status.ConnectionInfo.Username = userName
	reqInfo.TempSecret = userPassword
	status.ConnectionInfoUpdatedAt = &timeNow
}

func updateDBStatus(status *v1.Status, dbName string) {
	timeNow := metav1.Now()
	status.DbCreatedAt = &timeNow
	status.ConnectionInfo.DatabaseName = dbName
	status.ConnectionInfoUpdatedAt = &timeNow
}

func updateHostPortStatus(status *v1.Status, host, port, sslMode string) {
	timeNow := metav1.Now()
	status.ConnectionInfo.Host = host
	status.ConnectionInfo.Port = port
	status.ConnectionInfo.SSLMode = sslMode
	status.ConnectionInfoUpdatedAt = &timeNow
}

func updateClusterStatus(status *v1.Status, hostParams *hostparams.HostParams) {
	status.DBVersion = hostParams.EngineVersion
	status.Type = v1.DatabaseType(hostParams.Engine)
	status.Shape = hostParams.Shape
	status.MinStorageGB = hostParams.MinStorageGB
	if hostParams.Engine == string(v1.Postgres) {
		status.MaxStorageGB = hostParams.MaxStorageGB
	}
}

func (r *DatabaseClaimReconciler) getServiceNamespace() (string, error) {
	if r.Config.Namespace == "" {
		return "", fmt.Errorf("service namespace env %s must be set", serviceNamespaceEnvVar)
	}

	return r.Config.Namespace, nil
}

func (r *DatabaseClaimReconciler) getSrcAdminPasswdFromSecret(ctx context.Context, dbClaim *v1.DatabaseClaim) (string, error) {
	secretKey := "password"
	gs := &corev1.Secret{}

	ns := dbClaim.Spec.SourceDataFrom.Database.SecretRef.Namespace
	if ns == "" {
		ns = dbClaim.Namespace
	}
	err := r.Client.Get(ctx, client.ObjectKey{
		Namespace: ns,
		Name:      dbClaim.Spec.SourceDataFrom.Database.SecretRef.Name,
	}, gs)
	if err == nil {
		return string(gs.Data[secretKey]), nil
	}
	//err!=nil
	if !errors.IsNotFound(err) {
		return "", err
	}
	//not found - check temp secret
	p, err := r.getMasterPasswordFromTempSecret(ctx, dbClaim)
	if err != nil {
		return "", err
	}
	return p, nil
}

func (r *DatabaseClaimReconciler) getSrcAppDsnFromSecret(ctx context.Context, dbClaim *v1.DatabaseClaim) (string, error) {
	migrationState := dbClaim.Status.MigrationState
	state, err := pgctl.GetStateEnum(migrationState)
	if err != nil {
		return "", err
	}

	if state > pgctl.S_RerouteTargetSecret {
		//dsn is pulled from temp secret since app secret is not using new db
		return r.getSourceDsnFromTempSecret(ctx, dbClaim)
	}

	secretName := dbClaim.Spec.SecretName
	gs := &corev1.Secret{}

	ns := dbClaim.Namespace
	if ns == "" {
		ns = "default"
	}
	err = r.Client.Get(ctx, client.ObjectKey{
		Namespace: ns,
		Name:      secretName,
	}, gs)
	if err != nil {
		log.FromContext(ctx).Error(err, "getSrcAppPasswdFromSecret failed")
		return "", err
	}
	return string(gs.Data[v1.DSNURIKey]), nil

}

func (r *DatabaseClaimReconciler) deleteTempSecret(ctx context.Context, dbClaim *v1.DatabaseClaim) error {
	secretName := getTempSecretName(dbClaim)

	gs := &corev1.Secret{}

	ns := dbClaim.Namespace
	if ns == "" {
		ns = "default"
	}
	err := r.Client.Get(ctx, client.ObjectKey{
		Namespace: ns,
		Name:      secretName,
	}, gs)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	return r.Client.Delete(ctx, gs)
}

func (r *DatabaseClaimReconciler) getSourceDsnFromTempSecret(ctx context.Context, dbClaim *v1.DatabaseClaim) (string, error) {
	secretName := getTempSecretName(dbClaim)

	gs := &corev1.Secret{}

	ns := dbClaim.Namespace
	if ns == "" {
		ns = "default"
	}
	err := r.Client.Get(ctx, client.ObjectKey{
		Namespace: ns,
		Name:      secretName,
	}, gs)
	if err != nil {
		return "", err
	}
	return string(gs.Data[tempSourceDsn]), nil
}

func (r *DatabaseClaimReconciler) getTargetPasswordFromTempSecret(ctx context.Context, dbClaim *v1.DatabaseClaim) (string, error) {

	secretName := "temp-" + dbClaim.Spec.SecretName

	gs := &corev1.Secret{}

	ns := dbClaim.Namespace
	if ns == "" {
		ns = "default"
	}
	err := r.Client.Get(ctx, client.ObjectKey{
		Namespace: ns,
		Name:      secretName,
	}, gs)
	if err != nil {
		return "", err
	}
	return string(gs.Data[tempTargetPassword]), nil
}

func (r *DatabaseClaimReconciler) getMasterPasswordFromTempSecret(ctx context.Context, dbClaim *v1.DatabaseClaim) (string, error) {

	secretName := "temp-" + dbClaim.Spec.SecretName

	gs := &corev1.Secret{}

	ns := dbClaim.Namespace
	if ns == "" {
		ns = "default"
	}
	err := r.Client.Get(ctx, client.ObjectKey{
		Namespace: ns,
		Name:      secretName,
	}, gs)
	if err != nil {
		return "", err
	}
	return string(gs.Data[cachedMasterPasswdForExistingDB]), nil
}

func (r *DatabaseClaimReconciler) setSourceDsnInTempSecret(ctx context.Context, dsn string, dbClaim *v1.DatabaseClaim) error {

	tSecret, err := r.getTempSecret(ctx, dbClaim)
	if err != nil {
		return err
	}

	log.FromContext(ctx).Info("updating temp secret with source dsn")
	tSecret.Data[tempSourceDsn] = []byte(dsn)
	return r.Client.Update(ctx, tSecret)
}

func (r *DatabaseClaimReconciler) setTargetPasswordInTempSecret(ctx context.Context, password string, dbClaim *v1.DatabaseClaim) error {

	tSecret, err := r.getTempSecret(ctx, dbClaim)
	if err != nil {
		return err
	}

	log.FromContext(ctx).Info("updating temp secret target password")
	tSecret.Data[tempTargetPassword] = []byte(password)
	return r.Client.Update(ctx, tSecret)
}

func (r *DatabaseClaimReconciler) setMasterPasswordInTempSecret(ctx context.Context, password string, dbClaim *v1.DatabaseClaim) error {

	tSecret, err := r.getTempSecret(ctx, dbClaim)
	if err != nil {
		return err
	}

	log.FromContext(ctx).Info("updating temp secret target password")
	tSecret.Data[cachedMasterPasswdForExistingDB] = []byte(password)
	return r.Client.Update(ctx, tSecret)
}

func (r *DatabaseClaimReconciler) getTempSecret(ctx context.Context, dbClaim *v1.DatabaseClaim) (*corev1.Secret, error) {

	logr := log.FromContext(ctx)

	gs := &corev1.Secret{}
	secretName := getTempSecretName(dbClaim)

	err := r.Client.Get(ctx, client.ObjectKey{
		Namespace: dbClaim.Namespace,
		Name:      secretName,
	}, gs)

	if err != nil {
		if !errors.IsNotFound(err) {
			return nil, err
		}
		truePtr := true
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: dbClaim.Namespace,
				Name:      secretName,
				Labels:    map[string]string{"app.kubernetes.io/managed-by": "db-controller"},
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion:         "persistance.atlas.infoblox.com/v1",
						Kind:               "DatabaseClaim",
						Name:               dbClaim.Name,
						UID:                dbClaim.UID,
						Controller:         &truePtr,
						BlockOwnerDeletion: &truePtr,
					},
				},
			},
			Data: map[string][]byte{
				tempTargetPassword:              nil,
				tempSourceDsn:                   nil,
				cachedMasterPasswdForExistingDB: nil,
			},
		}

		logr.Info("creating temp secret", "name", secret.Name, "namespace", secret.Namespace)
		err = r.Client.Create(ctx, secret)
		return secret, err
	} else {
		logr.Info("secret exists returning temp secret", "name", secretName)
		return gs, nil
	}
}

func getTempSecretName(dbClaim *v1.DatabaseClaim) string {
	return "temp-" + dbClaim.Spec.SecretName
}
