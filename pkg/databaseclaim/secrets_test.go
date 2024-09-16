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

	err := dbClaimReconciler.updateSecret(ctx, "dsn", "dsnUri", "ro_dsnUri", &claimConnInfo, &secret)

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
