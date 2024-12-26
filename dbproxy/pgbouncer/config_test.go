package pgbouncer

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

func mustWriteFile(t *testing.T, path string, content string) string {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestConfig(t *testing.T) {

	// Create a temporary directory
	tempDir, err := os.MkdirTemp("", "pgbouncer")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	uriDSNPath := mustWriteFile(t, filepath.Join(tempDir, "uri_dsn"), `postgres://myuser_a:%2AYj0V2MmBC%3D4A%2CF@dbproxy-test-db.default.svc:5432/mydb?sslmode=disable`)
	cfgPath := filepath.Join(tempDir, "pgbouncer.ini")

	// Create a new pgbouncer configuration
	cfg, err := NewConfig(Params{
		DSNPath:   uriDSNPath,
		LocalAddr: "localhost:5432",
		OutPath:   cfgPath,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := cfg.Write(); err != nil {
		t.Fatal(err)
	}

	bs, err := ioutil.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	e := `
[databases]
mydb = host=dbproxy-test-db.default.svc port=5432 dbname=mydb

[pgbouncer]
listen_port = 5432
listen_addr = localhost
auth_type = trust
auth_file = userlist.txt
logfile = pgbouncer.log
pidfile = pgbouncer.pid
admin_users = myuser_a
remote_user_override = myuser_a
remote_db_override = mydb
ignore_startup_parameters = extra_float_digits
client_tls_sslmode = prefer
client_tls_key_file=dbproxy-client.key
client_tls_cert_file=dbproxy-client.crt
server_tls_sslmode = disable
#server_tls_key_file=dbproxy-server.key
#server_tls_cert_file=dbproxy-server.crt
`

	if string(bs) != e {
		t.Fatalf("got:\n%q\nwanted:\n%q", string(bs), e)
	}

	// No change should result in duplicate error
	if err := cfg.Write(); err != ErrDuplicateWrite {
		t.Fatal(err)
	}

	// Change should result in successful write
	cfg.LocalHost = "0.0.0.0"
	if err := cfg.Write(); err != nil {
		t.Fatal(err)
	}

	e = `
[databases]
mydb = host=dbproxy-test-db.default.svc port=5432 dbname=mydb

[pgbouncer]
listen_port = 5432
listen_addr = localhost
auth_type = trust
auth_file = userlist.txt
logfile = pgbouncer.log
pidfile = pgbouncer.pid
admin_users = myuser_a
remote_user_override = myuser_a
remote_db_override = mydb
ignore_startup_parameters = extra_float_digits
client_tls_sslmode = prefer
client_tls_key_file=dbproxy-client.key
client_tls_cert_file=dbproxy-client.crt
server_tls_sslmode = disable
#server_tls_key_file=dbproxy-server.key
#server_tls_cert_file=dbproxy-server.crt
`

	if string(bs) != e {
		t.Fatalf("got:\n%q\nwanted:\n%q", string(bs), e)
	}

	// Update creds and ensure auth file is changed
	mustWriteFile(t, filepath.Join(tempDir, "uri_dsn"), `postgres://myuser_a:updated@dbproxy-test-db.default.svc:5432/mydb?sslmode=disable`)
	if err := cfg.Write(); err != nil {
		t.Fatal(err)
	}

	bs, err = ioutil.ReadFile(filepath.Join(filepath.Dir(cfgPath), "userlist.txt"))
	if err != nil {
		t.Fatal(err)
	}

	if e := `"myuser_a" "updated"`; string(bs) != e {
		t.Fatalf("got:\n%q\nwanted:\n%q", string(bs), e)
	}
}
