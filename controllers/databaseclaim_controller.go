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
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
	"github.com/infobloxopen/db-controller/pkg/config"
	"github.com/infobloxopen/db-controller/pkg/dbclient"
	"github.com/infobloxopen/db-controller/pkg/dbuser"
	"github.com/infobloxopen/db-controller/pkg/metrics"
	"github.com/infobloxopen/db-controller/pkg/rdsauth"
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
	Log        logr.Logger
	Scheme     *runtime.Scheme
	Config     *viper.Viper
	MasterAuth *rdsauth.MasterAuth
}

func (r *DatabaseClaimReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
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

	// Grab opriginal host before it changes
	originalHost := dbClaim.Status.ConnectionInfo.Host

	fragmentKey, err := r.matchInstanceLabel(dbClaim)
	if err != nil {
		return r.manageError(ctx, dbClaim, err)
	}

	log.Info("creating database client")
	dbClient, err := r.getClient(ctx, log, fragmentKey, dbClaim)
	if err != nil {
		log.Error(err, "creating database client error")
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

	baseUsername := dbClaim.Spec.Username
	dbu := dbuser.NewDBUser(baseUsername)
	rotationTime := r.getPasswordRotationTime()

	if dbu.IsUserChanged(dbClaim) {
		oldUsername := dbu.TrimUserSuffix(dbClaim.Status.ConnectionInfo.Username)
		// renaming common role
		if err := dbClient.RenameUser(oldUsername, baseUsername); err != nil {
			return r.manageError(ctx, dbClaim, err)
		}

		// updating user a
		userPassword, err := r.generatePassword()
		if err != nil {
			return r.manageError(ctx, dbClaim, err)
		}

		if err := dbClient.UpdateUser(oldUsername+dbuser.SuffixA, dbu.GetUserA(), baseUsername, userPassword); err != nil {
			return r.manageError(ctx, dbClaim, err)
		}

		updateUserStatus(dbClaim, dbu.GetUserA(), userPassword)

		// updating user b
		userPassword, err = r.generatePassword()
		if err != nil {
			return r.manageError(ctx, dbClaim, err)
		}

		if err := dbClient.UpdateUser(oldUsername+dbuser.SuffixB, dbu.GetUserB(), baseUsername, userPassword); err != nil {
			return r.manageError(ctx, dbClaim, err)
		}

	} else {
		_, err = dbClient.CreateGroup(dbName, baseUsername)
		if err != nil {
			return r.manageError(ctx, dbClaim, err)
		}
	}

	// Force a user/password refresh if we do not have a user OR the rotation period has expired OR the host has changed
	if dbClaim.Status.UserUpdatedAt == nil || time.Since(dbClaim.Status.UserUpdatedAt.Time) > rotationTime || originalHost != dbClaim.Status.ConnectionInfo.Host {
		log.Info("rotating users")

		userPassword, err := r.generatePassword()
		if err != nil {
			return r.manageError(ctx, dbClaim, err)
		}

		nextUser := dbu.NextUser(dbClaim.Status.ConnectionInfo.Username)
		created, err = dbClient.CreateUser(nextUser, baseUsername, userPassword)
		if err != nil {
			metrics.PasswordRotatedErrors.WithLabelValues("create error").Inc()
			return r.manageError(ctx, dbClaim, err)
		}

		if !created {
			if err := dbClient.UpdatePassword(nextUser, userPassword); err != nil {
				return r.manageError(ctx, dbClaim, err)
			}
		}

		updateUserStatus(dbClaim, nextUser, userPassword)

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

	// Customize the list of symbols so we don't have characters that cause encoding issues
	gen, err := gopassword.NewGenerator(&gopassword.GeneratorInput{
		Symbols: "~!$^*()_+-{}|[]:,.",
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
		pass, err = gen.Generate(defaultPassLen, defaultNumDig, defaultNumSimb, false, false)
		if err != nil {
			return "", err
		}
	}

	return pass, nil
}

func (r *DatabaseClaimReconciler) SetupWithManager(mgr ctrl.Manager) error {
	pred := predicate.GenerationChangedPredicate{}
	return ctrl.NewControllerManagedBy(mgr).
		For(&persistancev1.DatabaseClaim{}).WithEventFilter(pred).
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
	return r.Config.GetString(fmt.Sprintf("%s::sslMode", fragmentKey))
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

func (r *DatabaseClaimReconciler) getAuthSource() string {
	return r.Config.GetString("authSource")
}

func (r *DatabaseClaimReconciler) createSecret(ctx context.Context, dbClaim *persistancev1.DatabaseClaim, dsn, dbURI string) error {
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
					APIVersion:         "databaseclaims.persistance.atlas.infoblox.com/v1",
					Kind:               "CustomResourceDefinition",
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
		},
	}
	r.Log.Info("creating connection info secret", "secret", secret.Name, "namespace", secret.Namespace)

	return r.Client.Create(ctx, secret)
}

func (r *DatabaseClaimReconciler) updateSecret(ctx context.Context, dsnName, dsn, dbURI string, exSecret *corev1.Secret) error {
	exSecret.Data[dsnName] = []byte(dsn)
	exSecret.Data["uri_"+dsnName] = []byte(dbURI)
	r.Log.Info("updating connection info secret", "secret", exSecret.Name, "namespace", exSecret.Namespace)

	return r.Client.Update(ctx, exSecret)
}

func (r *DatabaseClaimReconciler) createOrUpdateSecret(ctx context.Context, dbClaim *persistancev1.DatabaseClaim) error {
	gs := &corev1.Secret{}
	dbType := dbClaim.Spec.Type
	secretName := dbClaim.Spec.SecretName
	connInfo := dbClaim.Status.ConnectionInfo.DeepCopy()
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
		if err := r.createSecret(ctx, dbClaim, dsn, dbURI); err != nil {
			return err
		}
	} else if err := r.updateSecret(ctx, dbClaim.Spec.DSNName, dsn, dbURI, gs); err != nil {
		return err
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

	dbClaim.Status.MatchedLabel = m

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

	var password string

	switch authType := r.getAuthSource(); authType {
	case config.SecretAuthSourceType:
		r.Log.Info("using credentials from secret")
		password, err = r.readMasterPassword(ctx, fragmentKey, serviceNS)
		if err != nil {
			return nil, err
		}
	case config.AWSAuthSourceType:
		r.Log.Info("using aws IAM authorization")
		if r.MasterAuth.IsExpired() {
			token, err := r.MasterAuth.RetrieveToken(fmt.Sprintf("%s:%s", host, port), user)
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

	sslMode := r.getSSLMode(fragmentKey)
	if sslMode == "" {
		return nil, fmt.Errorf("invalid sslMode")
	}

	updateHostPortStatus(dbClaim, host, port, sslMode)

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

func updateHostPortStatus(dbClaim *persistancev1.DatabaseClaim, host, port, sslMode string) {
	timeNow := metav1.Now()
	dbClaim.Status.ConnectionInfo.Host = host
	dbClaim.Status.ConnectionInfo.Port = port
	dbClaim.Status.ConnectionInfo.SSLMode = sslMode
	dbClaim.Status.ConnectionInfoUpdatedAt = &timeNow
}

func getServiceNamespace() (string, error) {
	ns, found := os.LookupEnv(serviceNamespaceEnvVar)
	if !found {
		return "", fmt.Errorf("service namespcae env %s must be set", serviceNamespaceEnvVar)
	}
	return ns, nil
}
