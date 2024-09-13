package databaseclaim

import (
	"context"
	"fmt"
	"strings"

	_ "github.com/lib/pq"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	v1 "github.com/infobloxopen/db-controller/api/v1"
	"github.com/infobloxopen/db-controller/pkg/dbclient"

	// FIXME: upgrade kubebuilder so this package will be removed
	"k8s.io/apimachinery/pkg/api/errors"
)

func (r *DatabaseClaimReconciler) createOrUpdateSecret(ctx context.Context, dbClaim *v1.DatabaseClaim,
	connInfo *v1.DatabaseClaimConnectionInfo) error {
	logr := log.FromContext(ctx)
	gs := &corev1.Secret{}
	dbType := dbClaim.Spec.Type
	secretName := dbClaim.Spec.SecretName
	logr.Info("createOrUpdateSecret being executed. SecretName: " + secretName)
	var dsn, dsnURI, replicaDsn, replicaDsnURI string = "", "", "", ""

	switch dbType {
	case v1.Postgres:
		fallthrough
	case v1.AuroraPostgres:
		dsn = dbclient.PostgresConnectionString(connInfo.Host, connInfo.Port, connInfo.Username, connInfo.Password, connInfo.DatabaseName, connInfo.SSLMode)
		dsnURI = dbclient.PostgresURI(connInfo.Host, connInfo.Port, connInfo.Username, connInfo.Password, connInfo.DatabaseName, connInfo.SSLMode)

		replicaDsn = strings.Replace(dsn, ".cluster-", ".cluster-ro-", -1)
		replicaDsnURI = strings.Replace(dsnURI, ".cluster-", ".cluster-ro-", -1)
	default:
		return fmt.Errorf("unknown DB type")
	}

	err := r.Client.Get(ctx, client.ObjectKey{
		Namespace: dbClaim.Namespace,
		Name:      secretName,
	}, gs)

	if err != nil && errors.IsNotFound(err) {
		if err := r.createSecret(ctx, dbClaim, dsn, dsnURI, replicaDsn, replicaDsnURI, connInfo); err != nil {
			return err
		}
		return nil
	}

	return r.updateSecret(ctx, dsn, dsnURI, replicaDsn, replicaDsnURI, connInfo, gs)
}

func (r *DatabaseClaimReconciler) createSecret(ctx context.Context, dbClaim *v1.DatabaseClaim, dsn, dbURI, replicaDsn, replicaDbURI string, connInfo *v1.DatabaseClaimConnectionInfo) error {

	logr := log.FromContext(ctx)
	secretName := dbClaim.Spec.SecretName
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
			v1.DSNKey:           []byte(dsn),
			v1.DSNURIKey:        []byte(dbURI),
			v1.ReplicaDSNKey:    []byte(replicaDsn),
			v1.ReplicaDSNURIKey: []byte(replicaDbURI),
			"hostname":          []byte(connInfo.Host),
			"port":              []byte(connInfo.Port),
			"database":          []byte(connInfo.DatabaseName),
			"username":          []byte(connInfo.Username),
			"password":          []byte(connInfo.Password),
			"sslmode":           []byte(connInfo.SSLMode),
		},
	}
	logr.Info("creating connection info SECRET: "+secret.Name, "secret", secret.Name, "namespace", secret.Namespace)

	return r.Client.Create(ctx, secret)
}

func (r *DatabaseClaimReconciler) updateSecret(ctx context.Context, dsn, dbURI, replicaDsn, replicaDsnURI string, connInfo *v1.DatabaseClaimConnectionInfo, exSecret *corev1.Secret) error {

	logr := log.FromContext(ctx)

	exSecret.Data[v1.DSNKey] = []byte(dsn)
	exSecret.Data[v1.DSNURIKey] = []byte(dbURI)
	exSecret.Data[v1.ReplicaDSNKey] = []byte(replicaDsn)
	exSecret.Data[v1.ReplicaDSNURIKey] = []byte(replicaDsnURI)
	exSecret.Data["hostname"] = []byte(connInfo.Host)
	exSecret.Data["port"] = []byte(connInfo.Port)
	exSecret.Data["database"] = []byte(connInfo.DatabaseName)
	exSecret.Data["username"] = []byte(connInfo.Username)
	exSecret.Data["password"] = []byte(connInfo.Password)
	exSecret.Data["sslmode"] = []byte(connInfo.SSLMode)
	logr.Info("updating connection info SECRET: "+exSecret.Name, "secret", exSecret.Name, "namespace", exSecret.Namespace)

	return r.Client.Update(ctx, exSecret)
}
