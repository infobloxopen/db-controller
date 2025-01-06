package databaseclaim

import (
	"context"
	"fmt"
	"strings"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	_ "github.com/lib/pq"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	v1 "github.com/infobloxopen/db-controller/api/v1"
	"github.com/infobloxopen/db-controller/pkg/dbclient"
	"github.com/infobloxopen/db-controller/pkg/pgctl"

	// FIXME: upgrade kubebuilder so this package will be removed
	"k8s.io/apimachinery/pkg/api/errors"
)

func (r *DatabaseClaimReconciler) createOrUpdateSecret(ctx context.Context, dbClaim *v1.DatabaseClaim, connInfo *v1.DatabaseClaimConnectionInfo, cloud string) error {
	gs := &corev1.Secret{}
	dbType := dbClaim.Spec.Type
	secretName := dbClaim.Spec.SecretName
	var dsn, dsnURI, replicaDsnURI string = "", "", ""

	switch dbType {
	case v1.Postgres:
		fallthrough
	case v1.AuroraPostgres:
		dsn = dbclient.PostgresConnectionString(connInfo.Host, connInfo.Port, connInfo.Username, connInfo.Password, connInfo.DatabaseName, connInfo.SSLMode)
		dsnURI = dbclient.PostgresURI(connInfo.Host, connInfo.Port, connInfo.Username, connInfo.Password, connInfo.DatabaseName, connInfo.SSLMode)

		if cloud == "aws" {
			replicaDsnURI = strings.Replace(dsnURI, ".cluster-", ".cluster-ro-", -1)
		}
	default:
		return fmt.Errorf("unknown DB type")
	}

	err := r.Client.Get(ctx, client.ObjectKey{
		Namespace: dbClaim.Namespace,
		Name:      secretName,
	}, gs)

	useExistingSource := dbClaim.Spec.UseExistingSource != nil && *dbClaim.Spec.UseExistingSource
	if dbClaim.Status.ActiveDB.ConnectionInfo == nil && err == nil && !useExistingSource {
		return fmt.Errorf(
			"secret %s already exists in namespace %s, but no active database connection info is available",
			secretName, dbClaim.Namespace,
		)
	}

	if err != nil && errors.IsNotFound(err) {
		if err := r.createSecret(ctx, dbClaim, dsn, dsnURI, replicaDsnURI, connInfo); err != nil {
			return err
		}
		return nil
	}

	return r.updateSecret(ctx, dbClaim, dsn, dsnURI, replicaDsnURI, connInfo, gs)
}

func (r *DatabaseClaimReconciler) createSecret(ctx context.Context, dbClaim *v1.DatabaseClaim, dsn, dbURI, replicaDbURI string, connInfo *v1.DatabaseClaimConnectionInfo) error {
	logr := log.FromContext(ctx)
	secretName := dbClaim.Spec.SecretName

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: dbClaim.Namespace,
			Name:      secretName,
			Labels:    map[string]string{"app.kubernetes.io/managed-by": "db-controller"},
		},
		Data: map[string][]byte{
			v1.DSNKey:           []byte(dsn),
			v1.DSNURIKey:        []byte(dbURI),
			v1.ReplicaDSNURIKey: []byte(replicaDbURI),
			"hostname":          []byte(connInfo.Host),
			"port":              []byte(connInfo.Port),
			"database":          []byte(connInfo.DatabaseName),
			"username":          []byte(connInfo.Username),
			"password":          []byte(connInfo.Password),
			"sslmode":           []byte(connInfo.SSLMode),
		},
	}
	if dbClaim.Spec.DSNName != "" && dbClaim.Spec.DSNName != v1.DSNKey {
		secret.Data[dbClaim.Spec.DSNName] = []byte(dsn)
		secret.Data["uri_"+dbClaim.Spec.DSNName] = []byte(dbURI)
	}

	logr.Info("creating connection info SECRET: "+secret.Name, "secret", secret.Name, "namespace", secret.Namespace)

	return r.Client.Create(ctx, secret)
}

func (r *DatabaseClaimReconciler) updateSecret(ctx context.Context, dbClaim *v1.DatabaseClaim, dsn, dbURI, replicaDsnURI string, connInfo *v1.DatabaseClaimConnectionInfo, exSecret *corev1.Secret) error {

	logr := log.FromContext(ctx)

	exSecret.Data[v1.DSNKey] = []byte(dsn)
	exSecret.Data[v1.DSNURIKey] = []byte(dbURI)
	exSecret.Data[v1.ReplicaDSNURIKey] = []byte(replicaDsnURI)
	exSecret.Data["hostname"] = []byte(connInfo.Host)
	exSecret.Data["port"] = []byte(connInfo.Port)
	exSecret.Data["database"] = []byte(connInfo.DatabaseName)
	exSecret.Data["username"] = []byte(connInfo.Username)
	exSecret.Data["password"] = []byte(connInfo.Password)
	exSecret.Data["sslmode"] = []byte(connInfo.SSLMode)
	logr.Info("updating connection info SECRET: "+exSecret.Name, "secret", exSecret.Name, "namespace", exSecret.Namespace)

	if dbClaim.Spec.DSNName != "" && dbClaim.Spec.DSNName != v1.DSNKey {
		exSecret.Data[dbClaim.Spec.DSNName] = []byte(dsn)
		exSecret.Data["uri_"+dbClaim.Spec.DSNName] = []byte(dbURI)
	}

	return r.Client.Update(ctx, exSecret)
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

	tSecret.Data[tempSourceDsn] = []byte(dsn)
	return r.Client.Update(ctx, tSecret)
}

func (r *DatabaseClaimReconciler) setTargetPasswordInTempSecret(ctx context.Context, password string, dbClaim *v1.DatabaseClaim) error {

	tSecret, err := r.getTempSecret(ctx, dbClaim)
	if err != nil {
		return err
	}

	tSecret.Data[tempTargetPassword] = []byte(password)
	return r.Client.Update(ctx, tSecret)
}

func (r *DatabaseClaimReconciler) setMasterPasswordInTempSecret(ctx context.Context, password string, dbClaim *v1.DatabaseClaim) error {

	tSecret, err := r.getTempSecret(ctx, dbClaim)
	if err != nil {
		return err
	}

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

	if err == nil {
		logr.Info("secret exists returning temp secret", "name", secretName)
		return gs, nil
	}
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
			tempTargetDsn:                   nil,
			cachedMasterPasswdForExistingDB: nil,
		},
	}

	logr.V(1).Info("creating temp secret", "secret", fmt.Sprintf("%s/%s", secret.Namespace, secret.Name))
	err = r.Client.Create(ctx, secret)
	return secret, err

}

func getTempSecretName(dbClaim *v1.DatabaseClaim) string {
	return "temp-" + dbClaim.Spec.SecretName
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

// manageMasterPassword creates the master password that
// crossplane uses to create root user in the database.
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
		logr.Info("creating master secret", "name", secret.Name, "namespace", secret.Namespace, "key", secret.Key)
		return r.Client.Create(ctx, masterSecret)
	}
	logr.Info("master secret exists")
	return nil
}
