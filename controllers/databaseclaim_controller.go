/*


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
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/armon/go-radix"
	"github.com/go-logr/logr"
	_ "github.com/lib/pq"
	gopassword "github.com/sethvargo/go-password/password"
	"github.com/spf13/viper"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	crossplanerds "github.com/crossplane-contrib/provider-aws/apis/rds/v1alpha1"
	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"

	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
	"github.com/infobloxopen/db-controller/pkg/dbclient"
	"github.com/infobloxopen/db-controller/pkg/dbuser"
	"github.com/infobloxopen/db-controller/pkg/metrics"
	"github.com/infobloxopen/db-controller/pkg/pgctl"
	"github.com/infobloxopen/db-controller/pkg/rdsauth"
)

const (
	defaultPassLen = 32
	defaultNumDig  = 10
	defaultNumSimb = 10
	// rotation time in minutes
	minRotationTime          = 60
	maxRotationTime          = 1440
	maxWaitTime              = 10
	defaultRotationTime      = minRotationTime
	serviceNamespaceEnvVar   = "SERVICE_NAMESPACE"
	defaultPostgresStr       = "postgres"
	defaultAuroraPostgresStr = "aurora-postgresql"
)

type ModeEnum int

type input struct {
	FragmentKey      string
	ManageCloudDB    bool
	MasterConnInfo   persistancev1.DatabaseClaimConnectionInfo
	TempSecret       string
	DbHostIdentifier string
	DbType           string
	HostParams       DynamicHostParms
}

const (
	M_NotSupported ModeEnum = iota
	M_UseExistingDB
	M_MigrateToNewDB
	M_MigrationInProgress
	M_UseNewDB
)

// DatabaseClaimReconciler reconciles a DatabaseClaim object
type DatabaseClaimReconciler struct {
	client.Client
	Log                logr.Logger
	Scheme             *runtime.Scheme
	Config             *viper.Viper
	MasterAuth         *rdsauth.MasterAuth
	DbIdentifierPrefix string
	Mode               ModeEnum
	Input              *input
	Class              string
}
type DynamicHostParms struct {
	Engine                          string
	Shape                           string
	MinStorageGB                    int
	EngineVersion                   string
	MasterUsername                  string
	SkipFinalSnapshotBeforeDeletion bool
	PubliclyAccessible              bool
	EnableIAMDatabaseAuthentication bool
	DeletionPolicy                  xpv1.DeletionPolicy
	Port                            int64
}

func (r *DatabaseClaimReconciler) isClassPermitted(claimClass string) bool {
	// r.Log.Info("in isClassPermitted", "claimClass", claimClass, "r.Class", r.Class)
	controllerClass := r.Class

	if claimClass == "" {
		claimClass = "default"
	}
	if controllerClass == "" {
		controllerClass = "default"
	}
	if claimClass != controllerClass {
		return false
	}

	return true
}

func (r *DatabaseClaimReconciler) setMode(dbClaim *persistancev1.DatabaseClaim) {
	logr := r.Log.WithValues("databaseclaim", dbClaim.Namespace+"/"+dbClaim.Name, "func", "setMode")

	if *dbClaim.Spec.UseExistingSource {
		if dbClaim.Spec.SourceDataFrom.Type == "database" {
			r.Mode = M_UseExistingDB
		}
	} else if dbClaim.Spec.SourceDataFrom != nil {
		if dbClaim.Spec.SourceDataFrom.Type == "database" {
			if dbClaim.Status.MigrationState == "" {
				r.Mode = M_MigrateToNewDB
			} else if dbClaim.Status.MigrationState != pgctl.S_Completed.String() {
				r.Mode = M_MigrationInProgress
			} else {
				r.Mode = M_UseNewDB
			}
		}
	} else {
		r.Mode = M_UseNewDB
	}

	logr.Info("successfully selected mode for", "dbclaim", dbClaim.Spec, "selected mode", r.Mode)
}

func (r *DatabaseClaimReconciler) setReqInfo(dbClaim *persistancev1.DatabaseClaim) error {
	logr := r.Log.WithValues("databaseclaim", dbClaim.Namespace+"/"+dbClaim.Name, "func", "setReqInfo")

	r.Input = &input{}
	var (
		fragmentKey      string
		err              error
		createCloudDB    bool
		dbHostIdentifier string
		port             int
	)
	hostParams := DynamicHostParms{}

	if dbClaim.Spec.InstanceLabel != "" {
		fragmentKey, err = r.matchInstanceLabel(dbClaim)
		if err != nil {
			return err
		}
	}
	connInfo := r.getClientConn(fragmentKey, dbClaim)
	if connInfo.Port == "" {
		return fmt.Errorf("cannot get master port")
	}

	if port, err = strconv.Atoi(connInfo.Port); err != nil {
		return fmt.Errorf("invalid master port")
	}
	hostParams.Port = int64(port)

	if connInfo.Username == "" {
		return fmt.Errorf("invalid credentials (username)")
	}
	if connInfo.SSLMode == "" {
		return fmt.Errorf("invalid sslMode")
	}
	if connInfo.DatabaseName == "" {
		return fmt.Errorf("invalid DatabaseName")
	}
	if connInfo.Host == "" {
		createCloudDB = true
		dbHostIdentifier = r.getDynamicHostName(dbClaim)
	}
	if fragmentKey == "" {
		hostParams.MasterUsername = connInfo.Username
		hostParams.Shape = dbClaim.Spec.Shape
		hostParams.Engine = string(dbClaim.Spec.Type)
		hostParams.MinStorageGB = dbClaim.Spec.MinStorageGB
	} else {
		hostParams.MasterUsername = r.getMasterUser(dbClaim)
		hostParams.EngineVersion = r.Config.GetString(fmt.Sprintf("%s::Engineversion", fragmentKey))
		hostParams.Shape = r.Config.GetString(fmt.Sprintf("%s::shape", fragmentKey))
		hostParams.MinStorageGB = r.Config.GetInt(fmt.Sprintf("%s::minStorageGB", fragmentKey))
	}

	if hostParams.EngineVersion == "" {
		hostParams.EngineVersion = r.Config.GetString("defaultEngineVersion")
	}

	if hostParams.Shape == "" {
		hostParams.Shape = r.Config.GetString("defaultShape")
	}

	if hostParams.Engine == "" {
		hostParams.Engine = r.Config.GetString("defaultEngine")
	}

	if hostParams.MinStorageGB == 0 {
		hostParams.MinStorageGB = r.Config.GetInt("defaultMinStorageGB")
	}

	if hostParams.Port == 0 {
		hostParams.Port = int64(r.Config.GetInt("defaultMasterPort"))
	}

	// TODO - Implement these for each fragmentKey also
	hostParams.SkipFinalSnapshotBeforeDeletion = r.Config.GetBool("defaultSkipFinalSnapshotBeforeDeletion")
	hostParams.PubliclyAccessible = r.Config.GetBool("defaultPubliclyAccessible")
	if r.Config.GetString("defaultDeletionPolicy") == "delete" {
		hostParams.DeletionPolicy = xpv1.DeletionDelete
	} else {
		hostParams.DeletionPolicy = xpv1.DeletionOrphan
	}

	// TODO - Enable IAM auth based on authSource config
	hostParams.EnableIAMDatabaseAuthentication = false

	r.Input = &input{ManageCloudDB: createCloudDB,
		MasterConnInfo: connInfo, FragmentKey: fragmentKey,
		DbType: string(dbClaim.Spec.Type), DbHostIdentifier: dbHostIdentifier,
		HostParams: hostParams,
	}
	logr.Info("setup values of ", "DatabaseClaimReconciler", r)
	return nil
}

func (r *DatabaseClaimReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logr := r.Log.WithValues("databaseclaim", req.NamespacedName)

	var dbClaim persistancev1.DatabaseClaim
	if err := r.Get(ctx, req.NamespacedName, &dbClaim); err != nil {
		logr.Error(err, "unable to fetch DatabaseClaim")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if permitted := r.isClassPermitted(*dbClaim.Spec.Class); !permitted {
		logr.Info("ignoring this claim as this controller does not own this class", "claimClass", *dbClaim.Spec.Class, "controllerClas", r.Class)
		return ctrl.Result{}, nil
	}

	r.setReqInfo(&dbClaim)
	// name of our custom finalizer
	dbFinalizerName := "databaseclaims.persistance.atlas.infoblox.com/finalizer"

	// examine DeletionTimestamp to determine if object is under deletion
	if dbClaim.ObjectMeta.DeletionTimestamp.IsZero() {
		// The object is not being deleted, so if it does not have our finalizer,
		// then lets add the finalizer and update the object. This is equivalent
		// registering our finalizer.
		if !controllerutil.ContainsFinalizer(&dbClaim, dbFinalizerName) {
			controllerutil.AddFinalizer(&dbClaim, dbFinalizerName)
			if err := r.Update(ctx, &dbClaim); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		// The object is being deleted
		if controllerutil.ContainsFinalizer(&dbClaim, dbFinalizerName) {
			// our finalizer is present, so lets handle any external dependency
			if err := r.deleteExternalResources(ctx, &dbClaim); err != nil {
				// if fail to delete the external dependency here, return with error
				// so that it can be retried
				return ctrl.Result{}, err
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

	return r.updateStatus(ctx, &dbClaim)
}

func (r *DatabaseClaimReconciler) updateStatus(ctx context.Context, dbClaim *persistancev1.DatabaseClaim) (ctrl.Result, error) {
	logr := r.Log.WithValues("databaseclaim", dbClaim.Namespace+"/"+dbClaim.Name)

	if dbClaim.Status.ActiveDB.ConnectionInfo == nil {
		dbClaim.Status.ActiveDB.ConnectionInfo = new(persistancev1.DatabaseClaimConnectionInfo)
	}
	if dbClaim.Status.NewDB.ConnectionInfo == nil {
		dbClaim.Status.NewDB.ConnectionInfo = new(persistancev1.DatabaseClaimConnectionInfo)
	}

	r.setMode(dbClaim)

	if r.Mode == M_UseExistingDB {
		logr.Info("existing db reconcile started")
		err := r.reconcileUseExistingDB(ctx, dbClaim)
		if err != nil {
			return r.manageError(ctx, dbClaim, err)
		}
		logr.Info("existing db reconcile complete")
		return r.manageSuccess(ctx, dbClaim)
	}
	if r.Mode == M_MigrateToNewDB {
		logr.Info("migrate to new  db reconcile started")
		//check if existingDB has been already reconciled, else reconcileUseExisitngDB
		existing_db_conn, err := persistancev1.ParseDsn(dbClaim.Spec.SourceDataFrom.Database.DSN)
		if err != nil {
			return r.manageError(ctx, dbClaim, err)
		}
		if (dbClaim.Status.ActiveDB.ConnectionInfo.DatabaseName != existing_db_conn.DatabaseName) ||
			(dbClaim.Status.ActiveDB.ConnectionInfo.Host != existing_db_conn.Host) {

			logr.Info("existing db was not reconciled, calling reconcileUseExisitngDB before reconcileUseExisitngDB")

			err := r.reconcileUseExistingDB(ctx, dbClaim)
			if err != nil {
				return r.manageError(ctx, dbClaim, err)
			}
		}

		return r.reconcileMigrateToNewDB(ctx, dbClaim)
	}
	if r.Mode == M_MigrationInProgress {

		return r.reconcileMigrationInProgress(ctx, dbClaim)
	}
	if r.Mode == M_UseNewDB {
		logr.Info("Use new DB")
		//r.setReqInfo(dbClaim)
		result, err := r.reconcileNewDB(ctx, dbClaim)
		if err != nil {
			return r.manageError(ctx, dbClaim, err)
		}
		if result.Requeue {
			logr.Info("requeuing request")
			return result, nil
		}
		newDBConnInfo := dbClaim.Status.NewDB.ConnectionInfo.DeepCopy()
		newDBConnInfo.Password = r.Input.TempSecret

		if err := r.createOrUpdateSecret(ctx, dbClaim, newDBConnInfo); err != nil {
			return r.manageError(ctx, dbClaim, err)
		}
		dbClaim.Status.ActiveDB = *dbClaim.Status.NewDB.DeepCopy()
		dbClaim.Status.NewDB = persistancev1.Status{ConnectionInfo: &persistancev1.DatabaseClaimConnectionInfo{}}

		return r.manageSuccess(ctx, dbClaim)
	}

	logr.Info("unhandled mode")
	return r.manageError(ctx, dbClaim, fmt.Errorf("unhandled mode"))

}

func (r *DatabaseClaimReconciler) reconcileUseExistingDB(ctx context.Context, dbClaim *persistancev1.DatabaseClaim) error {
	logr := r.Log.WithValues("databaseclaim", dbClaim.Namespace+"/"+dbClaim.Name, "func", "reconcileUseExisitngDB")

	existingDBConnInfo, err := persistancev1.ParseDsn(dbClaim.Spec.SourceDataFrom.Database.DSN)
	if err != nil {
		return err
	}
	logr.Info("creating database client")
	dbClient, err := r.getClientForExistingDB(ctx, logr, dbClaim, existingDBConnInfo)
	if err != nil {
		logr.Error(err, "creating database client error")
		return err
	}
	defer dbClient.Close()

	logr.Info(fmt.Sprintf("processing DBClaim: %s namespace: %s AppID: %s", dbClaim.Name, dbClaim.Namespace, dbClaim.Spec.AppID))

	dbName := existingDBConnInfo.DatabaseName
	updateDBStatus(&dbClaim.Status.ActiveDB, dbName)

	err = r.manageUser(dbClient, &dbClaim.Status.ActiveDB, dbName, dbClaim.Spec.Username)
	if err != nil {
		return err
	}
	if err := r.Status().Update(ctx, dbClaim); err != nil {
		logr.Error(err, "could not update db claim")
		return err
	} // create connection info secret
	if r.Input.TempSecret != "" {
		logr.Info("password reset. updating secret")
		activeDBConnInfo := dbClaim.Status.ActiveDB.ConnectionInfo.DeepCopy()
		activeDBConnInfo.Password = r.Input.TempSecret

		if err := r.createOrUpdateSecret(ctx, dbClaim, activeDBConnInfo); err != nil {
			return err
		}
	}
	return nil
}

func (r *DatabaseClaimReconciler) reconcileNewDB(ctx context.Context,
	dbClaim *persistancev1.DatabaseClaim) (ctrl.Result, error) {

	logr := r.Log.WithValues("databaseclaim", dbClaim.Namespace+"/"+dbClaim.Name, "func", "reconcileNewDB")
	logr.Info("reconcileNewDB", "r.Input", r.Input)

	if r.Input.ManageCloudDB {
		isReady, err := r.manageCloudHost(ctx, dbClaim)
		if err != nil {
			return ctrl.Result{}, err
		}
		if !isReady {
			logr.Info("cloud instance provioning is in progress", "instance name", r.Input.DbHostIdentifier, "next-step", "requeueing")
			return ctrl.Result{RequeueAfter: r.getDynamicHostWaitTime(), Requeue: true}, nil
		}
		logr.Info("cloud instance ready. reading generated master secret")
		connInfo, err := r.readResourceSecret(ctx, dbClaim)
		if err != nil {
			logr.Info("unable to read the complete secret. requeueing")
			return ctrl.Result{RequeueAfter: r.getDynamicHostWaitTime(), Requeue: true}, nil
		}
		r.Input.MasterConnInfo.Host = connInfo.Host
		r.Input.MasterConnInfo.Password = connInfo.Password
		r.Input.MasterConnInfo.Port = connInfo.Port
		r.Input.MasterConnInfo.Username = connInfo.Username

	} else {
		password, err := r.readMasterPassword(ctx, dbClaim)
		if err != nil {
			return r.manageError(ctx, dbClaim, err)
		}
		// password := "postgres"
		r.Input.MasterConnInfo.Password = password
	}

	dbClient, err := r.getDBClient(dbClaim)
	if err != nil {
		logr.Error(err, "creating database client error")
		return ctrl.Result{}, err
	}
	defer dbClient.Close()

	if dbClaim.Status.NewDB.ConnectionInfo.Host == dbClaim.Status.ActiveDB.ConnectionInfo.Host {
		dbClaim.Status.NewDB.UserUpdatedAt = dbClaim.Status.ActiveDB.UserUpdatedAt
		dbClaim.Status.NewDB.DbCreatedAt = dbClaim.Status.ActiveDB.DbCreatedAt
		dbClaim.Status.NewDB.ConnectionInfoUpdatedAt = dbClaim.Status.ActiveDB.ConnectionInfoUpdatedAt
	}

	if err := r.manageDatabase(dbClient, &dbClaim.Status.NewDB); err != nil {
		return ctrl.Result{}, err

	}

	err = r.manageUser(dbClient, &dbClaim.Status.NewDB, GetDBName(dbClaim), dbClaim.Spec.Username)
	if err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *DatabaseClaimReconciler) reconcileMigrateToNewDB(ctx context.Context,
	dbClaim *persistancev1.DatabaseClaim) (ctrl.Result, error) {

	result, err := r.reconcileNewDB(ctx, dbClaim)
	if err != nil {
		return r.manageError(ctx, dbClaim, err)
	}
	if result.Requeue {
		return result, nil
	}
	//store a temp secret to beused by migration process
	//removing the practice of storing the secret in status
	if r.Input.TempSecret != "" {
		r.createTempSecret(ctx, r.Input.TempSecret, dbClaim)
	}

	return r.reconcileMigrationInProgress(ctx, dbClaim)
}

func (r *DatabaseClaimReconciler) reconcileMigrationInProgress(ctx context.Context,
	dbClaim *persistancev1.DatabaseClaim) (ctrl.Result, error) {
	logr := r.Log.WithValues("databaseclaim", dbClaim.Namespace+"/"+dbClaim.Name, "func", "reconcileMigrationInProgress")

	migrationState := dbClaim.Status.MigrationState

	logr.Info("Migration is progress", "state", migrationState)

	logr.Info("cloud instance ready. reading generated master secret")
	connInfo, err := r.readResourceSecret(ctx, dbClaim)
	if err != nil {
		logr.Info("unable to read the complete secret. requeueing")
		return ctrl.Result{RequeueAfter: r.getDynamicHostWaitTime(), Requeue: true}, nil
	}
	r.Input.MasterConnInfo.Host = connInfo.Host
	r.Input.MasterConnInfo.Password = connInfo.Password
	r.Input.MasterConnInfo.Port = connInfo.Port
	r.Input.MasterConnInfo.Username = connInfo.Username

	target_master_dsn := r.Input.MasterConnInfo.Dsn()
	target_app_conn := dbClaim.Status.NewDB.ConnectionInfo.DeepCopy()
	target_app_conn.Password, err = r.getTempPasswordFromSecret(ctx, dbClaim)
	if err != nil {
		return r.manageError(ctx, dbClaim, err)
	}

	source_master_conn, err := persistancev1.ParseDsn(dbClaim.Spec.SourceDataFrom.Database.DSN)
	if err != nil {
		return r.manageError(ctx, dbClaim, err)
	}
	source_master_conn.Password, err = r.getSrcAdminPasswdFromSecret(ctx, dbClaim)
	if err != nil {
		return r.manageError(ctx, dbClaim, err)
	}

	source_app_conn := dbClaim.Status.ActiveDB.ConnectionInfo.DeepCopy()
	source_app_conn.Password, err = r.getSrcAppPasswdFromSecret(ctx, dbClaim)
	if err != nil {
		return r.manageError(ctx, dbClaim, err)
	}

	config := pgctl.Config{
		Log:              r.Log,
		SourceDBAdminDsn: source_master_conn.Dsn(),
		SourceDBUserDsn:  source_app_conn.Dsn(),
		TargetDBUserDsn:  target_app_conn.Dsn(),
		TargetDBAdminDsn: target_master_dsn,
		ExportFilePath:   r.Config.GetString("pgTemp"),
	}

	logr.Info("DSN", "config", config)

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
			logr.Info("Completed Migration")
			break loop

		case pgctl.S_Retry:
			logr.Info("Retry called")
			return ctrl.Result{RequeueAfter: 60 * time.Second, Requeue: true}, nil

		case pgctl.S_RerouteTargetSecret:
			s = next
			dbClaim.Status.MigrationState = s.String()
			if err := r.Status().Update(ctx, dbClaim); err != nil {
				logr.Error(err, "could not update db claim status")
				return r.manageError(ctx, dbClaim, err)
			}
			if err := r.createOrUpdateSecret(ctx, dbClaim, target_app_conn); err != nil {
				return r.manageError(ctx, dbClaim, err)
			}

		default:
			s = next
			dbClaim.Status.MigrationState = s.String()
			if err := r.Status().Update(ctx, dbClaim); err != nil {
				logr.Error(err, "could not update db claim status")
				return r.manageError(ctx, dbClaim, err)
			}
		}
	}
	dbClaim.Status.MigrationState = pgctl.S_Completed.String()

	//done with migration- switch active server to newDB
	dbClaim.Status.ActiveDB = *dbClaim.Status.NewDB.DeepCopy()
	dbClaim.Status.NewDB = persistancev1.Status{ConnectionInfo: &persistancev1.DatabaseClaimConnectionInfo{}}

	if err := r.Status().Update(ctx, dbClaim); err != nil {
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

func (r *DatabaseClaimReconciler) getClientForExistingDB(ctx context.Context, logr logr.Logger,
	dbClaim *persistancev1.DatabaseClaim, connInfo *persistancev1.DatabaseClaimConnectionInfo) (dbclient.Client, error) {

	secretKey := "password"
	gs := &corev1.Secret{}

	if connInfo.Port == "" {
		return nil, fmt.Errorf("cannot get master port")
	}

	if connInfo.Username == "" {
		return nil, fmt.Errorf("invalid credentials (username)")
	}

	if connInfo.SSLMode == "" {
		return nil, fmt.Errorf("invalid sslMode")
	}

	r.Log.Info("using credentials from secret")

	ns := dbClaim.Spec.SourceDataFrom.Database.SecretRef.Namespace
	if ns == "" {
		ns = "default"
	}
	err := r.Client.Get(ctx, client.ObjectKey{
		Namespace: ns,
		Name:      dbClaim.Spec.SourceDataFrom.Database.SecretRef.Name,
	}, gs)
	if err != nil {
		return nil, err
	}
	connInfo.Password = string(gs.Data[secretKey])

	if connInfo.Password == "" {
		return nil, fmt.Errorf("invalid credentials (password)")
	}
	updateHostPortStatus(&dbClaim.Status.ActiveDB, connInfo.Host, connInfo.Port, connInfo.SSLMode)

	return dbclient.New(dbclient.Config{Log: r.Log, DBType: "postgres", DSN: connInfo.Dsn()})
}

func (r *DatabaseClaimReconciler) getClientConn(fragmentKey string, dbClaim *persistancev1.DatabaseClaim) persistancev1.DatabaseClaimConnectionInfo {
	connInfo := persistancev1.DatabaseClaimConnectionInfo{}

	connInfo.Host = r.getMasterHost(dbClaim)
	connInfo.Port = r.getMasterPort(dbClaim)
	connInfo.Username = r.getMasterUser(dbClaim)
	connInfo.SSLMode = r.getSSLMode(dbClaim)
	connInfo.DatabaseName = GetDBName(dbClaim)
	return connInfo
}

func (r *DatabaseClaimReconciler) getDBClient(dbClaim *persistancev1.DatabaseClaim) (dbclient.Client, error) {
	logr := r.Log.WithValues("databaseclaim", dbClaim.Namespace+"/"+dbClaim.Name, "func", "getDBClient")

	logr.Info("getting dbclient", "dsn", r.getMasterDefaultDsn())
	updateHostPortStatus(&dbClaim.Status.NewDB, r.Input.MasterConnInfo.Host, r.Input.MasterConnInfo.Port, r.Input.MasterConnInfo.SSLMode)
	return dbclient.New(dbclient.Config{Log: r.Log, DBType: "postgres", DSN: r.getMasterDefaultDsn()})
}

func (r *DatabaseClaimReconciler) getMasterDefaultDsn() string {

	return fmt.Sprintf("%s://%s:%s@%s:%s/%s?sslmode=%s", "postgres",
		r.Input.MasterConnInfo.Username, r.Input.MasterConnInfo.Password,
		r.Input.MasterConnInfo.Host, r.Input.MasterConnInfo.Port,
		"postgres", r.Input.MasterConnInfo.SSLMode)
}

func (r *DatabaseClaimReconciler) getReclaimPolicy(fragmentKey string) string {
	defaultReclaimPolicy := r.Config.GetString("defaultReclaimPolicy")

	if fragmentKey == "" {
		return defaultReclaimPolicy
	}

	reclaimPolicy := r.Config.GetString(fmt.Sprintf("%s::reclaimPolicy", fragmentKey))

	if reclaimPolicy == "retain" || (reclaimPolicy == "" && defaultReclaimPolicy == "retain") {
		// Don't need to delete
		return "retain"
	} else {
		// Assume reclaimPolicy == "delete"
		return "delete"
	}
}

func (r *DatabaseClaimReconciler) deleteExternalResources(ctx context.Context, dbClaim *persistancev1.DatabaseClaim) error {
	// delete any external resources associated with the dbClaim
	// Only RDS Instance are managed for now

	if r.Input.ManageCloudDB {

		fragmentKey := dbClaim.Spec.InstanceLabel
		reclaimPolicy := r.getReclaimPolicy(fragmentKey)

		if reclaimPolicy == "delete" {
			dbHostName := r.getDynamicHostName(dbClaim)
			pgName := r.getParameterGroupName(ctx, dbClaim)
			if fragmentKey == "" {
				// Delete
				if err := r.deleteCloudDatabase(dbHostName, ctx); err != nil {
					return err
				}
				return r.deleteParameterGroup(ctx, pgName)

			} else {
				// Check there is no other Claims that use this fragment
				var dbClaimList persistancev1.DatabaseClaimList
				if err := r.List(ctx, &dbClaimList, client.MatchingFields{instanceLableKey: dbClaim.Spec.InstanceLabel}); err != nil {
					return err
				}

				if len(dbClaimList.Items) == 1 {
					// Delete
					if err := r.deleteCloudDatabase(dbHostName, ctx); err != nil {
						return err
					}
					return r.deleteParameterGroup(ctx, pgName)

				}
			}
		}
		// else reclaimPolicy == "retain" nothing to do!
	}

	return nil
}

func (r *DatabaseClaimReconciler) generatePassword() (string, error) {
	var pass string
	var err error
	minPasswordLength := r.getMinPasswordLength()
	complEnabled := r.isPasswordComplexity()

	if complEnabled {
		count := minPasswordLength / 4
		pass, err = gopassword.Generate(minPasswordLength, count, count, false, false)
		if err != nil {
			return "", err
		}
	} else {
		pass, err = gopassword.Generate(defaultPassLen, defaultNumDig, defaultNumSimb, false, false)
		if err != nil {
			return "", err
		}
	}

	return pass, nil
}

var (
	instanceLableKey = ".spec.instanceLabel"
	// apiGVStr         = persistancev1.GroupVersion.String()
)

func (r *DatabaseClaimReconciler) SetupWithManager(mgr ctrl.Manager) error {

	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &persistancev1.DatabaseClaim{}, instanceLableKey, func(rawObj client.Object) []string {
		// grab the DatabaseClaim object, extract the InstanceLabel for index...
		claim := rawObj.(*persistancev1.DatabaseClaim)
		return []string{claim.Spec.InstanceLabel}
	}); err != nil {
		return err
	}

	pred := predicate.GenerationChangedPredicate{}
	return ctrl.NewControllerManagedBy(mgr).
		For(&persistancev1.DatabaseClaim{}).WithEventFilter(pred).
		Complete(r)
}

func (r *DatabaseClaimReconciler) getMasterHost(dbClaim *persistancev1.DatabaseClaim) string {
	// If config host is overridden by db claims host
	if dbClaim.Spec.Host != "" {
		return dbClaim.Spec.Host
	}
	return r.Config.GetString(fmt.Sprintf("%s::Host", r.Input.FragmentKey))
}

func (r *DatabaseClaimReconciler) getMasterUser(dbClaim *persistancev1.DatabaseClaim) string {

	u := r.Config.GetString(fmt.Sprintf("%s::masterUsername", r.Input.FragmentKey))
	if u != "" {
		return u
	}
	return r.Config.GetString("defaultMasterUsername")
}

func (r *DatabaseClaimReconciler) getMasterPort(dbClaim *persistancev1.DatabaseClaim) string {

	if dbClaim.Spec.Port != "" {
		return dbClaim.Spec.Port
	}

	p := r.Config.GetString(fmt.Sprintf("%s::Port", r.Input.FragmentKey))
	if p != "" {
		return p
	}

	return r.Config.GetString("defaultMasterPort")
}

func (r *DatabaseClaimReconciler) getSSLMode(dbClaim *persistancev1.DatabaseClaim) string {

	s := r.Config.GetString(fmt.Sprintf("%s::sslMode", r.Input.FragmentKey))
	if s != "" {
		return s
	}

	return r.Config.GetString("defaultSslMode")
}

func (r *DatabaseClaimReconciler) getPasswordRotationTime() time.Duration {
	prt := r.Config.GetInt("passwordconfig::passwordRotationPeriod")
	if prt < minRotationTime || prt > maxRotationTime {
		r.Log.Info("password rotation time is out of range, should be between 60 and 1440 min, use the default")
		return time.Duration(defaultRotationTime) * time.Minute
	}

	return time.Duration(prt) * time.Minute
}

func (r *DatabaseClaimReconciler) isPasswordComplexity() bool {
	complEnabled := r.Config.GetString("passwordconfig::passwordComplexity")

	return complEnabled == "enabled"
}

func (r *DatabaseClaimReconciler) getMinPasswordLength() int {
	return r.Config.GetInt("passwordconfig::minPasswordLength")
}

func (r *DatabaseClaimReconciler) getSecretRef(fragmentKey string) string {
	return r.Config.GetString(fmt.Sprintf("%s::PasswordSecretRef", fragmentKey))
}

func (r *DatabaseClaimReconciler) getSecretKey(fragmentKey string) string {
	return r.Config.GetString(fmt.Sprintf("%s::PasswordSecretKey", fragmentKey))
}

// func (r *DatabaseClaimReconciler) getAuthSource() string {
// 	return r.Config.GetString("authSource")
// }

func (r *DatabaseClaimReconciler) getRegion() string {
	return r.Config.GetString("region")
}

func (r *DatabaseClaimReconciler) getMultiAZEnabled() bool {
	return r.Config.GetBool("multiAZEnabled")
}

func (r *DatabaseClaimReconciler) getVpcSecurityGroupIDRefs() string {
	return r.Config.GetString("vpcSecurityGroupIDRefs")
}

func (r *DatabaseClaimReconciler) getDbSubnetGroupNameRef() string {
	return r.Config.GetString("dbSubnetGroupNameRef")
}

func (r *DatabaseClaimReconciler) getDynamicHostWaitTime() time.Duration {
	t := r.Config.GetInt("dynamicHostWaitTimeMin")
	if t > maxWaitTime {
		msg := "dynamic host wait time is out of range, should be between 1 " + strconv.Itoa(maxWaitTime) + " min"
		r.Log.Info(msg)
		return time.Minute
	}

	return time.Duration(t) * time.Minute
}

// FindStatusCondition finds the conditionType in conditions.
func (r *DatabaseClaimReconciler) isResourceReady(resourceStatus xpv1.ResourceStatus) bool {
	conditions := resourceStatus.Conditions
	ready := xpv1.TypeReady
	conditionTrue := corev1.ConditionTrue
	for i := range conditions {
		if conditions[i].Type == ready {
			if conditions[i].Status == conditionTrue {
				return true
			} else {
				return false
			}
		}
	}
	return false
}

func (r *DatabaseClaimReconciler) readResourceSecret(ctx context.Context, dbClaim *persistancev1.DatabaseClaim) (persistancev1.DatabaseClaimConnectionInfo, error) {
	rs := &corev1.Secret{}
	connInfo := persistancev1.DatabaseClaimConnectionInfo{}

	secretName := r.Input.DbHostIdentifier
	serviceNS, _ := getServiceNamespace()

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

	if connInfo.Host == "" ||
		connInfo.Port == "" ||
		connInfo.Username == "" ||
		connInfo.Password == "" {
		return connInfo, fmt.Errorf("generated secret is incomplete")
	}

	return connInfo, nil
}

func (r *DatabaseClaimReconciler) getDynamicHostName(dbClaim *persistancev1.DatabaseClaim) string {
	prefix := "dbc-"

	if r.DbIdentifierPrefix != "" {
		prefix = prefix + r.DbIdentifierPrefix + "-"
	}

	if r.Input.FragmentKey == "" {
		return prefix + dbClaim.Name
	}

	return prefix + r.Input.FragmentKey
}

func (r *DatabaseClaimReconciler) getParameterGroupName(ctx context.Context, dbClaim *persistancev1.DatabaseClaim) string {
	hostName := r.getDynamicHostName(dbClaim)
	params := &r.Input.HostParams

	switch r.Input.DbType {
	case defaultPostgresStr:
		return hostName + "-" + (strings.Split(params.EngineVersion, "."))[0]
	case defaultAuroraPostgresStr:
		return hostName + "-a-" + (strings.Split(params.EngineVersion, "."))[0]
	default:
		return hostName + "-" + (strings.Split(params.EngineVersion, "."))[0]
	}
}

func (r *DatabaseClaimReconciler) manageCloudHost(ctx context.Context, dbClaim *persistancev1.DatabaseClaim) (bool, error) {
	dbHostIdentifier := r.Input.DbHostIdentifier

	if dbClaim.Spec.Type == defaultPostgresStr {
		return r.managePostgresDBInstance(ctx, dbHostIdentifier, dbClaim)
	} else if dbClaim.Spec.Type == defaultAuroraPostgresStr {
		_, err := r.manageDBCluster(ctx, dbHostIdentifier, dbClaim)
		if err != nil {
			return false, err
		}
		r.Log.Info("dbcluster is ready. proceeding to manage dbinstance")
		_, err = r.manageAuroraDBInstance(ctx, dbHostIdentifier, dbClaim, false)
		if err != nil {
			return false, err
		}
		if r.getMultiAZEnabled() {
			_, err = r.manageAuroraDBInstance(ctx, dbHostIdentifier, dbClaim, true)
			if err != nil {
				return false, err
			}
		}
		return true, nil
	}
	return false, fmt.Errorf("unsupported db type requested - %s", dbClaim.Spec.Type)
}
func (r *DatabaseClaimReconciler) manageDatabase(dbClient dbclient.Client, status *persistancev1.Status) error {
	logr := r.Log.WithValues("func", "manageDatabase")

	dbName := r.Input.MasterConnInfo.DatabaseName
	created, err := dbClient.CreateDatabase(dbName)
	if err != nil {
		msg := fmt.Sprintf("error creating database postgresURI %s using %s", dbName, r.Input.MasterConnInfo.Dsn())
		logr.Error(err, msg)
		return err
	} else if created || status.ConnectionInfo.DatabaseName == "" {
		updateDBStatus(status, dbName)
	}
	return nil
}

func (r *DatabaseClaimReconciler) manageUser(dbClient dbclient.Client, status *persistancev1.Status, dbName string, baseUsername string) error {
	logr := r.Log.WithValues("func", "manageUser")

	// baseUsername := dbClaim.Spec.Username
	dbu := dbuser.NewDBUser(baseUsername)
	rotationTime := r.getPasswordRotationTime()

	// create role
	_, err := dbClient.CreateGroup(dbName, baseUsername)
	if err != nil {
		return err
	}

	if dbu.IsUserChanged(*status) {
		oldUsername := dbu.TrimUserSuffix(status.ConnectionInfo.Username)
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
		r.updateUserStatus(status, dbu.GetUserA(), userPassword)
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
		logr.Info("rotating users")

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
		r.updateUserStatus(status, nextUser, userPassword)
	}

	return nil
}

func (r *DatabaseClaimReconciler) manageDBCluster(ctx context.Context, dbHostName string,
	dbClaim *persistancev1.DatabaseClaim) (bool, error) {

	pgName, err := r.manageClusterParamGroup(ctx, dbClaim)
	if err != nil {
		r.Log.Error(err, "parameter group setup failed")
		return false, err
	}

	serviceNS, err := getServiceNamespace()
	if err != nil {
		return false, err
	}

	dbSecretCluster := xpv1.SecretReference{
		Name:      dbHostName,
		Namespace: serviceNS,
	}

	dbMasterSecretCluster := xpv1.SecretKeySelector{
		SecretReference: xpv1.SecretReference{
			Name:      dbHostName + "-master",
			Namespace: serviceNS,
		},
		Key: "password",
	}
	dbCluster := &crossplanerds.DBCluster{}
	providerConfigReference := xpv1.Reference{
		Name: "default",
	}

	params := &r.Input.HostParams

	encryptStrg := true

	err = r.Client.Get(ctx, client.ObjectKey{
		Name: dbHostName,
	}, dbCluster)
	if err != nil {
		if errors.IsNotFound(err) {
			dbCluster = &crossplanerds.DBCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: dbHostName,
					// TODO - Figure out the proper labels for resource
					// Labels:    map[string]string{"app.kubernetes.io/managed-by": "db-controller"},
				},
				Spec: crossplanerds.DBClusterSpec{
					ForProvider: crossplanerds.DBClusterParameters{
						Region: r.getRegion(),
						CustomDBClusterParameters: crossplanerds.CustomDBClusterParameters{
							SkipFinalSnapshot: params.SkipFinalSnapshotBeforeDeletion,
							VPCSecurityGroupIDRefs: []xpv1.Reference{
								{Name: r.getVpcSecurityGroupIDRefs()},
							},
							DBSubnetGroupNameRef: &xpv1.Reference{
								Name: r.getDbSubnetGroupNameRef(),
							},
							AutogeneratePassword:        true,
							MasterUserPasswordSecretRef: &dbMasterSecretCluster,
							DBClusterParameterGroupNameRef: &xpv1.Reference{
								Name: pgName,
							},
						},
						// Items from Claim and fragmentKey
						Engine: &params.Engine,
						Tags:   DBClaimTags(dbClaim.Spec.Tags).DBTags(),
						// Items from Config
						MasterUsername:                  &params.MasterUsername,
						EngineVersion:                   &params.EngineVersion,
						EnableIAMDatabaseAuthentication: &params.EnableIAMDatabaseAuthentication,
						StorageEncrypted:                &encryptStrg,
						Port:                            &params.Port,
					},
					ResourceSpec: xpv1.ResourceSpec{
						WriteConnectionSecretToReference: &dbSecretCluster,
						ProviderConfigReference:          &providerConfigReference,
						DeletionPolicy:                   params.DeletionPolicy,
					},
				},
			}
			r.Log.Info("creating crossplane DBCluster resource", "DBCluster", dbCluster.Name)
			r.Client.Create(ctx, dbCluster)
		} else {
			return false, err
		}
	}
	if !dbCluster.ObjectMeta.DeletionTimestamp.IsZero() {
		err = fmt.Errorf("can not create Cloud DB cluster %s it is being deleted", dbHostName)
		r.Log.Error(err, "dbCluster", "dbHostIdentifier", dbHostName)
		return false, err
	}
	_, err = r.updateDBCluster(ctx, dbClaim, dbCluster)
	if err != nil {
		return false, err
	}

	return r.isResourceReady(dbCluster.Status.ResourceStatus), nil
}

func (r *DatabaseClaimReconciler) managePostgresDBInstance(ctx context.Context, dbHostName string,
	dbClaim *persistancev1.DatabaseClaim) (bool, error) {
	serviceNS, err := getServiceNamespace()
	if err != nil {
		return false, err
	}
	dbSecretInstance := xpv1.SecretReference{
		Name:      dbHostName,
		Namespace: serviceNS,
	}

	dbMasterSecretInstance := xpv1.SecretKeySelector{
		SecretReference: xpv1.SecretReference{
			Name:      dbHostName + "-master",
			Namespace: serviceNS,
		},
		Key: "password",
	}

	pgName, err := r.managePostgresParamGroup(ctx, dbClaim)
	if err != nil {
		r.Log.Error(err, "parameter group setup failed")
		return false, err
	}
	// Infrastructure Config
	region := r.getRegion()
	providerConfigReference := xpv1.Reference{
		Name: "default",
	}

	dbInstance := &crossplanerds.DBInstance{}

	params := &r.Input.HostParams
	ms64 := int64(params.MinStorageGB)
	multiAZ := r.getMultiAZEnabled()
	perfIns := true
	encryptStrg := true

	err = r.Client.Get(ctx, client.ObjectKey{
		Name: dbHostName,
	}, dbInstance)
	if err != nil {
		if errors.IsNotFound(err) {
			dbInstance = &crossplanerds.DBInstance{
				ObjectMeta: metav1.ObjectMeta{
					Name: dbHostName,
					// TODO - Figure out the proper labels for resource
					// Labels:    map[string]string{"app.kubernetes.io/managed-by": "db-controller"},
				},
				Spec: crossplanerds.DBInstanceSpec{
					ForProvider: crossplanerds.DBInstanceParameters{
						Region: region,
						CustomDBInstanceParameters: crossplanerds.CustomDBInstanceParameters{
							SkipFinalSnapshot: params.SkipFinalSnapshotBeforeDeletion,
							VPCSecurityGroupIDRefs: []xpv1.Reference{
								{Name: r.getVpcSecurityGroupIDRefs()},
							},
							DBSubnetGroupNameRef: &xpv1.Reference{
								Name: r.getDbSubnetGroupNameRef(),
							},
							DBParameterGroupNameRef: &xpv1.Reference{
								Name: pgName,
							},
							AutogeneratePassword:        true,
							MasterUserPasswordSecretRef: &dbMasterSecretInstance,
						},
						// Items from Claim and fragmentKey
						Engine:           &params.Engine,
						MultiAZ:          &multiAZ,
						DBInstanceClass:  &params.Shape,
						AllocatedStorage: &ms64,
						Tags:             DBClaimTags(dbClaim.Spec.Tags).DBTags(),
						// Items from Config
						MasterUsername:                  &params.MasterUsername,
						EngineVersion:                   &params.EngineVersion,
						PubliclyAccessible:              &params.PubliclyAccessible,
						EnableIAMDatabaseAuthentication: &params.EnableIAMDatabaseAuthentication,
						EnablePerformanceInsights:       &perfIns,
						StorageEncrypted:                &encryptStrg,
						Port:                            &params.Port,
					},
					ResourceSpec: xpv1.ResourceSpec{
						WriteConnectionSecretToReference: &dbSecretInstance,
						ProviderConfigReference:          &providerConfigReference,
						DeletionPolicy:                   params.DeletionPolicy,
					},
				},
			}

			r.Log.Info("creating crossplane DBInstance resource", "DBInstance", dbInstance.Name)

			r.Client.Create(ctx, dbInstance)
		} else {
			//not errors.IsNotFound(err) {
			return false, err
		}
	}

	// Deletion is long running task check that is not being deleted.
	if !dbInstance.ObjectMeta.DeletionTimestamp.IsZero() {
		err = fmt.Errorf("can not create Cloud DB instance %s it is being deleted", dbHostName)
		r.Log.Error(err, "DBInstance", "dbHostIdentifier", dbHostName)
		return false, err
	}

	_, err = r.updateDBInstance(ctx, dbClaim, dbInstance)
	if err != nil {
		return false, err
	}

	return r.isResourceReady(dbInstance.Status.ResourceStatus), nil
}

func (r *DatabaseClaimReconciler) manageAuroraDBInstance(ctx context.Context, dbHostName string,
	dbClaim *persistancev1.DatabaseClaim, isReader bool) (bool, error) {
	// Infrastructure Config
	region := r.getRegion()
	providerConfigReference := xpv1.Reference{
		Name: "default",
	}
	pgName, err := r.manageAuroraPostgresParamGroup(ctx, dbClaim)
	if err != nil {
		r.Log.Error(err, "parameter group setup failed")
		return false, err
	}
	dbClusterIdentifier := dbHostName
	if isReader {
		dbHostName = dbHostName + "-reader"
	}
	dbInstance := &crossplanerds.DBInstance{}

	params := &r.Input.HostParams
	perfIns := true

	err = r.Client.Get(ctx, client.ObjectKey{
		Name: dbHostName,
	}, dbInstance)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("aurora db instance not found. creating now")
			dbInstance = &crossplanerds.DBInstance{
				ObjectMeta: metav1.ObjectMeta{
					Name: dbHostName,
					// TODO - Figure out the proper labels for resource
					// Labels:    map[string]string{"app.kubernetes.io/managed-by": "db-controller"},
				},
				Spec: crossplanerds.DBInstanceSpec{
					ForProvider: crossplanerds.DBInstanceParameters{
						Region: region,
						CustomDBInstanceParameters: crossplanerds.CustomDBInstanceParameters{
							SkipFinalSnapshot: params.SkipFinalSnapshotBeforeDeletion,
						},
						DBParameterGroupName: &pgName,
						// Items from Claim and fragmentKey
						Engine:          &params.Engine,
						DBInstanceClass: &params.Shape,
						Tags:            DBClaimTags(dbClaim.Spec.Tags).DBTags(),
						// Items from Config
						EngineVersion:             &params.EngineVersion,
						PubliclyAccessible:        &params.PubliclyAccessible,
						DBClusterIdentifier:       &dbClusterIdentifier,
						EnablePerformanceInsights: &perfIns,
					},
					ResourceSpec: xpv1.ResourceSpec{
						ProviderConfigReference: &providerConfigReference,
						DeletionPolicy:          params.DeletionPolicy,
					},
				},
			}

			r.Log.Info("creating crossplane DBInstance resource", "DBInstance", dbInstance.Name)

			r.Client.Create(ctx, dbInstance)
		} else {
			//not errors.IsNotFound(err) {
			return false, err
		}
	}

	// Deletion is long running task check that is not being deleted.
	if !dbInstance.ObjectMeta.DeletionTimestamp.IsZero() {
		err = fmt.Errorf("can not create Cloud DB instance %s it is being deleted", dbHostName)
		r.Log.Error(err, "DBInstance", "dbHostIdentifier", dbHostName)
		return false, err
	}

	_, err = r.updateDBInstance(ctx, dbClaim, dbInstance)
	if err != nil {
		return false, err
	}

	return r.isResourceReady(dbInstance.Status.ResourceStatus), nil
}

func (r *DatabaseClaimReconciler) managePostgresParamGroup(ctx context.Context, dbClaim *persistancev1.DatabaseClaim) (string, error) {

	logical := "rds.logical_replication"
	one := "1"
	immediate := "immediate"
	reboot := "pending-reboot"
	forceSsl := "rds.force_ssl"
	transactionTimeout := "idle_in_transaction_session_timeout"
	transactionTimeoutValue := "300000"
	params := &r.Input.HostParams
	pgName := r.getParameterGroupName(ctx, dbClaim)
	sharedLib := "shared_preload_libraries"
	sharedLibValue := "pg_stat_statements,pg_cron"
	cron := "cron.database_name"
	cronValue := r.Input.MasterConnInfo.DatabaseName
	desc := "custom PG for " + pgName

	providerConfigReference := xpv1.Reference{
		Name: "default",
	}

	dbParamGroup := &crossplanerds.DBParameterGroup{}

	err := r.Client.Get(ctx, client.ObjectKey{
		Name: pgName,
	}, dbParamGroup)
	if err != nil {
		if errors.IsNotFound(err) {
			dbParamGroup = &crossplanerds.DBParameterGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name: pgName,
				},
				Spec: crossplanerds.DBParameterGroupSpec{
					ForProvider: crossplanerds.DBParameterGroupParameters{
						Region:      r.getRegion(),
						Description: &desc,
						CustomDBParameterGroupParameters: crossplanerds.CustomDBParameterGroupParameters{
							DBParameterGroupFamilySelector: &crossplanerds.DBParameterGroupFamilyNameSelector{
								Engine:        params.Engine,
								EngineVersion: &params.EngineVersion,
							},
							Parameters: []crossplanerds.Parameter{
								{ParameterName: &logical,
									ParameterValue: &one,
									ApplyMethod:    &reboot,
								},
								{ParameterName: &forceSsl,
									ParameterValue: &one,
									ApplyMethod:    &immediate,
								},
								{ParameterName: &transactionTimeout,
									ParameterValue: &transactionTimeoutValue,
									ApplyMethod:    &immediate,
								},
								{ParameterName: &sharedLib,
									ParameterValue: &sharedLibValue,
									ApplyMethod:    &reboot,
								},
								{ParameterName: &cron,
									ParameterValue: &cronValue,
									ApplyMethod:    &reboot,
								},
							},
						},
					},
					ResourceSpec: xpv1.ResourceSpec{
						ProviderConfigReference: &providerConfigReference,
						DeletionPolicy:          params.DeletionPolicy,
					},
				},
			}
			r.Log.Info("creating crossplane DBParameterGroup resource", "DBParameterGroup", dbParamGroup.Name)

			err = r.Client.Create(ctx, dbParamGroup)
			if err != nil {
				return pgName, err
			}

		} else {
			//not errors.IsNotFound(err) {
			return pgName, err
		}
	}
	return pgName, nil
}
func (r *DatabaseClaimReconciler) manageAuroraPostgresParamGroup(ctx context.Context, dbClaim *persistancev1.DatabaseClaim) (string, error) {

	immediate := "immediate"
	reboot := "pending-reboot"
	transactionTimeout := "idle_in_transaction_session_timeout"
	transactionTimeoutValue := "300000"
	params := &r.Input.HostParams
	pgName := r.getParameterGroupName(ctx, dbClaim)
	sharedLib := "shared_preload_libraries"
	sharedLibValue := "pg_stat_statements,pg_cron"
	cron := "cron.database_name"
	cronValue := r.Input.MasterConnInfo.DatabaseName
	desc := "custom PG for " + pgName

	providerConfigReference := xpv1.Reference{
		Name: "default",
	}

	dbParamGroup := &crossplanerds.DBParameterGroup{}

	err := r.Client.Get(ctx, client.ObjectKey{
		Name: pgName,
	}, dbParamGroup)
	if err != nil {
		if errors.IsNotFound(err) {
			dbParamGroup = &crossplanerds.DBParameterGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name: pgName,
				},
				Spec: crossplanerds.DBParameterGroupSpec{
					ForProvider: crossplanerds.DBParameterGroupParameters{
						Region:      r.getRegion(),
						Description: &desc,
						CustomDBParameterGroupParameters: crossplanerds.CustomDBParameterGroupParameters{
							DBParameterGroupFamilySelector: &crossplanerds.DBParameterGroupFamilyNameSelector{
								Engine:        params.Engine,
								EngineVersion: &params.EngineVersion,
							},
							Parameters: []crossplanerds.Parameter{
								{ParameterName: &transactionTimeout,
									ParameterValue: &transactionTimeoutValue,
									ApplyMethod:    &immediate,
								},
								{ParameterName: &sharedLib,
									ParameterValue: &sharedLibValue,
									ApplyMethod:    &reboot,
								},
								{ParameterName: &cron,
									ParameterValue: &cronValue,
									ApplyMethod:    &reboot,
								},
							},
						},
					},
					ResourceSpec: xpv1.ResourceSpec{
						ProviderConfigReference: &providerConfigReference,
						DeletionPolicy:          params.DeletionPolicy,
					},
				},
			}
			r.Log.Info("creating crossplane DBParameterGroup resource", "DBParameterGroup", dbParamGroup.Name)

			err = r.Client.Create(ctx, dbParamGroup)
			if err != nil {
				return pgName, err
			}

		} else {
			//not errors.IsNotFound(err) {
			return pgName, err
		}
	}
	return pgName, nil
}

func (r *DatabaseClaimReconciler) manageClusterParamGroup(ctx context.Context, dbClaim *persistancev1.DatabaseClaim) (string, error) {

	logical := "rds.logical_replication"
	one := "1"
	immediate := "immediate"
	reboot := "pending-reboot"
	forceSsl := "rds.force_ssl"
	transactionTimeout := "idle_in_transaction_session_timeout"
	transactionTimeoutValue := "300000"
	params := &r.Input.HostParams
	pgName := r.getParameterGroupName(ctx, dbClaim)
	sharedLib := "shared_preload_libraries"
	sharedLibValue := "pg_stat_statements,pg_cron"
	cron := "cron.database_name"
	cronValue := r.Input.MasterConnInfo.DatabaseName
	desc := "custom PG for " + pgName

	providerConfigReference := xpv1.Reference{
		Name: "default",
	}

	dbParamGroup := &crossplanerds.DBClusterParameterGroup{}

	err := r.Client.Get(ctx, client.ObjectKey{
		Name: pgName,
	}, dbParamGroup)
	if err != nil {
		if errors.IsNotFound(err) {
			dbParamGroup = &crossplanerds.DBClusterParameterGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name: pgName,
				},
				Spec: crossplanerds.DBClusterParameterGroupSpec{
					ForProvider: crossplanerds.DBClusterParameterGroupParameters{
						Region:      r.getRegion(),
						Description: &desc,
						CustomDBClusterParameterGroupParameters: crossplanerds.CustomDBClusterParameterGroupParameters{
							DBParameterGroupFamilySelector: &crossplanerds.DBParameterGroupFamilyNameSelector{
								Engine:        params.Engine,
								EngineVersion: &params.EngineVersion,
							},
							Parameters: []crossplanerds.Parameter{
								{ParameterName: &logical,
									ParameterValue: &one,
									ApplyMethod:    &reboot,
								},
								{ParameterName: &forceSsl,
									ParameterValue: &one,
									ApplyMethod:    &immediate,
								},
								{ParameterName: &transactionTimeout,
									ParameterValue: &transactionTimeoutValue,
									ApplyMethod:    &immediate,
								},
								{ParameterName: &sharedLib,
									ParameterValue: &sharedLibValue,
									ApplyMethod:    &reboot,
								},
								{ParameterName: &cron,
									ParameterValue: &cronValue,
									ApplyMethod:    &reboot,
								},
							},
						},
					},
					ResourceSpec: xpv1.ResourceSpec{
						ProviderConfigReference: &providerConfigReference,
						DeletionPolicy:          params.DeletionPolicy,
					},
				},
			}
			r.Log.Info("creating crossplane DBParameterGroup resource", "DBParameterGroup", dbParamGroup.Name)

			err = r.Client.Create(ctx, dbParamGroup)
			if err != nil {
				return pgName, err
			}

		} else {
			//not errors.IsNotFound(err) {
			return pgName, err
		}
	}
	return pgName, nil
}

func (r *DatabaseClaimReconciler) deleteCloudDatabase(dbHostName string, ctx context.Context) error {

	dbInstance := &crossplanerds.DBInstance{}
	dbCluster := &crossplanerds.DBCluster{}

	if r.getMultiAZEnabled() {
		err := r.Client.Get(ctx, client.ObjectKey{
			Name: dbHostName + "-reader",
		}, dbInstance)
		if err != nil {
			if !errors.IsNotFound(err) {
				return err
			} // else not found - no action required
		} else if dbInstance.ObjectMeta.DeletionTimestamp.IsZero() {
			if err := r.Delete(ctx, dbInstance, client.PropagationPolicy(metav1.DeletePropagationBackground)); (err) != nil {
				r.Log.Info("unable delete crossplane DBInstance resource", "DBInstance", dbHostName+"-reader")
				return err
			} else {
				r.Log.Info("deleted crossplane DBInstance resource", "DBInstance", dbHostName+"-reader")
			}
		}
	}

	err := r.Client.Get(ctx, client.ObjectKey{
		Name: dbHostName,
	}, dbInstance)
	if err != nil {
		if !errors.IsNotFound(err) {
			return err
		} // else not found - no action required
	} else if dbInstance.ObjectMeta.DeletionTimestamp.IsZero() {
		if err := r.Delete(ctx, dbInstance, client.PropagationPolicy(metav1.DeletePropagationBackground)); (err) != nil {
			r.Log.Info("unable delete crossplane DBInstance resource", "DBInstance", dbHostName)
			return err
		} else {
			r.Log.Info("deleted crossplane DBInstance resource", "DBInstance", dbHostName)
		}
	}

	err = r.Client.Get(ctx, client.ObjectKey{
		Name: dbHostName,
	}, dbCluster)

	if err != nil {
		if errors.IsNotFound(err) {
			return nil //nothing to delete
		} else {
			return err
		}
	}

	if dbCluster.ObjectMeta.DeletionTimestamp.IsZero() {
		if err := r.Delete(ctx, dbCluster, client.PropagationPolicy(metav1.DeletePropagationBackground)); (err) != nil {
			r.Log.Info("unable delete crossplane DBCluster resource", "DBCluster", dbHostName)
			return err
		} else {
			r.Log.Info("deleted crossplane DBCluster resource", "DBCluster", dbHostName)
		}
	}

	return nil
}

func (r *DatabaseClaimReconciler) deleteParameterGroup(ctx context.Context, pgName string) error {

	dbParamGroup := &crossplanerds.DBParameterGroup{}
	dbClusterParamGroup := &crossplanerds.DBClusterParameterGroup{}

	err := r.Client.Get(ctx, client.ObjectKey{
		Name: pgName,
	}, dbParamGroup)

	if err != nil {
		if !errors.IsNotFound(err) {
			return err
		} // else not found - no action required
	} else if dbParamGroup.ObjectMeta.DeletionTimestamp.IsZero() {
		if err := r.Delete(ctx, dbParamGroup, client.PropagationPolicy(metav1.DeletePropagationBackground)); (err) != nil {
			r.Log.Info("unable delete crossplane dbParamGroup resource", "dbParamGroup", dbParamGroup)
			return err
		} else {
			r.Log.Info("deleted crossplane dbParamGroup resource", "dbParamGroup", dbParamGroup)
		}
	}

	err = r.Client.Get(ctx, client.ObjectKey{
		Name: pgName,
	}, dbClusterParamGroup)

	if err != nil {
		if errors.IsNotFound(err) {
			return nil //nothing to delete
		} else {
			return err
		}
	}

	if dbClusterParamGroup.ObjectMeta.DeletionTimestamp.IsZero() {
		if err := r.Delete(ctx, dbClusterParamGroup, client.PropagationPolicy(metav1.DeletePropagationBackground)); (err) != nil {
			r.Log.Info("unable delete crossplane DBCluster resource", "dbClusterParamGroup", dbClusterParamGroup)
			return err
		} else {
			r.Log.Info("deleted crossplane DBCluster resource", "dbClusterParamGroup", dbClusterParamGroup)
		}
	}

	return nil
}

func (r *DatabaseClaimReconciler) updateDBInstance(ctx context.Context, dbClaim *persistancev1.DatabaseClaim,
	dbInstance *crossplanerds.DBInstance) (bool, error) {

	// Create a patch snapshot from current DBInstance
	patchDBInstance := client.MergeFrom(dbInstance.DeepCopy())

	// Update DBInstance

	dbInstance.Spec.ForProvider.Tags = DBClaimTags(dbClaim.Spec.Tags).DBTags()

	// TODO:currently ignoring changes to shape and minStorage if a CR already exists.
	/*
		params := r.getDynamicHostParams(ctx, fragmentKey, dbClaim)

		// Update Shape and MinGibStorage for now
		if rdsInstance.Spec.ForProvider.DBInstanceClass != params.Shape {
			rdsInstance.Spec.ForProvider.DBInstanceClass = params.Shape
			update = true
		}

		if *rdsInstance.Spec.ForProvider.AllocatedStorage != params.MinStorageGB {
			rdsInstance.Spec.ForProvider.AllocatedStorage = &params.MinStorageGB
			update = true
		}
	*/

	// Compute a json patch based on the changed DBInstance
	dbInstancePatchData, err := patchDBInstance.Data(dbInstance)
	if err != nil {
		return false, err
	}
	// an empty json patch will be {}, we can assert that no update is required if len == 2
	// we could also just apply the empty patch if additional call to apiserver isn't an issue
	if len(dbInstancePatchData) == 2 {
		return false, nil
	}
	r.Log.Info("updating crossplane DBInstance resource", "DBInstance", dbInstance.Name)
	err = r.Client.Patch(ctx, dbInstance, patchDBInstance)
	if err != nil {
		return false, err
	}

	return true, nil
}

func (r *DatabaseClaimReconciler) updateDBCluster(ctx context.Context, dbClaim *persistancev1.DatabaseClaim,
	dbCluster *crossplanerds.DBCluster) (bool, error) {

	// Create a patch snapshot from current DBCluster
	patchDBCluster := client.MergeFrom(dbCluster.DeepCopy())

	// Update DBCluster

	dbCluster.Spec.ForProvider.Tags = DBClaimTags(dbClaim.Spec.Tags).DBTags()

	// Compute a json patch based on the changed RDSInstance
	dbClusterPatchData, err := patchDBCluster.Data(dbCluster)
	if err != nil {
		return false, err
	}
	// an empty json patch will be {}, we can assert that no update is required if len == 2
	// we could also just apply the empty patch if additional call to apiserver isn't an issue
	if len(dbClusterPatchData) == 2 {
		return false, nil
	}
	r.Log.Info("updating crossplane DBCluster resource", "DBCluster", dbCluster.Name)
	r.Client.Patch(ctx, dbCluster, patchDBCluster)
	if err != nil {
		return false, err
	}

	return true, nil
}
func (r *DatabaseClaimReconciler) createOrUpdateSecret(ctx context.Context, dbClaim *persistancev1.DatabaseClaim,
	connInfo *persistancev1.DatabaseClaimConnectionInfo) error {

	gs := &corev1.Secret{}
	dbType := dbClaim.Spec.Type
	secretName := dbClaim.Spec.SecretName
	var dsn, dbURI string

	switch dbType {
	case defaultPostgresStr:
		dsn = dbclient.PostgresConnectionString(connInfo.Host, connInfo.Port, connInfo.Username, connInfo.Password,
			connInfo.DatabaseName, connInfo.SSLMode)
		dbURI = dbclient.PostgresURI(connInfo.Host, connInfo.Port, connInfo.Username, connInfo.Password,
			connInfo.DatabaseName, connInfo.SSLMode)
	case defaultAuroraPostgresStr:
		dsn = dbclient.PostgresConnectionString(connInfo.Host, connInfo.Port, connInfo.Username, connInfo.Password,
			connInfo.DatabaseName, connInfo.SSLMode)
		dbURI = dbclient.PostgresURI(connInfo.Host, connInfo.Port, connInfo.Username, connInfo.Password,
			connInfo.DatabaseName, connInfo.SSLMode)

	default:
		return fmt.Errorf("unknown DB type")
	}

	err := r.Client.Get(ctx, client.ObjectKey{
		Namespace: dbClaim.Namespace,
		Name:      secretName,
	}, gs)

	if err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
		if err := r.createSecret(ctx, dbClaim, dsn, dbURI, connInfo); err != nil {
			return err
		}
	} else if err := r.updateSecret(ctx, dbClaim.Spec.DSNName, dsn, dbURI, connInfo, gs); err != nil {
		return err
	}

	return nil
}

func (r *DatabaseClaimReconciler) createSecret(ctx context.Context, dbClaim *persistancev1.DatabaseClaim, dsn, dbURI string, connInfo *persistancev1.DatabaseClaimConnectionInfo) error {
	secretName := dbClaim.Spec.SecretName
	truePtr := true
	dsnName := dbClaim.Spec.DSNName
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
			dsnName:          []byte(dsn),
			"uri_" + dsnName: []byte(dbURI),
			"hostname":       []byte(connInfo.Host),
			"port":           []byte(connInfo.Port),
			"database":       []byte(connInfo.DatabaseName),
			"username":       []byte(connInfo.Username),
			"password":       []byte(connInfo.Password),
			"sslmode":        []byte(connInfo.SSLMode),
		},
	}
	r.Log.Info("creating connection info secret", "secret", secret.Name, "namespace", secret.Namespace)

	return r.Client.Create(ctx, secret)
}

func (r *DatabaseClaimReconciler) updateSecret(ctx context.Context, dsnName, dsn, dbURI string, connInfo *persistancev1.DatabaseClaimConnectionInfo, exSecret *corev1.Secret) error {
	exSecret.Data[dsnName] = []byte(dsn)
	exSecret.Data["uri_"+dsnName] = []byte(dbURI)
	exSecret.Data["hostname"] = []byte(connInfo.Host)
	exSecret.Data["port"] = []byte(connInfo.Port)
	exSecret.Data["username"] = []byte(connInfo.Username)
	exSecret.Data["password"] = []byte(connInfo.Password)
	exSecret.Data["sslmode"] = []byte(connInfo.SSLMode)
	r.Log.Info("updating connection info secret", "secret", exSecret.Name, "namespace", exSecret.Namespace)

	return r.Client.Update(ctx, exSecret)
}

func (r *DatabaseClaimReconciler) readMasterPassword(ctx context.Context, dbClaim *persistancev1.DatabaseClaim) (string, error) {
	gs := &corev1.Secret{}
	secretName := r.getSecretRef(r.Input.FragmentKey)
	secretKey := r.getSecretKey(r.Input.FragmentKey)
	if secretKey == "" {
		secretKey = "password"
	}
	if secretName == "" {
		return "", fmt.Errorf("an empty password secret reference")
	}
	namespace, err := getServiceNamespace()
	if err != nil {
		return "", err
	}
	err = r.Client.Get(ctx, client.ObjectKey{
		Namespace: namespace,
		Name:      secretName,
	}, gs)
	if err != nil {
		return "", err
	}
	return string(gs.Data[secretKey]), nil
}

func (r *DatabaseClaimReconciler) matchInstanceLabel(dbClaim *persistancev1.DatabaseClaim) (string, error) {
	settingsMap := r.Config.AllSettings()

	rTree := radix.New()
	for k := range settingsMap {
		if k != "passwordconfig" {
			rTree.Insert(k, true)
		}
	}
	// Find the longest prefix match
	m, _, ok := rTree.LongestPrefix(dbClaim.Spec.InstanceLabel)
	if !ok {
		return "", fmt.Errorf("can't find any instance label matching fragment keys")
	}

	dbClaim.Status.ActiveDB.MatchedLabel = m

	return m, nil
}

func (r *DatabaseClaimReconciler) manageError(ctx context.Context, dbClaim *persistancev1.DatabaseClaim, inErr error) (ctrl.Result, error) {
	dbClaim.Status.Error = inErr.Error()

	err := r.Client.Status().Update(ctx, dbClaim)
	if err != nil {
		// Ignore conflicts, resource might just be outdated.
		if errors.IsConflict(err) {
			err = nil
		}
		return ctrl.Result{}, err
	}
	r.Log.Error(inErr, "error")
	return ctrl.Result{}, inErr
}

func (r *DatabaseClaimReconciler) manageSuccess(ctx context.Context, dbClaim *persistancev1.DatabaseClaim) (ctrl.Result, error) {
	dbClaim.Status.Error = ""

	err := r.Client.Status().Update(ctx, dbClaim)
	if err != nil {
		// Ignore conflicts, resource might just be outdated.
		if errors.IsConflict(err) {
			err = nil
		}
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: r.getPasswordRotationTime()}, nil
}

func GetDBName(dbClaim *persistancev1.DatabaseClaim) string {
	if dbClaim.Spec.DBNameOverride != "" {
		return dbClaim.Spec.DBNameOverride
	}

	return dbClaim.Spec.DatabaseName
}

func (r *DatabaseClaimReconciler) updateUserStatus(status *persistancev1.Status, userName, userPassword string) {
	timeNow := metav1.Now()
	status.UserUpdatedAt = &timeNow
	status.ConnectionInfo.Username = userName
	r.Input.TempSecret = userPassword
	status.ConnectionInfoUpdatedAt = &timeNow
}

func updateDBStatus(status *persistancev1.Status, dbName string) {
	timeNow := metav1.Now()
	status.DbCreatedAt = &timeNow
	status.ConnectionInfo.DatabaseName = dbName
	status.ConnectionInfoUpdatedAt = &timeNow
}

func updateHostPortStatus(status *persistancev1.Status, host, port, sslMode string) {
	timeNow := metav1.Now()
	status.ConnectionInfo.Host = host
	status.ConnectionInfo.Port = port
	status.ConnectionInfo.SSLMode = sslMode
	status.ConnectionInfoUpdatedAt = &timeNow
}

func getServiceNamespace() (string, error) {
	ns, found := os.LookupEnv(serviceNamespaceEnvVar)
	if !found {
		return "", fmt.Errorf("service namespace env %s must be set", serviceNamespaceEnvVar)
	}
	return ns, nil
}

func (r *DatabaseClaimReconciler) getSrcAdminPasswdFromSecret(ctx context.Context, dbClaim *persistancev1.DatabaseClaim) (string, error) {
	secretKey := "password"
	gs := &corev1.Secret{}

	ns := dbClaim.Spec.SourceDataFrom.Database.SecretRef.Namespace
	if ns == "" {
		ns = "default"
	}
	err := r.Client.Get(ctx, client.ObjectKey{
		Namespace: ns,
		Name:      dbClaim.Spec.SourceDataFrom.Database.SecretRef.Name,
	}, gs)
	if err != nil {
		return "", err
	}
	return string(gs.Data[secretKey]), nil
}

func (r *DatabaseClaimReconciler) getSrcAppPasswdFromSecret(ctx context.Context, dbClaim *persistancev1.DatabaseClaim) (string, error) {
	secretKey := "password"
	secretName := dbClaim.Spec.SecretName
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
		r.Log.Error(err, "getSrcAppPasswdFromSecret failed")
		return "", err
	}
	return string(gs.Data[secretKey]), nil
}

func (r *DatabaseClaimReconciler) deleteTempSecret(ctx context.Context, dbClaim *persistancev1.DatabaseClaim) error {
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
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	return r.Client.Delete(ctx, gs)
}

func (r *DatabaseClaimReconciler) getTempPasswordFromSecret(ctx context.Context, dbClaim *persistancev1.DatabaseClaim) (string, error) {
	secretKey := "password"
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
	return string(gs.Data[secretKey]), nil
}

func (r *DatabaseClaimReconciler) createTempSecret(ctx context.Context, password string, dbClaim *persistancev1.DatabaseClaim) error {

	gs := &corev1.Secret{}
	secretName := "temp-" + dbClaim.Spec.SecretName

	err := r.Client.Get(ctx, client.ObjectKey{
		Namespace: dbClaim.Namespace,
		Name:      secretName,
	}, gs)

	if err != nil {
		if !errors.IsNotFound(err) {
			return err
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
				"password": []byte(password),
			},
		}

		r.Log.Info("creating temp secret", "name", secret.Name, "namespace", secret.Namespace)
		return r.Client.Create(ctx, secret)
	} else {
		r.Log.Info("updating temp secret", "name", secretName)
		gs.Data["password"] = []byte(password)
		return r.Client.Update(ctx, gs)
	}
}
