package databaseclaim

import (
	"context"
	"fmt"

	_ "github.com/lib/pq"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/infobloxopen/db-controller/api/v1"
	// FIXME: upgrade kubebuilder so this package will be removed
)

func getExistingDSN(ctx context.Context, cli client.Reader, dbClaim *v1.DatabaseClaim) (*v1.DatabaseClaimConnectionInfo, error) {

	ns := dbClaim.Spec.SourceDataFrom.Database.SecretRef.Namespace
	if ns == "" {
		ns = dbClaim.Namespace
	}

	secret := corev1.Secret{}
	err := cli.Get(ctx, client.ObjectKey{
		Namespace: ns,
		Name:      dbClaim.Spec.SourceDataFrom.Database.SecretRef.Name,
	}, &secret)
	if err != nil {
		return nil, err
	}

	return parseExistingDSN(secret.Data, dbClaim)
}

func parseExistingDSN(secretData map[string][]byte, dbClaim *v1.DatabaseClaim) (*v1.DatabaseClaimConnectionInfo, error) {

	if dbClaim.Spec.DSNName == "" {
		dbClaim.Spec.DSNName = defaultDSNKey
	}

	// FIXME: remove usage of password key and only read dsn.txt
	bsDSN, ok := secretData[dbClaim.Spec.DSNName]
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
