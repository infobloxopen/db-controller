package pgbouncer

import (
	"context"
	"os"
	"path"
	"testing"

	"github.com/go-logr/logr/testr"
)

func TestParseURI(t *testing.T) {
	tests := []struct {
		name    string
		dsn     string
		wantErr bool
	}{
		{
			name:    "invalid scheme",
			dsn:     "invalid://localhost:5432",
			wantErr: true,
		},
		{
			name:    "invalid port",
			dsn:     "postgres://localhost:invalid",
			wantErr: true,
		},
		{
			name:    "valid",
			dsn:     "postgres://localhost:5432",
			wantErr: false,
		},
		{
			name:    "valid with database",
			dsn:     "postgres://localhost:5432/dbname",
			wantErr: false,
		},
		{
			name:    "valid with sslmode",
			dsn:     "postgres://localhost:5432/dbname?sslmode=disable",
			wantErr: false,
		},
	}
	var cfg PGBouncerConfig
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := parseURI(&cfg, tt.dsn)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseURI() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}

func TestStart(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	testLogger := testr.New(t)

	for _, script := range []string{"start-pgbouncer.sh", "reload-pgbouncer.sh"} {

		script := path.Join(wd, "..", "scripts", script)

		err := run(context.TODO(), script, testLogger)
		if err == nil {
			t.Fatal("expected error")
		}

		// if !strings.Contains(out, "pgbouncer: not found") {
		// 	t.Fatalf("expected pgbouncer not found got:\n%s", out)
		// }
	}
}
