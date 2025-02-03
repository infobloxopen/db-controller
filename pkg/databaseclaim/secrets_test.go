package databaseclaim

import (
	"context"
	"github.com/stretchr/testify/assert"
	"testing"

	v1 "github.com/infobloxopen/db-controller/api/v1"
	role "github.com/infobloxopen/db-controller/pkg/roleclaim"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCreateOrUpdateSecret(t *testing.T) {
	ctx := context.Background()

	claimConnInfo := v1.DatabaseClaimConnectionInfo{
		Host:         "host",
		Port:         "123",
		DatabaseName: "dbName",
		Username:     "user",
		Password:     "pass",
		SSLMode:      "ssl",
	}

	boolTrue := true
	boolFalse := false
	type args struct {
		secretName        string
		namespace         string
		cloud             string
		databaseType      v1.DatabaseType
		useExistingSource *bool
		activeConnection  *v1.DatabaseClaimConnectionInfo
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "New db, no secret present, aurora postgres should not fail",
			args: args{
				secretName:        "new-secret",
				namespace:         "testNamespace",
				cloud:             "aws",
				databaseType:      v1.AuroraPostgres,
				useExistingSource: nil,
				activeConnection:  &claimConnInfo,
			},
			wantErr: false,
		},
		{
			name: "Fail when ActiveDB connection info is missing and secret exists",
			args: args{
				secretName:        "existing-secret",
				namespace:         "namespace6",
				cloud:             "aws",
				databaseType:      v1.AuroraPostgres,
				useExistingSource: &boolFalse,
				activeConnection:  nil,
			},
			wantErr: true,
		},
		{
			name: "No activeDB but existing datasource set, no error",
			args: args{
				secretName:        "new-secret",
				namespace:         "namespace7",
				cloud:             "aws",
				databaseType:      v1.AuroraPostgres,
				useExistingSource: &boolTrue,
				activeConnection:  nil,
			},
			wantErr: false,
		},
		{
			name: "Fail on unknown database type",
			args: args{
				secretName:        "unknown-db-secret",
				namespace:         "namespace3",
				cloud:             "aws",
				databaseType:      "UnknownType",
				useExistingSource: nil,
				activeConnection:  &claimConnInfo,
			},
			wantErr: true,
		},
		{
			name: "Fail on missing secret name",
			args: args{
				secretName:        "",
				namespace:         "namespace4",
				cloud:             "aws",
				databaseType:      v1.Postgres,
				useExistingSource: nil,
				activeConnection:  &claimConnInfo,
			},
			wantErr: true,
		},
		{
			name: "Fail on missing namespace",
			args: args{
				secretName:        "no-namespace-secret",
				namespace:         "",
				cloud:             "aws",
				databaseType:      v1.Postgres,
				useExistingSource: nil,
				activeConnection:  &claimConnInfo,
			},
			wantErr: true,
		},
		{
			name: "AuroraPostgres with valid connection info",
			args: args{
				secretName:        "new-secret",
				namespace:         "namespace5",
				cloud:             "aws",
				databaseType:      v1.AuroraPostgres,
				useExistingSource: nil,
				activeConnection:  &claimConnInfo,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := role.MockClient{}
			dbClaimReconciler := DatabaseClaimReconciler{
				Client: &mockClient,
			}

			dbClaim := v1.DatabaseClaim{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: tt.args.namespace,
				},
				Spec: v1.DatabaseClaimSpec{
					SecretName:        tt.args.secretName,
					Type:              tt.args.databaseType,
					UseExistingSource: tt.args.useExistingSource,
				},
				Status: v1.DatabaseClaimStatus{
					ActiveDB: v1.Status{
						ConnectionInfo: tt.args.activeConnection,
					},
				},
			}

			err := dbClaimReconciler.createOrUpdateSecret(ctx, &dbClaim, &claimConnInfo, tt.args.cloud)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCreateSecret(t *testing.T) {
	mockClient := role.MockClient{}
	dbClaimReconciler := DatabaseClaimReconciler{
		Client: &mockClient,
	}

	ctx := context.Background()
	claimConnInfo := v1.DatabaseClaimConnectionInfo{
		Host:         "host",
		Port:         "123",
		DatabaseName: "dbName",
		Username:     "user",
		Password:     "pass",
		SSLMode:      "ssl",
	}

	dbClaim := v1.DatabaseClaim{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dbClaim",
			Namespace: "testNamespace",
		},
		Spec: v1.DatabaseClaimSpec{
			SecretName: "create-master-secret",
		},
		Status: v1.DatabaseClaimStatus{},
	}

	err := dbClaimReconciler.createSecret(ctx, &dbClaim, "dsn", "dsnUri", "ro_dsnUri", &claimConnInfo)
	assert.NoError(t, err, "Error updating secret")

	secret := mockClient.CreatedObject.(*corev1.Secret)

	assert.Equal(t, []byte("dsn"), secret.Data[v1.DSNKey], "DSN should match")
	assert.Equal(t, []byte("dsnUri"), secret.Data[v1.DSNURIKey], "DSNURI should match")
	assert.Equal(t, []byte("ro_dsnUri"), secret.Data[v1.ReplicaDSNURIKey], "ReplicaDSNURI should match")
	assert.Equal(t, []byte("host"), secret.Data["hostname"], "Hostname should match")
	assert.Equal(t, []byte("123"), secret.Data["port"], "Port should match")
	assert.Equal(t, []byte("dbName"), secret.Data["database"], "DatabaseName should match")
	assert.Equal(t, []byte("user"), secret.Data["username"], "Username should match")
	assert.Equal(t, []byte("pass"), secret.Data["password"], "Password should match")
	assert.Equal(t, []byte("ssl"), secret.Data["sslmode"], "SSLMode should match")
}

func TestUpdateSecret(t *testing.T) {
	mockClient := role.MockClient{}
	dbClaimReconciler := DatabaseClaimReconciler{
		Client: &mockClient,
	}

	ctx := context.Background()
	claimConnInfo := v1.DatabaseClaimConnectionInfo{
		Host:         "host",
		Port:         "123",
		DatabaseName: "dbName",
		Username:     "user",
		Password:     "pass",
		SSLMode:      "ssl",
	}
	secret := corev1.Secret{
		Data: make(map[string][]byte),
	}
	dbClaim := v1.DatabaseClaim{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dbClaim",
			Namespace: "testNamespace",
		},
		Spec: v1.DatabaseClaimSpec{
			SecretName: "create-master-secret",
		},
		Status: v1.DatabaseClaimStatus{},
	}

	err := dbClaimReconciler.updateSecret(ctx, &dbClaim, "dsn", "dsnUri", "ro_dsnUri", &claimConnInfo, &secret)
	assert.NoError(t, err, "Error updating secret")

	assert.Equal(t, []byte("dsn"), secret.Data[v1.DSNKey], "DSN should match")
	assert.Equal(t, []byte("dsnUri"), secret.Data[v1.DSNURIKey], "DSNURI should match")
	assert.Equal(t, []byte("ro_dsnUri"), secret.Data[v1.ReplicaDSNURIKey], "ReplicaDSNURI should match")
	assert.Equal(t, []byte("host"), secret.Data["hostname"], "Hostname should match")
	assert.Equal(t, []byte("123"), secret.Data["port"], "Port should match")
	assert.Equal(t, []byte("dbName"), secret.Data["database"], "DatabaseName should match")
	assert.Equal(t, []byte("user"), secret.Data["username"], "Username should match")
	assert.Equal(t, []byte("pass"), secret.Data["password"], "Password should match")
	assert.Equal(t, []byte("ssl"), secret.Data["sslmode"], "SSLMode should match")
}
