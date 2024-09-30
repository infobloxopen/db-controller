package auth

import (
	"context"
	"fmt"

	_ "github.com/lib/pq"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	v1 "github.com/infobloxopen/db-controller/api/v1"
)

var ErrInvalidCredentialsPasswordMissing = fmt.Errorf("invalid_credentials_password_missing")

func getSourceDataFromDSN(ctx context.Context, cli client.Reader, dbClaim *v1.DatabaseClaim) (*v1.DatabaseClaimConnectionInfo, error) {

	ns := dbClaim.Spec.SourceDataFrom.Database.SecretRef.Namespace
	if ns == "" {
		ns = dbClaim.Namespace
	}
	key := client.ObjectKey{
		Namespace: ns,
		Name:      dbClaim.Spec.SourceDataFrom.Database.SecretRef.Name,
	}

	secret := corev1.Secret{}
	err := cli.Get(ctx, key, &secret)
	if err != nil {
		return nil, err
	}

	return parseExistingDSN(ctx, secret.Data, dbClaim)
}

func parseExistingDSN(ctx context.Context, secretData map[string][]byte, dbClaim *v1.DatabaseClaim) (*v1.DatabaseClaimConnectionInfo, error) {

	logr := log.FromContext(ctx)
	for k, v := range secretData {
		logr.Info("parseExistingDSN", "key", k, "value", string(v))
	}
	bsDSN, ok := secretData[v1.DSNURIKey]
	if ok {
		uriDSN, err := v1.ParseUri(string(bsDSN))
		if err == nil {
			return uriDSN, nil
		}
	}

	// DSN is not provided in secret, try to read it from the claim and inject the password
	connInfo, err := v1.ParseUri(dbClaim.Spec.SourceDataFrom.Database.DSN)
	if err != nil {
		return nil, fmt.Errorf("unable_read_dsn: %w", err)
	}

	// Populate fields from duplicated fields in the claim itself
	if connInfo.Username == "" {
		connInfo.Username = dbClaim.Spec.Username
	}

	// No password in DSN, so lets check password field
	if connInfo.Password == "" {
		password, passOK := secretData["password"]
		if !passOK {
			return nil, ErrInvalidCredentialsPasswordMissing
		}
		connInfo.Password = string(password)
	}

	return connInfo, nil
}
