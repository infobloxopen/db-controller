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
	"database/sql"
	"fmt"
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

	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
)

const (
	defaultPassLen = 32
	defaultNumDig  = 10
	defaultNumSimb = 10
	// rotation time in minutes
	minRotationTime     = 60
	maxRotationTime     = 1440
	defaultRotationTime = minRotationTime
)

const (
	postgresType = "postgres"
)

var dbTypesMap = map[string]bool{
	postgresType: true,
}

// DatabaseClaimReconciler reconciles a DatabaseClaim object
type DatabaseClaimReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
	Config *viper.Viper
}

type instanceConfig struct {
	username          string
	host              string
	port              string
	sslMod            string
	passwordSecretRef string
}

// +kubebuilder:rbac:groups=persistance.atlas.infoblox.com,resources=databaseclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=persistance.atlas.infoblox.com,resources=databaseclaims/status,verbs=get;update;patch

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
		return ctrl.Result{}, err
	}

	curInstanceConfig := r.newInstanceConfig(fragmentKey)
	dbConnectionString := r.dbConnectionString(ctx, curInstanceConfig, dbClaim)

	log.Info("opening database: ")
	db, err := sql.Open(getDBType(dbClaim.Spec.Type), dbConnectionString)
	if err != nil {
		return ctrl.Result{}, err
	}
	defer db.Close()

	log.Info(fmt.Sprintf("processing DBClaim: %s namespace: %s AppID: %s", dbClaim.Name, dbClaim.Namespace, dbClaim.Spec.AppID))

	if err := r.createDataBase(db, dbClaim); err != nil {
		return ctrl.Result{}, err
	}

	if r.isUserChanged(dbClaim) {
		if err := r.updateUser(db, dbClaim); err != nil {
			return ctrl.Result{}, err
		}
	} else {
		if err := r.createUser(db, dbClaim); err != nil {
			return ctrl.Result{}, err
		}
	}

	if dbClaim.Status.UserUpdatedAt == nil || time.Since(dbClaim.Status.UserUpdatedAt.Time) > r.getPasswordRotationTime() {
		err := r.updatePassword(db, dbClaim)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	if err := r.Status().Update(ctx, dbClaim); err != nil {
		log.Error(err, "could not update db claim")
		return ctrl.Result{}, err
	}
	// create connection info secret
	if err := r.createOrUpdateSecret(ctx, dbClaim); err != nil {
		return ctrl.Result{Requeue: true}, err
	}

	return ctrl.Result{RequeueAfter: time.Minute}, nil
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

func (r *DatabaseClaimReconciler) dbConnectionString(ctx context.Context, ic instanceConfig, dbClaim *persistancev1.DatabaseClaim) string {
	var h, p string
	pass, err := r.readMasterPassword(ctx, ic.passwordSecretRef, dbClaim.Namespace)
	if err != nil {
		r.Log.Error(err, "error during getting master password")
	}
	// If config host is overridden by db claims host
	if dbClaim.Spec.Host != "" {
		h = dbClaim.Spec.Host
	} else {
		h = ic.host
	}

	if dbClaim.Spec.Port != "" {
		p = dbClaim.Spec.Port
	} else {
		p = ic.port
	}

	dbStr := fmt.Sprintf("host=%s port=%s user=%s password=%s sslmode=%s", h, p, ic.username, pass, ic.sslMod)
	dbClaim.Status.ConnectionInfo.Host = ic.host
	dbClaim.Status.ConnectionInfo.Host = ic.port

	return dbStr
}

func (r *DatabaseClaimReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&persistancev1.DatabaseClaim{}).
		Complete(r)
}

func (r *DatabaseClaimReconciler) getMasterHost(fragmentKey string) string {
	return r.Config.GetString(fmt.Sprintf("%s::host", fragmentKey))
}

func (r *DatabaseClaimReconciler) getMasterUser(fragmentKey string) string {
	return r.Config.GetString(fmt.Sprintf("%s::username", fragmentKey))
}

func (r *DatabaseClaimReconciler) getMasterPort(fragmentKey string) string {
	return r.Config.GetString(fmt.Sprintf("%s::port", fragmentKey))
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
	return r.Config.GetString(fmt.Sprintf("%s::passwordSecretRef", fragmentKey))
}

func (r *DatabaseClaimReconciler) createSecret(ctx context.Context, dbClaim *persistancev1.DatabaseClaim) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: dbClaim.Namespace,
			Name:      dbClaim.Name,
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

func (r *DatabaseClaimReconciler) createUser(db *sql.DB, dbClaim *persistancev1.DatabaseClaim) error {
	var exists bool
	username := dbClaim.Spec.Username

	err := db.QueryRow("SELECT EXISTS(SELECT pg_user.usename FROM pg_catalog.pg_user where pg_user.usename = $1)", username).Scan(&exists)
	if err != nil {
		r.Log.Error(err, "could not query for user name")
		return err
	}

	if !exists {
		r.Log.Info("creating a user", "user", username)
		password, err := r.generatePassword()
		if err != nil || password == "" {
			return err
		}
		_, err = db.Exec("CREATE USER" + fmt.Sprintf("%q", username) + " with encrypted password '" + password + "'")
		if err != nil {
			if !strings.Contains(err.Error(), "already exists") {
				r.Log.Error(err, "could not create user "+username)
				return err
			}
		}

		if _, err := db.Exec("GRANT ALL PRIVILEGES ON DATABASE " + fmt.Sprintf("%q", getDBName(dbClaim)) + " TO " + fmt.Sprintf("%q", username)); err != nil {
			r.Log.Error(err, "could not set permissions to user "+username)
			return err
		}

		timeNow := metav1.Now()
		dbClaim.Status.UserUpdatedAt = &timeNow
		dbClaim.Status.ConnectionInfo.Username = username
		dbClaim.Status.ConnectionInfo.Password = password
		dbClaim.Status.ConnectionInfoUpdatedAt = &timeNow

		r.Log.Info("user has been created", "user", username)
	}

	return nil
}

func (r *DatabaseClaimReconciler) updateUser(db *sql.DB, dbClaim *persistancev1.DatabaseClaim) error {
	var exists bool
	username := dbClaim.Status.ConnectionInfo.Username
	newUsername := dbClaim.Spec.Username

	err := db.QueryRow("SELECT EXISTS(SELECT pg_user.usename FROM pg_catalog.pg_user where pg_user.usename = $1)", username).Scan(&exists)
	if err != nil {
		r.Log.Error(err, "could not query for user name")
		return err
	}

	if exists {
		r.Log.Info(fmt.Sprintf("renaming user %s to %s", username, newUsername))

		_, err = db.Exec("ALTER USER" + fmt.Sprintf("%q", username) + " RENAME TO  " + fmt.Sprintf("%q", newUsername))
		if err != nil {
			r.Log.Error(err, "could not rename user "+username)
			return err
		}

		timeNow := metav1.Now()
		dbClaim.Status.UserUpdatedAt = &timeNow
		dbClaim.Status.ConnectionInfo.Username = newUsername
		dbClaim.Status.ConnectionInfoUpdatedAt = &timeNow

		r.Log.Info("user has been updated", "user", newUsername)
	}

	return nil
}

func (r *DatabaseClaimReconciler) updatePassword(db *sql.DB, dbClaim *persistancev1.DatabaseClaim) error {
	password, err := r.generatePassword()
	if err != nil || password == "" {
		r.Log.Error(err, "error occurred during password generating")
		return err
	}

	r.Log.Info("update user password")

	username := dbClaim.Spec.Username

	_, err = db.Exec("ALTER ROLE" + fmt.Sprintf("%q", username) + " with encrypted password '" + password + "'")
	if err != nil {
		if !strings.Contains(err.Error(), "already exists") {
			r.Log.Error(err, "could not alter user "+username)
			return err
		}
	}
	timeNow := metav1.Now()
	dbClaim.Status.UserUpdatedAt = &timeNow
	dbClaim.Status.ConnectionInfo.Password = password
	dbClaim.Status.ConnectionInfoUpdatedAt = &timeNow

	return nil
}

func (r *DatabaseClaimReconciler) createDataBase(db *sql.DB, dbClaim *persistancev1.DatabaseClaim) error {
	var exists bool
	dbName := getDBName(dbClaim)
	err := db.QueryRow("SELECT EXISTS(SELECT datname FROM pg_catalog.pg_database WHERE datname = $1)", dbName).Scan(&exists)
	if err != nil {
		r.Log.Error(err, "could not query for database name")
		return err
	}
	if !exists {
		r.Log.Info("creating db:", "database name", dbName)
		// create the database
		if _, err := db.Exec("create database " + fmt.Sprintf("%q", dbName)); err != nil {
			r.Log.Error(err, "could not create database")
			return err
		}

		timeNow := metav1.Now()
		dbClaim.Status.DbCreatedAt = &timeNow
		dbClaim.Status.ConnectionInfo.DatabaseName = dbName
		dbClaim.Status.ConnectionInfoUpdatedAt = &timeNow

		r.Log.Info("database has been created", "DB", dbName)
	}

	return nil
}

func (r *DatabaseClaimReconciler) readMasterPassword(ctx context.Context, fragmentKey string, namespace string) (string, error) {
	gs := &corev1.Secret{}
	secretName := fragmentKey

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

func getDBName(dbClaim *persistancev1.DatabaseClaim) string {
	if dbClaim.Spec.DBNameOverride != "" {
		return dbClaim.Spec.DBNameOverride
	}

	return dbClaim.Spec.DatabaseName
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

func (r *DatabaseClaimReconciler) newInstanceConfig(fragmentKey string) instanceConfig {
	return instanceConfig{
		username:          r.getMasterUser(fragmentKey),
		host:              r.getMasterHost(fragmentKey),
		port:              r.getMasterPort(fragmentKey),
		sslMod:            r.getSSLMode(fragmentKey),
		passwordSecretRef: r.getSecretRef(fragmentKey),
	}
}

func (r *DatabaseClaimReconciler) isUserChanged(dbClaim *persistancev1.DatabaseClaim) bool {
	if dbClaim.Spec.Username != dbClaim.Status.ConnectionInfo.Username && dbClaim.Status.ConnectionInfo.Username != "" {
		return true
	}

	return false
}

func getDBType(dbType string) string {

	_, found := dbTypesMap[strings.ToLower(dbType)]
	if found {
		return strings.ToLower(dbType)
	}

	return postgresType
}
