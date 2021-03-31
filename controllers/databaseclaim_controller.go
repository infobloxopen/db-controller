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
)

// DatabaseClaimReconciler reconciles a DatabaseClaim object
type DatabaseClaimReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
	Config *viper.Viper
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

	DbConnectionString := r.dbConnectionString(dbClaim.Spec.InstanceLabel)

	log.Info("Current config", "Config", r.Config.AllSettings())
	log.Info("opening database: ")

	db, err := sql.Open("postgres", DbConnectionString)
	if err != nil {
		return ctrl.Result{}, err
	}
	defer db.Close()

	log.Info(fmt.Sprintf("processing DBClaim: %s namespace: %s AppID: %s", dbClaim.Name, dbClaim.Namespace, dbClaim.Spec.AppID))
	log.Info(fmt.Sprintf("db name: %v", dbClaim.Spec.AppID))

	var exists bool
	err = db.QueryRow("SELECT EXISTS(SELECT datname FROM pg_catalog.pg_database WHERE datname = $1)", dbClaim.Spec.AppID).Scan(&exists)
	if err != nil {
		log.Error(err, "could not query for database name")
		return ctrl.Result{}, err
	}
	if !exists {
		// create the database
		if _, err := db.Exec("create database " + fmt.Sprintf("%q", dbClaim.Spec.AppID)); err != nil {
			log.Error(err, "could not create databse")
			return ctrl.Result{}, err
		}
	}

	if dbClaim.Status.UserCreateTime == nil || time.Since(dbClaim.Status.UserCreateTime.Time) > time.Minute {
		log.Info("time to create a user")

		t := time.Now().Truncate(time.Minute)
		tStr := fmt.Sprintf("%d%02d%02d%02d%02d", t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute())
		to := t.Add(-time.Minute * 2)
		tStrOld := fmt.Sprintf("%d%02d%02d%02d%02d", to.Year(), to.Month(), to.Day(), to.Hour(), to.Minute())

		password, err := r.generatePassword()
		if err != nil || password == "" {
			return ctrl.Result{Requeue: true}, err
		}

		usernamePrefix := fmt.Sprintf("dbctl_%s_", dbClaim.Spec.AppID)
		username := usernamePrefix + tStr
		oldUsername := usernamePrefix + tStrOld
		_, err = db.Exec("create user " + fmt.Sprintf("%q", username) + " with encrypted password '" + password + "'")
		if err != nil {
			if !strings.Contains(err.Error(), "already exists") {
				log.Error(err, "could not create user "+username)
				return ctrl.Result{}, err
			}
		}
		if _, err := db.Exec("grant all privileges on database " + fmt.Sprintf("%q", dbClaim.Spec.AppID) + " to " + fmt.Sprintf("%q", username)); err != nil {
			log.Error(err, "could not set permissions to user "+username)
			return ctrl.Result{}, err
		}

		dbClaim.Status.ConnectionInfo = &persistancev1.DatabaseClaimConnectionInfo{
			Username:     dbClaim.Spec.Username,
			Host:         r.getHost(dbClaim.Spec.InstanceLabel),
			Port:         r.getPort(dbClaim.Spec.InstanceLabel),
			DatabaseName: dbClaim.Spec.DatabaseName,
			Password:     password,
		}
		now := metav1.Now()
		// TODO remove UserCreateTime with ConnectionInfo.UserUpdatedAt
		dbClaim.Status.UserCreateTime = &now
		dbClaim.Status.ConnectionInfo.UserUpdatedAt = &now
		dbClaim.Status.ConnectionInfoUpdatedAt = &now
		if err := r.Status().Update(ctx, dbClaim); err != nil {
			log.Error(err, "could not update db claim")
			return ctrl.Result{}, err
		}
		// create connection info secret
		if err := r.createOrUpdateSecret(ctx, dbClaim); err != nil {
			return ctrl.Result{Requeue: true}, err
		}
		// clean up older users
		log.Info("cleaning up old users")
		rows, err := db.Query("select usename FROM pg_catalog.pg_user WHERE usename like '"+usernamePrefix+"%' AND usename < $1", oldUsername)
		if err != nil {
			log.Error(err, "could not select old usernames")
			return ctrl.Result{}, err
		}
		defer rows.Close()
		for rows.Next() {
			var thisUsername string
			err = rows.Scan(&thisUsername)
			log.Info("dropping user " + thisUsername)
			if err != nil {
				log.Error(err, "could not scan usernames")
				return ctrl.Result{}, err
			}
			if _, err := db.Exec("REASSIGN OWNED BY " + thisUsername + " TO " + username); err != nil {
				log.Error(err, "could not reassign owned by "+thisUsername)
				return ctrl.Result{}, err
			}
			if _, err := db.Exec("REVOKE ALL ON " + dbClaim.Spec.AppID + " FROM " + thisUsername); err != nil {
				log.Error(err, "could not reassign owned by "+thisUsername)
				return ctrl.Result{}, err
			}
			if _, err := db.Exec("DROP USER " + username); err != nil {
				log.Error(err, "could not drop user: "+username)
				return ctrl.Result{}, err
			}

			log.Info("dropped user: " + username)
		}
	}

	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

func (r *DatabaseClaimReconciler) generatePassword() (string, error) {
	var pass string
	var err error

	complEnabled := r.Config.GetString("passwordconfig::passwordComplexity")
	if complEnabled == "enabled" {
		passLen := r.Config.GetInt("passwordconfig::minPasswordLength")
		count := passLen / 3
		pass, err = gopassword.Generate(passLen, count, count, false, false)
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

func (r *DatabaseClaimReconciler) dbConnectionString(instanceLabel string) string {
	h := r.getHost(instanceLabel)
	u := r.getUser(instanceLabel)
	// TODO fetch master password from secrets
	p := "postgres"

	dbStr := fmt.Sprintf("host=%s user=%s password=%s sslmode=%s", h, u, p, r.getSSLMode(instanceLabel))

	return dbStr
}

func (r *DatabaseClaimReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&persistancev1.DatabaseClaim{}).
		Complete(r)
}

func (r *DatabaseClaimReconciler) getHost(instanceLabel string) string {
	return r.Config.GetString(fmt.Sprintf("%s::host", instanceLabel))
}

func (r *DatabaseClaimReconciler) getUser(instanceLabel string) string {
	return r.Config.GetString(fmt.Sprintf("%s::username", instanceLabel))
}

func (r *DatabaseClaimReconciler) getPort(instanceLabel string) string {
	return r.Config.GetString(fmt.Sprintf("%s::port", instanceLabel))
}

func (r *DatabaseClaimReconciler) getSSLMode(instanceLabel string) string {
	var sslmode string

	useSSL := r.Config.GetBool(fmt.Sprintf("%s::usessl", instanceLabel))
	if useSSL {
		sslmode = "enable"
	} else {
		sslmode = "disable"
	}

	return sslmode
}

func (r *DatabaseClaimReconciler) createSecret(ctx context.Context, dbClaim *persistancev1.DatabaseClaim) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: dbClaim.Namespace,
			Name:      dbClaim.Name,
		},
		Data: map[string][]byte{
			"Username":      []byte(dbClaim.Spec.Username),
			"Host":          []byte(dbClaim.Status.ConnectionInfo.Host),
			"Port":          []byte(dbClaim.Status.ConnectionInfo.Port),
			"DatabaseName":  []byte(dbClaim.Status.ConnectionInfo.DatabaseName),
			"UserUpdatedAt": []byte(dbClaim.Status.ConnectionInfo.UserUpdatedAt.String()),
			"Password":      []byte(dbClaim.Status.ConnectionInfo.Password),
		},
	}
	r.Log.Info("creating connection info secret", "secret", dbClaim.Name, "namespace", dbClaim.Namespace)
	if err := r.Client.Create(ctx, secret); err != nil {
		return err
	}

	return nil
}

func (r *DatabaseClaimReconciler) updateSecret(ctx context.Context, dbClaim *persistancev1.DatabaseClaim, exSecret *corev1.Secret) error {
	exSecret.Data["Username"] = []byte(dbClaim.Spec.Username)
	exSecret.Data["Host"] = []byte(dbClaim.Status.ConnectionInfo.Host)
	exSecret.Data["Port"] = []byte(dbClaim.Status.ConnectionInfo.Port)
	exSecret.Data["DatabaseName"] = []byte(dbClaim.Status.ConnectionInfo.DatabaseName)
	exSecret.Data["UserUpdatedAt"] = []byte(dbClaim.Status.ConnectionInfo.UserUpdatedAt.String())
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
