package databaseclaim

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

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
			if (err != nil) != tt.wantErr {
				t.Fatalf("Test '%s' failed: unexpected error state: got err=%v, wantErr=%v", tt.name, err, tt.wantErr)
			}
			if err != nil {
				t.Logf("Expected error for '%s': %v", tt.name, err)
			}
		})
	}
}

func TestCreateSecret(t *testing.T) {

	RegisterFailHandler(Fail)

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

	secret := mockClient.CreatedObject.(*corev1.Secret)

	Expect(secret.Data[v1.DSNKey]).To(Equal([]byte("dsn")))
	Expect(secret.Data[v1.DSNURIKey]).To(Equal([]byte("dsnUri")))
	Expect(secret.Data[v1.ReplicaDSNURIKey]).To(Equal([]byte("ro_dsnUri")))
	Expect(secret.Data["hostname"]).To(Equal([]byte("host")))
	Expect(secret.Data["port"]).To(Equal([]byte("123")))
	Expect(secret.Data["database"]).To(Equal([]byte("dbName")))
	Expect(secret.Data["username"]).To(Equal([]byte("user")))
	Expect(secret.Data["password"]).To(Equal([]byte("pass")))
	Expect(secret.Data["sslmode"]).To(Equal([]byte("ssl")))

	if err != nil {
		t.Errorf("Error updating secret: %s", err)
	}
}

func TestUpdateSecret(t *testing.T) {

	RegisterFailHandler(Fail)

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

	Expect(secret.Data[v1.DSNKey]).To(Equal([]byte("dsn")))
	Expect(secret.Data[v1.DSNURIKey]).To(Equal([]byte("dsnUri")))
	Expect(secret.Data[v1.ReplicaDSNURIKey]).To(Equal([]byte("ro_dsnUri")))
	Expect(secret.Data["hostname"]).To(Equal([]byte("host")))
	Expect(secret.Data["port"]).To(Equal([]byte("123")))
	Expect(secret.Data["database"]).To(Equal([]byte("dbName")))
	Expect(secret.Data["username"]).To(Equal([]byte("user")))
	Expect(secret.Data["password"]).To(Equal([]byte("pass")))
	Expect(secret.Data["sslmode"]).To(Equal([]byte("ssl")))

	if err != nil {
		t.Errorf("Error updating secret: %s", err)
	}
}
