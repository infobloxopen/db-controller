package dockerdb

import (
	"context"
	"database/sql"
	"net/url"

	. "github.com/onsi/ginkgo/v2"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func MockRDS(t GinkgoTInterface, ctx context.Context, cli client.Client, secretName, userName, databaseName string) (*sql.DB, string, func()) {
	t.Helper()

	dbCli, fakeDSN, clean := Run(Config{
		Database:  databaseName,
		Username:  userName,
		Password:  "postgres",
		DockerTag: "15",
	})

	cleanSecret := MockRDSCredentials(t, ctx, cli, fakeDSN, secretName)

	return dbCli, fakeDSN, func() {
		cleanSecret()
		clean()
	}

}

func MockRDSCredentials(t GinkgoTInterface, ctx context.Context, cli client.Client, fakeDSN, secretName string) func() {

	u, err := url.Parse(fakeDSN)
	if err != nil {
		t.Fatalf("failed to parse fakeDSN: %v", err)
	}
	pw, _ := u.User.Password()

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: "default",
		},
		Data: map[string][]byte{
			"endpoint": []byte(u.Hostname()),
			"password": []byte(pw),
			"port":     []byte(u.Port()),
			"username": []byte(u.User.Username()),
		},
	}
	if err := cli.Create(ctx, secret); err != nil {
		t.Fatalf("failed to create secret: %v", err)
	}

	return func() {
		if err := cli.Delete(ctx, secret); err != nil {
			t.Logf("failed to delete secret: %v", err)
		}
	}
}
