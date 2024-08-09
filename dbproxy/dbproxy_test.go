package dbproxy

import (
	"context"
	"testing"
	"time"
)

func TestE2E(t *testing.T) {

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := Start(ctx, Config{
		DBCredentialPath: "db-credential",
		DBPasswordPath:   "db-password",
		PGCredentialPath: "pgbouncer.ini",
		PGBStartScript:   "/var/run/dbproxy/start-pgbouncer.sh",
		PGBReloadScript:  "/var/run/dbproxy/reload-pgbouncer.sh",
		Port:             5432,
	})

	if err != nil {
		t.Fatal(err)
	}

}
