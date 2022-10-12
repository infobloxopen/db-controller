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
	"net/url"
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

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	crossplanedb "github.com/crossplane/provider-aws/apis/database/v1beta1"

	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
	"github.com/infobloxopen/db-controller/pkg/config"
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
	minRotationTime        = 60
	maxRotationTime        = 1440
	maxWaitTime            = 10
	defaultRotationTime    = minRotationTime
	serviceNamespaceEnvVar = "SERVICE_NAMESPACE"
)

type ModeEnum int

type input struct {
	FragmentKey      string
	ManageCloudDB    bool
	MasterConnInfo   persistancev1.DatabaseClaimConnectionInfo
	DbHostIdentifier string
	DbType           string
}

const (
	M_UseExistingDB ModeEnum = iota
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
}

func (r *DatabaseClaimReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logr := r.Log.WithValues("databaseclaim", req.NamespacedName)

	var dbClaim persistancev1.DatabaseClaim
	if err := r.Get(ctx, req.NamespacedName, &dbClaim); err != nil {
		logr.Error(err, "unable to fetch DatabaseClaim")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

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

func (r *DatabaseClaimReconciler) setMode(dbClaim *persistancev1.DatabaseClaim) {
	logr := r.Log.WithValues("databaseclaim", dbClaim.Namespace+"/"+dbClaim.Name, "func", "getControllerMode")
	logr.Info("test message", "dbclaim", dbClaim.Spec)
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
			}
		}
	} else {
		r.Mode = M_UseNewDB
	}

}
func (r *DatabaseClaimReconciler) setReqInfo(dbClaim *persistancev1.DatabaseClaim) error {
	logr := r.Log.WithValues("databaseclaim", dbClaim.Namespace+"/"+dbClaim.Name, "func", "setReqInfo")

	r.Input = &input{}
	var (
		fragmentKey   string
		err           error
		createCloudDB bool
	)
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
		r.Input.DbHostIdentifier = r.getDynamicHostName(dbClaim)
	}
	r.Input = &input{ManageCloudDB: createCloudDB,
		MasterConnInfo: connInfo, FragmentKey: fragmentKey,
		DbType: string(dbClaim.Spec.Type),
	}
	logr.Info("setup values of ", "DatabaseClaimReconciler", r)
	return nil
}

func (r *DatabaseClaimReconciler) getMasterDefaultDsn() string {

	return fmt.Sprintf("%s://%s:%s@%s:%s/%s?sslmode=%s", r.Input.DbType,
		r.Input.MasterConnInfo.Username, r.Input.MasterConnInfo.Password,
		r.Input.MasterConnInfo.Host, r.Input.MasterConnInfo.Port,
		"postgres", r.Input.MasterConnInfo.SSLMode)
}

func (r *DatabaseClaimReconciler) updateStatus(ctx context.Context, dbClaim *persistancev1.DatabaseClaim) (ctrl.Result, error) {
	logr := r.Log.WithValues("databaseclaim", dbClaim.Namespace+"/"+dbClaim.Name)

	if dbClaim.Status.ActiveDB == nil {
		dbClaim.Status.ActiveDB = &persistancev1.Status{ConnectionInfo: &persistancev1.DatabaseClaimConnectionInfo{}}
	}
	if dbClaim.Status.NewDB == nil {
		dbClaim.Status.NewDB = &persistancev1.Status{ConnectionInfo: &persistancev1.DatabaseClaimConnectionInfo{}}
	}

	r.setMode(dbClaim)

	if r.Mode == M_UseExistingDB {
		logr.Info("existing db reconcile started")
		err := r.reconcileUseExisitngDB(ctx, dbClaim)
		if err != nil {
			return r.manageError(ctx, dbClaim, err)
		}
		logr.Info("existing db reconcile complete")
		return r.manageSuccess(ctx, dbClaim)
	}
	if r.Mode == M_MigrateToNewDB {
		logr.Info("migrate to new  db reconcile started")
		//check if existingDB has been already reconciled, else reconcileUseExisitngDB
		existing_db_conn, err := getConnInfoFromDSN(logr, dbClaim.Spec.SourceDataFrom.Database.DSN)
		if err != nil {
			return r.manageError(ctx, dbClaim, err)
		}
		if (dbClaim.Status.ActiveDB.ConnectionInfo.DatabaseName != existing_db_conn.DatabaseName) ||
			(dbClaim.Status.ActiveDB.ConnectionInfo.Host != existing_db_conn.Host) {

			logr.Info("existing db was not reconciled, calling reconcileUseExisitngDB before reconcileUseExisitngDB")

			err := r.reconcileUseExisitngDB(ctx, dbClaim)
			if err != nil {
				return r.manageError(ctx, dbClaim, err)
			}
		}
		r.setReqInfo(dbClaim)
		// return r.manageSuccess(ctx, dbClaim)
		return r.reconcileMigrateToNewDB(ctx, dbClaim)
	}
	if r.Mode == M_MigrationInProgress {
		logr.Info("migration in progress")
		//check if existingDB has been already reconciled, else reconcileUseExisitngDB
		r.setReqInfo(dbClaim)
		// return r.manageSuccess(ctx, dbClaim)
		return r.reconcileMigrationInProgress(ctx, dbClaim)
	}
	if r.Mode == M_UseNewDB {
		logr.Info("Use new DB")
		r.setReqInfo(dbClaim)
		result, err := r.reconcileNewDB(ctx, dbClaim)
		if err != nil {
			return r.manageError(ctx, dbClaim, err)
		}
		if result.Requeue {
			return result, nil
		}
		dbClaim.Status.ActiveDB = dbClaim.Status.NewDB.DeepCopy()
		dbClaim.Status.NewDB = &persistancev1.Status{ConnectionInfo: &persistancev1.DatabaseClaimConnectionInfo{}}
		return r.manageSuccess(ctx, dbClaim)
	}

	logr.Info("unhandled mode")
	return r.manageError(ctx, dbClaim, fmt.Errorf("unhandled mode"))

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

	if dbClaim.Spec.Host == "" {

		fragmentKey := dbClaim.Spec.InstanceLabel
		reclaimPolicy := r.getReclaimPolicy(fragmentKey)

		if reclaimPolicy == "delete" {
			dbHostName := r.getDynamicHostName(dbClaim)
			if fragmentKey == "" {
				// Delete
				return r.deleteCloudDatabase(dbHostName, ctx)
			} else {
				// Check there is no other Claims that use this fragment
				var dbClaimList persistancev1.DatabaseClaimList
				if err := r.List(ctx, &dbClaimList, client.MatchingFields{instanceLableKey: dbClaim.Spec.InstanceLabel}); err != nil {
					return err
				}

				if len(dbClaimList.Items) == 1 {
					// Delete
					return r.deleteCloudDatabase(dbHostName, ctx)
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
	apiGVStr         = persistancev1.GroupVersion.String()
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

func (r *DatabaseClaimReconciler) getAuthSource() string {
	return r.Config.GetString("authSource")
}

func (r *DatabaseClaimReconciler) getRegion() string {
	return r.Config.GetString("region")
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
	for i := range conditions {
		if conditions[i].Type == ready {
			return true
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

func (r *DatabaseClaimReconciler) getDynamicHost(ctx context.Context, fragmentKey string, dbClaim *persistancev1.DatabaseClaim) (persistancev1.DatabaseClaimConnectionInfo, error) {
	connInfo := persistancev1.DatabaseClaimConnectionInfo{}

	// Make sure dbHostName is unique
	dbHostName := r.getDynamicHostName(dbClaim)

	rds := &crossplanedb.RDSInstance{}

	// Found a use case where the change in DatabaseClaim updated crossplane RDSInstance resource
	// the change was rejected but the cache was updated and was out of sync with api server
	// See https://pkg.go.dev/sigs.k8s.io/controller-runtime#hdr-Clients_and_Caches
	// "client does not promise to invalidate the cache during writes"
	// We can use mgr.GetAPIReader() to bypass the cache and read value directly from API Server
	// Since update does not work I am not going to make this change as it will impact
	// the performance of the controller.
	//
	err := r.Client.Get(ctx, client.ObjectKey{
		Name: dbHostName,
	}, rds)
	if err != nil {
		err = r.createCloudDatabase(dbHostName, ctx, fragmentKey, dbClaim)
		if err != nil {
			return connInfo, err
		}
		return connInfo, nil
	}

	// Deletion is long running task check that is not being deleted.
	if !rds.ObjectMeta.DeletionTimestamp.IsZero() {
		err = fmt.Errorf("can not create Cloud Database %s it is being deleted", dbHostName)
		r.Log.Error(err, "RDSInstance", "dbHostName", dbHostName)
		return connInfo, err
	}

	update, err := r.updateCloudDatabase(ctx, dbClaim, rds)
	if update {
		// Reschedule run after update is complete
		return connInfo, err
	}

	// Check if resource is ready
	if !r.isResourceReady(rds.Status.ResourceStatus) {
		return connInfo, nil
	}

	// Check database secret
	connInfo, _ = r.readResourceSecret(ctx, dbClaim)

	// SSL Mode is always required
	// TODO connInfo.SSLMode should have types for enums
	// FIXME - should come from config now that it is defined.
	connInfo.SSLMode = "require"

	return connInfo, nil
}

func (r *DatabaseClaimReconciler) manageCloudHost(ctx context.Context, dbClaim *persistancev1.DatabaseClaim) (*crossplanedb.RDSInstance, error) {
	dbHostIdentifier := r.Input.DbHostIdentifier

	rds := &crossplanedb.RDSInstance{}

	err := r.Client.Get(ctx, client.ObjectKey{
		Name: dbHostIdentifier,
	}, rds)
	if err != nil {
		err = r.createCloudDBHost(dbHostIdentifier, ctx, dbClaim)
		if err != nil {
			return nil, err
		}
	}

	// Deletion is long running task check that is not being deleted.
	if !rds.ObjectMeta.DeletionTimestamp.IsZero() {
		err = fmt.Errorf("can not create Cloud Database %s it is being deleted", dbHostIdentifier)
		r.Log.Error(err, "RDSInstance", "dbHostIdentifier", dbHostIdentifier)
		return rds, err
	}

	_, err = r.updateCloudDatabase(ctx, dbClaim, rds)
	return rds, err
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
}

func (r *DatabaseClaimReconciler) getDynamicHostParams(ctx context.Context, fragmentKey string, dbClaim *persistancev1.DatabaseClaim) DynamicHostParms {
	params := DynamicHostParms{}

	// Database Config
	if fragmentKey == "" {
		params.MasterUsername = r.Config.GetString("defaultMasterUsername")
		params.EngineVersion = r.Config.GetString("defaultEngineVersion")
		params.Shape = dbClaim.Spec.Shape
		params.Engine = string(dbClaim.Spec.Type)
		params.MinStorageGB = dbClaim.Spec.MinStorageGB
	} else {
		params.MasterUsername = r.getMasterUser(dbClaim)
		params.EngineVersion = r.Config.GetString(fmt.Sprintf("%s::Engineversion", fragmentKey))
		params.Shape = r.Config.GetString(fmt.Sprintf("%s::shape", fragmentKey))
		params.MinStorageGB = r.Config.GetInt(fmt.Sprintf("%s::minStorageGB", fragmentKey))
	}

	// Set defaults

	if params.MasterUsername == "" {
		params.MasterUsername = r.Config.GetString("defaultMasterUsername")
	}

	if params.EngineVersion == "" {
		params.EngineVersion = r.Config.GetString("defaultEngineVersion")
	}

	if params.Shape == "" {
		params.Shape = r.Config.GetString("defaultShape")
	}

	if params.Engine == "" {
		params.Engine = r.Config.GetString("defaultEngine")
	}

	if params.MinStorageGB == 0 {
		params.MinStorageGB = r.Config.GetInt("defaultMinStorageGB")
	}

	// TODO - Implement these for each fragmentKey also
	params.SkipFinalSnapshotBeforeDeletion = r.Config.GetBool("defaultSkipFinalSnapshotBeforeDeletion")
	params.PubliclyAccessible = r.Config.GetBool("defaultPubliclyAccessible")
	if r.Config.GetString("defaultDeletionPolicy") == "delete" {
		params.DeletionPolicy = xpv1.DeletionDelete
	} else {
		params.DeletionPolicy = xpv1.DeletionOrphan
	}

	// TODO - Enable IAM auth based on authSource config
	params.EnableIAMDatabaseAuthentication = false

	return params
}
func (r *DatabaseClaimReconciler) getCloudDBHostParams(ctx context.Context, dbClaim *persistancev1.DatabaseClaim) DynamicHostParms {
	params := DynamicHostParms{}

	fragmentKey := r.Input.FragmentKey
	// Database Config
	if fragmentKey == "" {
		params.MasterUsername = r.Input.MasterConnInfo.Username
		params.Shape = dbClaim.Spec.Shape
		params.Engine = string(dbClaim.Spec.Type)
		params.MinStorageGB = dbClaim.Spec.MinStorageGB
	} else {
		params.MasterUsername = r.getMasterUser(dbClaim)
		params.EngineVersion = r.Config.GetString(fmt.Sprintf("%s::Engineversion", fragmentKey))
		params.Shape = r.Config.GetString(fmt.Sprintf("%s::shape", fragmentKey))
		params.MinStorageGB = r.Config.GetInt(fmt.Sprintf("%s::minStorageGB", fragmentKey))
	}

	if params.EngineVersion == "" {
		params.EngineVersion = r.Config.GetString("defaultEngineVersion")
	}

	if params.Shape == "" {
		params.Shape = r.Config.GetString("defaultShape")
	}

	if params.Engine == "" {
		params.Engine = r.Config.GetString("defaultEngine")
	}

	if params.MinStorageGB == 0 {
		params.MinStorageGB = r.Config.GetInt("defaultMinStorageGB")
	}

	// TODO - Implement these for each fragmentKey also
	params.SkipFinalSnapshotBeforeDeletion = r.Config.GetBool("defaultSkipFinalSnapshotBeforeDeletion")
	params.PubliclyAccessible = r.Config.GetBool("defaultPubliclyAccessible")
	if r.Config.GetString("defaultDeletionPolicy") == "delete" {
		params.DeletionPolicy = xpv1.DeletionDelete
	} else {
		params.DeletionPolicy = xpv1.DeletionOrphan
	}

	// TODO - Enable IAM auth based on authSource config
	params.EnableIAMDatabaseAuthentication = false

	return params
}

func (r *DatabaseClaimReconciler) createCloudDBHost(dbHostName string, ctx context.Context, dbClaim *persistancev1.DatabaseClaim) error {
	serviceNS, err := getServiceNamespace()
	if err != nil {
		return err
	}

	dbSecret := xpv1.SecretReference{
		Name:      dbHostName,
		Namespace: serviceNS,
	}

	// Infrastructure Config
	region := r.getRegion()
	providerConfigReference := xpv1.Reference{
		Name: "default",
	}

	params := r.getCloudDBHostParams(ctx, dbClaim)

	rdsInstance := &crossplanedb.RDSInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name: dbHostName,
			// TODO - Figure out the proper labels for resource
			// Labels:    map[string]string{"app.kubernetes.io/managed-by": "db-controller"},
		},
		Spec: crossplanedb.RDSInstanceSpec{
			ForProvider: crossplanedb.RDSInstanceParameters{
				Region: &region,
				VPCSecurityGroupIDRefs: []xpv1.Reference{
					{Name: r.getVpcSecurityGroupIDRefs()},
				},
				DBSubnetGroupNameRef: &xpv1.Reference{
					Name: r.getDbSubnetGroupNameRef(),
				},
				// Items from Claim and fragmentKey
				Engine:           params.Engine,
				DBInstanceClass:  params.Shape,
				AllocatedStorage: &params.MinStorageGB,
				Tags:             DBClaimTags(dbClaim.Spec.Tags).RDSInstanceTags(),
				// Items from Config
				MasterUsername:                  &params.MasterUsername,
				EngineVersion:                   &params.EngineVersion,
				SkipFinalSnapshotBeforeDeletion: &params.SkipFinalSnapshotBeforeDeletion,
				PubliclyAccessible:              &params.PubliclyAccessible,
				EnableIAMDatabaseAuthentication: &params.EnableIAMDatabaseAuthentication,
			},
			ResourceSpec: xpv1.ResourceSpec{
				WriteConnectionSecretToReference: &dbSecret,
				ProviderConfigReference:          &providerConfigReference,
				DeletionPolicy:                   params.DeletionPolicy,
			},
		},
	}

	r.Log.Info("creating crossplane RDSInstance resource", "RDSInstance", rdsInstance.Name)

	return r.Client.Create(ctx, rdsInstance)
}

func (r *DatabaseClaimReconciler) createCloudDatabase(dbHostName string, ctx context.Context, fragmentKey string, dbClaim *persistancev1.DatabaseClaim) error {
	serviceNS, err := getServiceNamespace()
	if err != nil {
		return err
	}

	dbSecret := xpv1.SecretReference{
		Name:      dbHostName,
		Namespace: serviceNS,
	}

	// Infrastructure Config
	region := r.getRegion()
	providerConfigReference := xpv1.Reference{
		Name: "default",
	}

	params := r.getDynamicHostParams(ctx, fragmentKey, dbClaim)

	rdsInstance := &crossplanedb.RDSInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name: dbHostName,
			// TODO - Figure out the proper labels for resource
			// Labels:    map[string]string{"app.kubernetes.io/managed-by": "db-controller"},
		},
		Spec: crossplanedb.RDSInstanceSpec{
			ForProvider: crossplanedb.RDSInstanceParameters{
				Region: &region,
				VPCSecurityGroupIDRefs: []xpv1.Reference{
					{Name: r.getVpcSecurityGroupIDRefs()},
				},
				DBSubnetGroupNameRef: &xpv1.Reference{
					Name: r.getDbSubnetGroupNameRef(),
				},
				// Items from Claim and fragmentKey
				Engine:           params.Engine,
				DBInstanceClass:  params.Shape,
				AllocatedStorage: &params.MinStorageGB,
				Tags:             DBClaimTags(dbClaim.Spec.Tags).RDSInstanceTags(),
				// Items from Config
				MasterUsername:                  &params.MasterUsername,
				EngineVersion:                   &params.EngineVersion,
				SkipFinalSnapshotBeforeDeletion: &params.SkipFinalSnapshotBeforeDeletion,
				PubliclyAccessible:              &params.PubliclyAccessible,
				EnableIAMDatabaseAuthentication: &params.EnableIAMDatabaseAuthentication,
			},
			ResourceSpec: xpv1.ResourceSpec{
				WriteConnectionSecretToReference: &dbSecret,
				ProviderConfigReference:          &providerConfigReference,
				DeletionPolicy:                   params.DeletionPolicy,
			},
		},
	}

	r.Log.Info("creating crossplane RDSInstance resource", "RDSInstance", rdsInstance.Name)

	return r.Client.Create(ctx, rdsInstance)
}

func (r *DatabaseClaimReconciler) deleteCloudDatabase(dbHostName string, ctx context.Context) error {

	rds := &crossplanedb.RDSInstance{}

	err := r.Client.Get(ctx, client.ObjectKey{
		Name: dbHostName,
	}, rds)
	if err != nil {
		// Nothing to delete
		return nil
	}

	// Deletion is long running task check that is not already deleted.
	if !rds.ObjectMeta.DeletionTimestamp.IsZero() {
		return nil
	}

	if err := r.Delete(ctx, rds, client.PropagationPolicy(metav1.DeletePropagationBackground)); (err) != nil {
		r.Log.Info("unable delete crossplane RDSInstance resource", "RDSInstance", dbHostName)
		return err
	} else {
		r.Log.Info("deleted crossplane RDSInstance resource", "RDSInstance", dbHostName)
	}

	return nil
}

func (r *DatabaseClaimReconciler) updateCloudDatabase(ctx context.Context, dbClaim *persistancev1.DatabaseClaim, rdsInstance *crossplanedb.RDSInstance) (bool, error) {
	// Create a patch snapshot from current RDSInstance
	patch := client.MergeFrom(rdsInstance.DeepCopy())
	// fragmentKey := r.Request.fragmentKey

	// Update RDSInstance

	rdsInstance.Spec.ForProvider.Tags = DBClaimTags(dbClaim.Spec.Tags).RDSInstanceTags()

	// TODO:currently ignoring changes to shape and minStorage if an RDSInstance CR already exists.
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

	// Compute a json patch based on the changed RDSInstance
	data, err := patch.Data(rdsInstance)
	if err != nil {
		return false, err
	}
	// an empty json patch will be {}, we can assert that no update is required if len == 2
	// we could also just apply the empty patch if additional call to apiserver isn't an issue
	if len(data) == 2 {
		return false, nil
	}
	r.Log.Info("updating crossplane RDSInstance resource", "RDSInstance", rdsInstance.Name)
	return true, r.Client.Patch(ctx, rdsInstance, patch)
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

func (r *DatabaseClaimReconciler) createOrUpdateSecret(ctx context.Context, dbClaim *persistancev1.DatabaseClaim) error {
	gs := &corev1.Secret{}
	dbType := dbClaim.Spec.Type
	secretName := dbClaim.Spec.SecretName
	connInfo := dbClaim.Status.ActiveDB.ConnectionInfo.DeepCopy()
	var dsn, dbURI string

	switch dbType {
	case dbclient.PostgresType:
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

func (r *DatabaseClaimReconciler) getClientConn(fragmentKey string, dbClaim *persistancev1.DatabaseClaim) persistancev1.DatabaseClaimConnectionInfo {
	connInfo := persistancev1.DatabaseClaimConnectionInfo{}

	connInfo.Host = r.getMasterHost(dbClaim)
	connInfo.Port = r.getMasterPort(dbClaim)
	connInfo.Username = r.getMasterUser(dbClaim)
	connInfo.SSLMode = r.getSSLMode(dbClaim)
	connInfo.DatabaseName = GetDBName(dbClaim)
	return connInfo
}
func getConnInfoFromDSN(logr logr.Logger, dsn string) (persistancev1.DatabaseClaimConnectionInfo, error) {

	connInfo := persistancev1.DatabaseClaimConnectionInfo{}

	u, err := url.Parse(dsn)
	if err != nil {
		logr.Error(err, "parsing dsn failed", "dsn", dsn)
		return connInfo, err
	}

	connInfo.Host = u.Hostname()
	connInfo.Port = u.Port()
	connInfo.Username = u.User.Username()
	connInfo.Password, _ = u.User.Password()
	db := strings.Split(u.Path, "/")
	if len(db) <= 1 {
		logr.Error(err, "parsing dsn failed. db not specified", "dsn", dsn)
		return connInfo, err
	}
	connInfo.DatabaseName = db[1]

	m, _ := url.ParseQuery(u.RawQuery)
	connInfo.SSLMode = m.Get("sslmode")

	return connInfo, nil
}

func (r *DatabaseClaimReconciler) getDBClient(dbClaim *persistancev1.DatabaseClaim) (dbclient.Client, error) {
	logr := r.Log.WithValues("databaseclaim", dbClaim.Namespace+"/"+dbClaim.Name, "func", "getDBClient")

	//updateHostPortStatus(dbClaim, connInfo.Host, connInfo.Port, connInfo.SSLMode)

	//connInfo := r.Request.masterConnInfo

	//return dbclient.DBClientFactory(r.Log, r.Request.dbType, connInfo.Host, connInfo.Port, connInfo.Username, connInfo.Password, connInfo.SSLMode)
	logr.Info("getting dbclient", "dsn", r.getMasterDefaultDsn())
	updateHostPortStatus(dbClaim.Status.NewDB, r.Input.MasterConnInfo.Host, r.Input.MasterConnInfo.Port, r.Input.MasterConnInfo.SSLMode)
	return dbclient.New(dbclient.Config{Log: r.Log, DBType: r.Input.DbType, DSN: r.getMasterDefaultDsn()})

	//revis this if/when IAM access is enabled
	// var password string

	// switch authType := r.getAuthSource(); authType {
	// case config.SecretAuthSourceType:
	// 	r.Log.Info("using credentials from secret")
	// 	password, err = r.readMasterPassword(ctx, fragmentKey, dbClaim, serviceNS)
	// 	if err != nil {
	// 		return nil, err
	// 	}
	// case config.AWSAuthSourceType:
	// 	r.Log.Info("using aws IAM authorization")
	// 	if r.MasterAuth.IsExpired() {
	// 		token, err := r.MasterAuth.RetrieveToken(fmt.Sprintf("%s:%s", connInfo.Host, connInfo.Port), connInfo.Username)
	// 		if err != nil {
	// 			return nil, err
	// 		}
	// 		r.MasterAuth.Set(token)
	// 	}
	// 	password = r.MasterAuth.Get()

	// default:
	// 	return nil, fmt.Errorf("unknown auth source type")
	// }

	// if password == "" {
	// 	return nil, fmt.Errorf("invalid credentials (password)")
	// }

}

func (r *DatabaseClaimReconciler) getClient(ctx context.Context, logr logr.Logger, fragmentKey string, dbClaim *persistancev1.DatabaseClaim, connInfo *persistancev1.DatabaseClaimConnectionInfo) (dbclient.DBClient, error) {
	dbType := dbClaim.Spec.Type

	if connInfo.Port == "" {
		return nil, fmt.Errorf("cannot get master port for fragment key %s", fragmentKey)
	}

	if connInfo.Username == "" {
		return nil, fmt.Errorf("invalid credentials (username)")
	}

	if connInfo.SSLMode == "" {
		return nil, fmt.Errorf("invalid sslMode")
	}

	var password string
	var err error

	switch authType := r.getAuthSource(); authType {
	case config.SecretAuthSourceType:
		r.Log.Info("using credentials from secret")
		password, err = r.readMasterPassword(ctx, dbClaim)
		if err != nil {
			return nil, err
		}
	case config.AWSAuthSourceType:
		r.Log.Info("using aws IAM authorization")
		if r.MasterAuth.IsExpired() {
			token, err := r.MasterAuth.RetrieveToken(fmt.Sprintf("%s:%s", connInfo.Host, connInfo.Port), connInfo.Username)
			if err != nil {
				return nil, err
			}
			r.MasterAuth.Set(token)
		}
		password = r.MasterAuth.Get()

	default:
		return nil, fmt.Errorf("unknown auth source type")
	}

	if password == "" {
		return nil, fmt.Errorf("invalid credentials (password)")
	}

	updateHostPortStatus(dbClaim.Status.ActiveDB, connInfo.Host, connInfo.Port, connInfo.SSLMode)

	return dbclient.DBClientFactory(logr, string(dbType), connInfo.Host, connInfo.Port, connInfo.Username, password, connInfo.SSLMode)
}

func GetDBName(dbClaim *persistancev1.DatabaseClaim) string {
	if dbClaim.Spec.DBNameOverride != "" {
		return dbClaim.Spec.DBNameOverride
	}

	return dbClaim.Spec.DatabaseName
}

func updateUserStatus(status *persistancev1.Status, userName, userPassword string) {
	timeNow := metav1.Now()
	status.UserUpdatedAt = &timeNow
	status.ConnectionInfo.Username = userName
	status.ConnectionInfo.Password = userPassword
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

func (r *DatabaseClaimReconciler) reconcileUseExisitngDB(ctx context.Context, dbClaim *persistancev1.DatabaseClaim) error {
	logr := r.Log.WithValues("databaseclaim", dbClaim.Namespace+"/"+dbClaim.Name, "func", "reconcileUseExisitngDB")

	existingDBConnInfo, err := getConnInfoFromDSN(logr, dbClaim.Spec.SourceDataFrom.Database.DSN)
	if err != nil {
		return err
	}

	logr.Info("creating database client")
	dbClient, err := r.getClientForExistingDB(ctx, logr, dbClaim, &existingDBConnInfo)
	if err != nil {
		logr.Error(err, "creating database client error")
		return err
	}

	defer dbClient.Close()

	logr.Info(fmt.Sprintf("processing DBClaim: %s namespace: %s AppID: %s", dbClaim.Name, dbClaim.Namespace, dbClaim.Spec.AppID))

	dbName := existingDBConnInfo.DatabaseName
	updateDBStatus(dbClaim.Status.ActiveDB, dbName)

	err = r.manageUser(dbClient, dbClaim.Status.ActiveDB, dbName, dbClaim.Spec.Username)
	if err != nil {
		return err
	}
	if err := r.Status().Update(ctx, dbClaim); err != nil {
		logr.Error(err, "could not update db claim")
		return err
	} // create connection info secret
	if err := r.createOrUpdateSecret(ctx, dbClaim); err != nil {
		return err
	}
	return nil
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

	updateHostPortStatus(dbClaim.Status.ActiveDB, connInfo.Host, connInfo.Port, connInfo.SSLMode)

	return dbclient.New(dbclient.Config{Log: r.Log, DBType: "postgres", DSN: connInfo.Dsn()})

}

func (r *DatabaseClaimReconciler) reconcileMigrationInProgress(ctx context.Context,
	dbClaim *persistancev1.DatabaseClaim) (ctrl.Result, error) {
	logr := r.Log.WithValues("databaseclaim", dbClaim.Namespace+"/"+dbClaim.Name, "func", "reconcileMigrationInProgress")

	migrationState := dbClaim.Status.MigrationState

	logr.Info("Migration is progress", "state", migrationState)

	target_master_dsn := r.Input.MasterConnInfo.Dsn()
	target_app_conn := dbClaim.Status.NewDB.ConnectionInfo
	source_master_conn, err := getConnInfoFromDSN(logr, dbClaim.Spec.SourceDataFrom.Database.DSN)
	if err != nil {
		return r.manageError(ctx, dbClaim, err)
	}
	source_master_conn.Password, err = r.getPasswordFromSecret(ctx, dbClaim)
	if err != nil {
		return r.manageError(ctx, dbClaim, err)
	}
	source_app_conn := dbClaim.Status.ActiveDB.ConnectionInfo

	config := pgctl.Config{
		Log:              r.Log,
		SourceDBAdminDsn: source_master_conn.Dsn(),
		SourceDBUserDsn:  source_app_conn.Dsn(),
		TargetDBUserDsn:  target_app_conn.Dsn(),
		TargetDBAdminDsn: target_master_dsn,
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

			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
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
	dbClaim.Status.ActiveDB = dbClaim.Status.NewDB.DeepCopy()
	dbClaim.Status.NewDB = &persistancev1.Status{ConnectionInfo: &persistancev1.DatabaseClaimConnectionInfo{}}

	if err := r.Status().Update(ctx, dbClaim); err != nil {
		logr.Error(err, "could not update db claim")
		return r.manageError(ctx, dbClaim, err)
	}

	//create connection info secret
	if err := r.createOrUpdateSecret(ctx, dbClaim); err != nil {
		return r.manageError(ctx, dbClaim, err)
	}

	return r.manageSuccess(ctx, dbClaim)
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

	return r.reconcileMigrationInProgress(ctx, dbClaim)

}

func (r *DatabaseClaimReconciler) reconcileNewDB(ctx context.Context,
	dbClaim *persistancev1.DatabaseClaim) (ctrl.Result, error) {

	logr := r.Log.WithValues("databaseclaim", dbClaim.Namespace+"/"+dbClaim.Name, "func", "reconcileMigrateToNewDB")

	logr.Info("reconcileMigrateToNewDB", "r.Request", r.Input)

	if r.Input.ManageCloudDB {
		dbHost, err := r.manageCloudHost(ctx, dbClaim)
		if err != nil {
			return ctrl.Result{}, err
		}
		if !r.isResourceReady(dbHost.Status.ResourceStatus) {
			logr.Info("cloud host %s is in progress. requeueing")
			return ctrl.Result{RequeueAfter: r.getDynamicHostWaitTime()}, nil
		}
		connInfo, err := r.readResourceSecret(ctx, dbClaim)
		if err != nil {
			logr.Info("unable to read secret. requeueing")
			return ctrl.Result{RequeueAfter: r.getDynamicHostWaitTime()}, nil
		}
		r.Input.MasterConnInfo.Host = connInfo.Host
		r.Input.MasterConnInfo.Password = connInfo.Password

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

	if err := r.manageDatabase(dbClient, dbClaim.Status.NewDB); err != nil {
		return ctrl.Result{}, err

	}

	err = r.manageUser(dbClient, dbClaim.Status.NewDB, GetDBName(dbClaim), dbClaim.Spec.Username)
	if err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
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

	if dbu.IsUserChanged(status) {
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
		updateUserStatus(status, dbu.GetUserA(), userPassword)
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
		updateUserStatus(status, nextUser, userPassword)
	}

	return nil
}

func (r *DatabaseClaimReconciler) getPasswordFromSecret(ctx context.Context, dbClaim *persistancev1.DatabaseClaim) (string, error) {
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
