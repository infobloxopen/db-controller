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

	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
	"github.com/infobloxopen/db-controller/pkg/dbclient"
)

const (
	defaultPassLen = 32
	defaultNumDig  = 10
	defaultNumSimb = 10
	// rotation time in minutes
	minRotationTime        = 60
	maxRotationTime        = 1440
	defaultRotationTime    = minRotationTime
	serviceNamespaceEnvVar = "SERVICE_NAMESPACE"
)

// DatabaseClaimReconciler reconciles a DatabaseClaim object
type DatabaseClaimReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
	Config *viper.Viper
}

func (r *DatabaseClaimReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("databaseclaim", req.NamespacedName)

	var dbClaim persistancev1.DatabaseClaim
	if err := r.Get(ctx, req.NamespacedName, &dbClaim); err != nil {
		log.Error(err, "unable to fetch DatabaseClaim")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	return r.updateStatus(ctx, &dbClaim)
}

func (r *DatabaseClaimReconciler) updateStatus(ctx context.Context, dbClaim *persistancev1.DatabaseClaim) (ctrl.Result, error) {
	log := r.Log.WithValues("databaseclaim", dbClaim.Namespace+"/"+dbClaim.Name)

	if dbClaim.Status.ConnectionInfo == nil {
		dbClaim.Status.ConnectionInfo = &persistancev1.DatabaseClaimConnectionInfo{}
	}

	fragmentKey, err := r.matchInstanceLabel(dbClaim)
	if err != nil {
		return r.manageError(ctx, dbClaim, err)
	}

	log.Info("creating database client")
	dbClient, err := r.getClient(ctx, log, fragmentKey, dbClaim)
	if err != nil {
		return r.manageError(ctx, dbClaim, err)
	}

	defer dbClient.Close()

	log.Info(fmt.Sprintf("processing DBClaim: %s namespace: %s AppID: %s", dbClaim.Name, dbClaim.Namespace, dbClaim.Spec.AppID))

	dbName := GetDBName(dbClaim)
	created, err := dbClient.CreateDataBase(dbName)
	if err != nil {
		return r.manageError(ctx, dbClaim, err)
	} else if created || dbClaim.Status.ConnectionInfo.DatabaseName == "" {
		updateDBStatus(dbClaim, dbName)
	}

	username := dbClaim.Spec.Username
	if r.isUserChanged(dbClaim) {
		oldUsername := dbClaim.Status.ConnectionInfo.Username
		if err := dbClient.UpdateUser(oldUsername, username); err != nil {
			return r.manageError(ctx, dbClaim, err)
		}
		timeNow := metav1.Now()
		dbClaim.Status.UserUpdatedAt = &timeNow
		dbClaim.Status.ConnectionInfo.Username = username
		dbClaim.Status.ConnectionInfoUpdatedAt = &timeNow
	} else {
		userPassword, err := r.generatePassword()
		if err != nil {
			return r.manageError(ctx, dbClaim, err)
		}

		created, err := dbClient.CreateUser(dbName, username, userPassword)
		if err != nil {
			return r.manageError(ctx, dbClaim, err)
		} else if created || dbClaim.Status.ConnectionInfo.Username == "" {
			updateUserStatus(dbClaim, username, userPassword)
		}
	}

	if dbClaim.Status.UserUpdatedAt == nil || time.Since(dbClaim.Status.UserUpdatedAt.Time) > r.getPasswordRotationTime() {
		userPassword, err := r.generatePassword()
		if err != nil {
			return r.manageError(ctx, dbClaim, err)
		}

		if err := dbClient.UpdatePassword(username, userPassword); err != nil {
			return r.manageError(ctx, dbClaim, err)
		}

		updateUserStatus(dbClaim, username, userPassword)
	}

	if err := r.Status().Update(ctx, dbClaim); err != nil {
		log.Error(err, "could not update db claim")
		return r.manageError(ctx, dbClaim, err)
	}
	// create connection info secret
	if err := r.createOrUpdateSecret(ctx, dbClaim); err != nil {
		return r.manageError(ctx, dbClaim, err)
	}

	return r.manageSuccess(ctx, dbClaim)
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

func (r *DatabaseClaimReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&persistancev1.DatabaseClaim{}).
		Complete(r)
}

func (r *DatabaseClaimReconciler) getMasterHost(fragmentKey string, dbClaim *persistancev1.DatabaseClaim) string {
	// If config host is overridden by db claims host
	if dbClaim.Spec.Host != "" {
		return dbClaim.Spec.Host
	}

	return r.Config.GetString(fmt.Sprintf("%s::Host", fragmentKey))
}

func (r *DatabaseClaimReconciler) getMasterUser(fragmentKey string) string {
	return r.Config.GetString(fmt.Sprintf("%s::Username", fragmentKey))
}

func (r *DatabaseClaimReconciler) getMasterPort(fragmentKey string, dbClaim *persistancev1.DatabaseClaim) string {
	// If config port is overridden by db claims port
	if dbClaim.Spec.Port != "" {
		return dbClaim.Spec.Port
	}

	return r.Config.GetString(fmt.Sprintf("%s::Port", fragmentKey))
}

func (r *DatabaseClaimReconciler) getSSLMode(fragmentKey string) string {
	var sslmode string

	useSSL := r.Config.GetBool(fmt.Sprintf("%s::usessl", fragmentKey))
	if useSSL {
		sslmode = "enable"
	} else {
		sslmode = "disable"
	}

	return sslmode
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
	if complEnabled == "enabled" {
		return true
	}

	return false
}

func (r *DatabaseClaimReconciler) getMinPasswordLength() int {
	return r.Config.GetInt("passwordconfig::minPasswordLength")
}

func (r *DatabaseClaimReconciler) getSecretRef(fragmentKey string) string {
	return r.Config.GetString(fmt.Sprintf("%s::PasswordSecretRef", fragmentKey))
}

func (r *DatabaseClaimReconciler) createSecret(ctx context.Context, dbClaim *persistancev1.DatabaseClaim) error {
	claimName := dbClaim.Name
	truePtr := true
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: dbClaim.Namespace,
			Name:      claimName,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         "databaseclaims.persistance.atlas.infoblox.com/v1",
					Kind:               "CustomResourceDefinition",
					Name:               claimName,
					UID:                dbClaim.UID,
					Controller:         &truePtr,
					BlockOwnerDeletion: &truePtr,
				},
			},
		},
		Data: map[string][]byte{
			"Username":     []byte(dbClaim.Spec.Username),
			"Host":         []byte(dbClaim.Status.ConnectionInfo.Host),
			"Port":         []byte(dbClaim.Status.ConnectionInfo.Port),
			"DatabaseName": []byte(dbClaim.Status.ConnectionInfo.DatabaseName),
			"Password":     []byte(dbClaim.Status.ConnectionInfo.Password),
		},
	}
	r.Log.Info("creating connection info secret", "secret", dbClaim.Name, "namespace", dbClaim.Namespace)
	if err := r.Client.Create(ctx, secret); err != nil {
		return err
	}

	return nil
}

func (r *DatabaseClaimReconciler) updateSecret(ctx context.Context, dbClaim *persistancev1.DatabaseClaim, exSecret *corev1.Secret) error {
	exSecret.Data["Username"] = []byte(dbClaim.Status.ConnectionInfo.Username)
	exSecret.Data["Host"] = []byte(dbClaim.Status.ConnectionInfo.Host)
	exSecret.Data["Port"] = []byte(dbClaim.Status.ConnectionInfo.Port)
	exSecret.Data["DatabaseName"] = []byte(dbClaim.Status.ConnectionInfo.DatabaseName)
	exSecret.Data["Password"] = []byte(dbClaim.Status.ConnectionInfo.Password)

	r.Log.Info("updating connection info secret", "secret", dbClaim.Name, "namespace", dbClaim.Namespace)
	if err := r.Client.Update(ctx, exSecret); err != nil {
		return err
	}

	return nil
}

func (r *DatabaseClaimReconciler) createOrUpdateSecret(ctx context.Context, dbClaim *persistancev1.DatabaseClaim) error {
	gs := &corev1.Secret{}

	err := r.Client.Get(ctx, client.ObjectKey{
		Namespace: dbClaim.Namespace,
		Name:      dbClaim.Name,
	}, gs)
	if err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
		if err := r.createSecret(ctx, dbClaim); err != nil {
			return err
		}
	} else {
		if err := r.updateSecret(ctx, dbClaim, gs); err != nil {
			return err
		}
	}

	return nil
}

func (r *DatabaseClaimReconciler) readMasterPassword(ctx context.Context, fragmentKey string, namespace string) (string, error) {
	gs := &corev1.Secret{}
	secretName := r.getSecretRef(fragmentKey)

	if secretName == "" {
		return "", fmt.Errorf("an empty password secret reference")
	}

	err := r.Client.Get(ctx, client.ObjectKey{
		Namespace: namespace,
		Name:      secretName,
	}, gs)
	if err != nil {
		return "", err
	}

	return string(gs.Data["password"]), nil
}

func (r *DatabaseClaimReconciler) matchInstanceLabel(dbClaim *persistancev1.DatabaseClaim) (string, error) {
	settingsMap := r.Config.AllSettings()

	rTree := radix.New()
	for k, _ := range settingsMap {
		if k != "passwordconfig" {
			rTree.Insert(k, true)
		}
	}
	// Find the longest prefix match
	m, _, ok := rTree.LongestPrefix(dbClaim.Spec.InstanceLabel)
	if !ok {
		return "", fmt.Errorf("can't find any instance label matching fragment keys")
	}

	dbClaim.Status.MatchedLabel = m

	return m, nil
}

func (r *DatabaseClaimReconciler) isUserChanged(dbClaim *persistancev1.DatabaseClaim) bool {
	if dbClaim.Spec.Username != dbClaim.Status.ConnectionInfo.Username && dbClaim.Status.ConnectionInfo.Username != "" {
		return true
	}

	return false
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

	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

func (r *DatabaseClaimReconciler) getClient(ctx context.Context, log logr.Logger, fragmentKey string, dbClaim *persistancev1.DatabaseClaim) (dbclient.DBClient, error) {
	dbType := dbClaim.Spec.Type

	host := r.getMasterHost(fragmentKey, dbClaim)
	if host == "" {
		return nil, fmt.Errorf("cannot get master host for fragment key %s", fragmentKey)
	}

	port := r.getMasterPort(fragmentKey, dbClaim)
	if port == "" {
		return nil, fmt.Errorf("cannot get master port for fragment key %s", fragmentKey)
	}

	user := r.getMasterUser(fragmentKey)
	if user == "" {
		return nil, fmt.Errorf("invalid credentials (username)")
	}

	serviceNS, err := getServiceNamespace()
	if err != nil {
		return nil, err
	}

	password, err := r.readMasterPassword(ctx, fragmentKey, serviceNS)
	if err != nil {
		return nil, err
	}

	if password == "" {
		return nil, fmt.Errorf("invalid credentials (password)")
	}

	sslMode := r.getSSLMode(fragmentKey)
	if sslMode == "" {
		return nil, fmt.Errorf("invalid sslMode")
	}

	updateHostPortStatus(dbClaim, host, port)

	return dbclient.DBClientFactory(log, dbType, host, port, user, password, sslMode)
}

func GetDBName(dbClaim *persistancev1.DatabaseClaim) string {
	if dbClaim.Spec.DBNameOverride != "" {
		return dbClaim.Spec.DBNameOverride
	}

	return dbClaim.Spec.DatabaseName
}

func updateUserStatus(dbClaim *persistancev1.DatabaseClaim, userName, userPassword string) {
	timeNow := metav1.Now()
	dbClaim.Status.UserUpdatedAt = &timeNow
	dbClaim.Status.ConnectionInfo.Username = userName
	dbClaim.Status.ConnectionInfo.Password = userPassword
	dbClaim.Status.ConnectionInfoUpdatedAt = &timeNow
}

func updateDBStatus(dbClaim *persistancev1.DatabaseClaim, dbName string) {
	timeNow := metav1.Now()
	dbClaim.Status.DbCreatedAt = &timeNow
	dbClaim.Status.ConnectionInfo.DatabaseName = dbName
	dbClaim.Status.ConnectionInfoUpdatedAt = &timeNow
}

func updateHostPortStatus(dbClaim *persistancev1.DatabaseClaim, host, port string) {
	timeNow := metav1.Now()
	dbClaim.Status.ConnectionInfo.Host = host
	dbClaim.Status.ConnectionInfo.Port = port
	dbClaim.Status.ConnectionInfoUpdatedAt = &timeNow
}

func getServiceNamespace() (string, error) {
	ns, found := os.LookupEnv(serviceNamespaceEnvVar)
	if !found {
		return "", fmt.Errorf("service namespcae env %s must be set", serviceNamespaceEnvVar)
	}
	return ns, nil
}
