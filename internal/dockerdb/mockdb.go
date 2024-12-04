package dockerdb

import (
	"context"
	"database/sql"
	"fmt"
	"io/ioutil"
	"net/url"
	"strings"

	. "github.com/onsi/ginkgo/v2"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// MustSQL will open a file and exec it on the database.
// It will panic on any errors
func MustSQL(ctx context.Context, db *sql.DB, fileName string, args ...any) {
	bs, err := ioutil.ReadFile(fileName)
	if err != nil {
		panic(err)
	}
	_, err = db.ExecContext(ctx, string(bs), args...)
	if err != nil {
		panic(err)
	}
}

func MockRDS(t GinkgoTInterface, ctx context.Context, cli client.Client, secretName, userName, databaseName string) (*sql.DB, string, func()) {
	t.Helper()

	logger := log.FromContext(ctx).WithName("mockrds")

	dbCli, fakeDSN, clean := Run(logger, Config{
		Database:  databaseName,
		Username:  userName,
		Password:  "postgres",
		DockerTag: "15",
	})

	fakeDSN = strings.Replace(fakeDSN, "localhost", "127.0.0.1", 1)

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

	return dbCli, fakeDSN, func() {
		if err := cli.Delete(ctx, secret); err != nil {
			panic(fmt.Sprintf("failed to delete secret: %v", err))
		}
		clean()
	}
}
