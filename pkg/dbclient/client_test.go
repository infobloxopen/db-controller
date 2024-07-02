package dbclient

import (
	"database/sql"
	"flag"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
)

const succeed = "\u2713"
const failed = "\u2717"

var sqlDB *sql.DB

// The following gingo struct and associted init() is required to run go test with ginkgo related flags
// Since this test is not using ginkgo, this is a hack to get around the issue of go test complaining about
// unknown flags.
var ginkgo struct {
	dry_run      string
	label_filter string
}

func init() {
	flag.StringVar(&ginkgo.dry_run, "ginkgo.dry-run", "", "Ignore this flag")
	flag.StringVar(&ginkgo.label_filter, "ginkgo.label-filter", "", "Ignore this flag")
}

type testDB struct {
	t        *testing.T
	port     int
	username string
	password string
	resource *dockertest.Resource
	pool     *dockertest.Pool
}

func (t *testDB) URL() string {
	u := url.URL{
		Scheme: "postgres",
		Host:   fmt.Sprintf("localhost:%d", t.port),
		User:   url.UserPassword(t.username, t.password),
		Path:   "/postgres",
	}
	q := u.Query()
	q.Set("sslmode", "disable")
	u.RawQuery = q.Encode()
	return u.String()
}

func (t *testDB) Close() {
	t.t.Log("Tearing down dockertest resource of PostgreSQL DB")
	if err := t.pool.Purge(t.resource); err != nil {
		t.t.Errorf("Could not purge resource: %s", err)
	}
}

func (t *testDB) OpenUser(dbname, username, password string) (*sql.DB, error) {
	if dbname == "" {
		dbname = "postgres"
	}
	dbConnStr := fmt.Sprintf("host=localhost port=%d user=%s dbname=%s password=%s sslmode=disable",
		t.port, username, dbname, password)
	return sql.Open("postgres", dbConnStr)
}

func (t *testDB) OpenUserWithURI(dbname, username, password string) (*sql.DB, error) {
	if dbname == "" {
		dbname = "postgres"
	}

	dbConnStr := PostgresURI(fmt.Sprintf("localhost:%d", t.port), username, password, dbname, "disable")
	t.t.Log("dbConnStr", dbConnStr)
	return sql.Open("postgres", dbConnStr)
}

func (t *testDB) OpenUserWithConnectionString(dbname, username, password string) (*sql.DB, error) {
	if dbname == "" {
		dbname = "postgres"
	}
	dbConnStr := PostgresConnectionString("localhost", strconv.Itoa(t.port), username, password, dbname, "disable")
	t.t.Log("dbConnStr", dbConnStr)
	return sql.Open("postgres", dbConnStr)
}

func openPort(t *testing.T) (string, int) {
	port, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("Unable to find an opened port for DB: %v", err)
	}
	defer port.Close()

	return port.Addr().String(), port.Addr().(*net.TCPAddr).Port

}

func setupSqlDB(t *testing.T) *testDB {
	t.Helper()
	t.Log("Setting up an instance of PostgreSQL DB with dockertest")

	addr, port := openPort(t)

	t.Log("Got available addr for DB: ", addr)
	user := "test"
	pass := "password"

	// Create a new pool for docker containers
	pool, err := dockertest.NewPool("")
	if err != nil {
		t.Fatalf("Could not construct pool: %s", err)
	}
	pool.MaxWait = 15 * time.Second

	// uses pool to try to connect to Docker
	err = pool.Client.Ping()
	if err != nil {
		t.Fatalf("Could not connect to Docker: %s", err)
	}

	resource, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "postgres",
		Tag:        "15",
		Env: []string{
			"POSTGRES_USER=" + user,
			"POSTGRES_PASSWORD=" + pass,
			"POSTGRES_DB=postgres",
		},
		ExposedPorts: []string{"5432"},
		PortBindings: map[docker.Port][]docker.PortBinding{
			"5432": {
				{HostIP: "0.0.0.0", HostPort: strconv.Itoa(port)},
			},
		},
	}, func(config *docker.HostConfig) {
		// set AutoRemove to true for the stopped container to go away by itself
		config.AutoRemove = true
		config.RestartPolicy = docker.RestartPolicy{
			Name: "no",
		}
	})

	// Exponential retry to connect to database while it is booting
	if err := pool.Retry(func() error {
		dbConnStr := PostgresURI(addr, user, pass, "postgres", "disable")
		sqlDB, err = sql.Open("postgres", dbConnStr)
		if err != nil {
			t.Log("Database is not ready yet (it is booting up, wait for a few tries)...")
			return err
		}
		// Tests if database is reachable
		return sqlDB.Ping()
	}); err != nil {
		t.Fatalf("Could not connect to docker: %s", err)
	}
	return &testDB{
		t:        t,
		username: user,
		password: pass,
		port:     port,
		resource: resource,
		pool:     pool,
	}
}

func TestPostgresClientOperations(t *testing.T) {
	testDB := setupSqlDB(t)
	defer testDB.Close()
	type mockClient struct {
		dbType string
		DB     *sql.DB
		log    logr.Logger
	}
	type args struct {
		dbName       string
		role         string
		username     string
		userPassword string
		newUsername  string
		newPassword  string
	}
	tests := []struct {
		name    string
		fields  mockClient
		args    args
		want    bool
		wantErr bool
	}{
		{
			"TEST DB client operations",
			mockClient{
				dbType: "postgres",
				DB:     sqlDB,
				log:    logr.Discard(),
			},
			args{
				"test_db",
				"test_role",
				"test-user",
				"test_password",
				"new-test-user",
				"password",
			},
			true,
			false,
		},
	}
	oldDefaultExtionsions := getDefaulExtensions
	defer func() {
		getDefaulExtensions = oldDefaultExtionsions
	}()
	// This overrides a var getDefaulExtensions because some of the extensions are not supported by the postgres image used by dockertest.
	// we chose to ignore those extensions in unit test as the focus of db-controller is not use these extensions - but just to make sure the mechanism of creating extensions
	// are working.
	getDefaulExtensions = func() []string {
		return []string{"citext", "uuid-ossp",
			"pgcrypto", "hstore", "pg_stat_statements", "plpgsql"}
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pc := &client{
				dbType: tt.fields.dbType,
				dbURL:  testDB.URL(),
				DB:     tt.fields.DB,
				log:    tt.fields.log,
			}

			t.Logf("CreateDataBase()")
			got, err := pc.CreateDataBase(tt.args.dbName)
			if (err != nil) != tt.wantErr {
				t.Errorf("\t%s CreateDataBase() error = %v, wantErr %v", failed, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("\t%sCreateDataBase() got = %v, want %v", failed, got, tt.want)
			}

			exists := false
			err = pc.DB.QueryRow("SELECT EXISTS(SELECT datname FROM pg_catalog.pg_database WHERE datname = $1)", tt.args.dbName).Scan(&exists)
			if err != nil {
				t.Errorf("\t%s CreateDataBase() error = %v", failed, err)
			}

			if exists {
				t.Logf("\t%s DB %v has been created", succeed, tt.args.dbName)
			} else {
				t.Errorf("\t%s can't find DB %v", failed, tt.args.dbName)
			}
			t.Logf("\t%s CreateDataBase() is passed", succeed)

			t.Logf("CreateGroup()")
			got, err = pc.CreateGroup(tt.args.dbName, tt.args.role)
			if (err != nil) != tt.wantErr {
				t.Errorf("\t%s CreateGroup() error = %v, wantErr %v", failed, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("\t%sCreateGroup() got = %v, want %v", failed, got, tt.want)
			}

			err = pc.DB.QueryRow("SELECT EXISTS(SELECT pg_roles.rolname FROM pg_catalog.pg_roles where pg_roles.rolname = $1)", tt.args.role).Scan(&exists)
			if err != nil {
				t.Errorf("\t%s CreateGroup error = %v", failed, err)
			}

			if exists {
				t.Logf("\t%s role %v has been created", succeed, tt.args.role)
			} else {
				t.Errorf("\t%s can't find user %v", failed, tt.args.role)
			}
			t.Logf("\t%s CreateGroup() is passed", succeed)

			t.Logf("CreateUser()")
			got, err = pc.CreateUser(tt.args.username, tt.args.role, tt.args.userPassword)
			if (err != nil) != tt.wantErr {
				t.Errorf("\t%s CreateUser() error = %v, wantErr %v", failed, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("\t%sCreateUser() got = %v, want %v", failed, got, tt.want)
			}

			err = pc.DB.QueryRow("SELECT EXISTS(SELECT pg_user.usename FROM pg_catalog.pg_user where pg_user.usename = $1)", tt.args.username).Scan(&exists)
			if err != nil {
				t.Errorf("\t%s CreateUser error = %v", failed, err)
			}

			if exists {
				t.Logf("\t%s user %v has been created", succeed, tt.args.username)
			} else {
				t.Errorf("\t%s can't find user %v", failed, tt.args.username)
			}
			t.Logf("\t%s CreateUser() is passed", succeed)

			t.Logf("UpdateUser()")
			err = pc.UpdateUser(tt.args.username, tt.args.newUsername, tt.args.role, tt.args.newPassword)
			if (err != nil) != tt.wantErr {
				t.Errorf("\t%s UpdateUser() error = %v, wantErr %v", failed, err, tt.wantErr)
				return
			}

			err = pc.DB.QueryRow("SELECT EXISTS(SELECT pg_user.usename FROM pg_catalog.pg_user where pg_user.usename = $1)", tt.args.newUsername).Scan(&exists)
			if err != nil {
				t.Errorf("\t%s UpdateUser error = %v", failed, err)
			}

			if exists {
				t.Logf("\t%s user %v has been updated", succeed, tt.args.newUsername)
			} else {
				t.Errorf("\t%s can't find new user %v", failed, tt.args.newUsername)
			}
			t.Logf("\t%s UpdateUser() is passed", succeed)

			t.Logf("Enable superuser access")
			//setup rds_superuser to simulate RDS superuser
			_, _ = pc.DB.Exec("CREATE ROLE rds_superuser")
			err = pc.ManageSuperUserRole(tt.args.newUsername, true)
			if (err != nil) != tt.wantErr {
				t.Errorf("\t%s ManageSuperUserRole() error = %v, wantErr %v", failed, err, tt.wantErr)
				return
			}
			// check if user has superuser access enabled in AWS
			err = pc.DB.QueryRow(`SELECT EXISTS (SELECT 1
				FROM pg_roles 
				WHERE pg_has_role( $1, oid, 'member')
				AND rolname in ( 'rds_superuser'))`, tt.args.newUsername).Scan(&exists)
			if err != nil {
				t.Errorf("\t%s ManageSuperUserRole error = %v", failed, err)
				return
			}
			if !exists {
				t.Errorf("\t%s user %v has superuser access disabled, wantErr %v", failed, tt.args.newUsername, tt.want)
				return
			}
			t.Logf("\t%s Enable superuser access passed", succeed)
			t.Logf("Disable superuser access")
			err = pc.ManageSuperUserRole(tt.args.newUsername, false)
			if (err != nil) != tt.wantErr {
				t.Errorf("\t%s ManageSuperUserRole() error = %v, wantErr %v", failed, err, tt.wantErr)
				return
			}
			// check if user has superuser access enabled in AWS
			err = pc.DB.QueryRow(`SELECT EXISTS (SELECT 1
				FROM pg_roles 
				WHERE pg_has_role( $1, oid, 'member')
				AND rolname in ( 'rds_superuser'))`, tt.args.newUsername).Scan(&exists)
			if err != nil {
				t.Errorf("\t%s ManageSuperUserRole error = %v", failed, err)
				return
			}
			if exists {
				t.Errorf("\t%s user %v has superuser access Enabled, wantErr %v", failed, tt.args.newUsername, tt.want)
				return
			}
			t.Logf("\t%s Disable superuser access passed", succeed)

			t.Logf("Disable replication role")
			replicationQueryStmt, _ := pc.DB.Prepare(`select exists (
				SELECT 1
				FROM pg_roles 
				WHERE pg_has_role( $1, oid, 'member')
				AND rolname in ( 'rds_superuser', 'rds_replication')
				UNION
				SELECT 1
				FROM pg_roles 
				WHERE rolname = $1
				AND rolreplication = 't'
				)`)

			err = pc.ManageReplicationRole(tt.args.newUsername, false)
			if (err != nil) != tt.wantErr {
				t.Errorf("\t%s ManageReplicationRole() error = %v, wantErr %v", failed, err, tt.wantErr)
				return
			}
			// check if user has replication role enabled in AWS and non AWS postgres environments
			err = replicationQueryStmt.QueryRow(tt.args.newUsername).Scan(&exists)
			if err != nil {
				t.Errorf("\t%s ManageReplicationRole error = %v", failed, err)
			}
			if !exists {
				t.Logf("\t%s user %v has replication role disabled", succeed, tt.args.newUsername)
			} else {
				t.Errorf("\t%s user %v does not have replication role disabled", failed, tt.args.newUsername)
			}
			t.Logf("Enable replication role")
			err = pc.ManageReplicationRole(tt.args.newUsername, true)
			if (err != nil) != tt.wantErr {
				t.Errorf("\t%s ManageReplicationRole() error = %v, wantErr %v", failed, err, tt.wantErr)
				return
			}
			// check if user has replication role enabled in AWS and non AWS postgres environments
			err = replicationQueryStmt.QueryRow(tt.args.newUsername).Scan(&exists)
			if err != nil {
				t.Errorf("\t%s ManageReplicationRole error = %v", failed, err)
			}
			if exists {
				t.Logf("\t%s user %v has replication role enabled", succeed, tt.args.newUsername)
			} else {
				t.Errorf("\t%s user %v does not have replication role enabled", failed, tt.args.newUsername)
			}
			t.Logf("\t%s ManageReplicationRole() is passed", succeed)

			t.Logf("Enable RDS replication role")

			//create rds_replication to simulate RDS replication role
			_ = pc.ManageReplicationRole(tt.args.newUsername, false)
			_, _ = pc.DB.Exec("CREATE ROLE rds_replication")
			err = pc.ManageReplicationRole(tt.args.newUsername, true)
			if (err != nil) != tt.wantErr {
				t.Errorf("\t%s ManageReplicationRole() error = %v, wantErr %v", failed, err, tt.wantErr)
				return
			}
			// check if user has replication role enabled in AWS and non AWS postgres environments
			err = replicationQueryStmt.QueryRow(tt.args.newUsername).Scan(&exists)
			if err != nil {
				t.Errorf("\t%s ManageReplicationRole error = %v", failed, err)
			}
			if exists {
				t.Logf("\t%s user %v has rds replication role enabled", succeed, tt.args.newUsername)
			} else {
				t.Errorf("\t%s user %v does not have rds replication role enabled", failed, tt.args.newUsername)
			}

			t.Logf("Disable RDS replication role")
			err = pc.ManageReplicationRole(tt.args.newUsername, false)
			if (err != nil) != tt.wantErr {
				t.Errorf("\t%s ManageReplicationRole() error = %v, wantErr %v", failed, err, tt.wantErr)
				return
			}
			// check if user has replication role enabled in AWS and non AWS postgres environments
			err = replicationQueryStmt.QueryRow(tt.args.newUsername).Scan(&exists)
			if err != nil {
				t.Errorf("\t%s ManageReplicationRole error = %v", failed, err)
			}
			if !exists {
				t.Logf("\t%s user %v has rds replication role disabled", succeed, tt.args.newUsername)
			} else {
				t.Errorf("\t%s user %v does not have rds replication role disabled", failed, tt.args.newUsername)
			}
			t.Logf("\t%s RDS ManageReplicationRole() is passed", succeed)

			t.Logf("Disable createRole access")
			err = pc.ManageCreateRole(tt.args.newUsername, false)
			if (err != nil) != tt.wantErr {
				t.Errorf("\t%s ManageCreateRole() error = %v, wantErr %v", failed, err, tt.wantErr)
				return
			}
			// check if user has createrole access enabled in AWS
			err = pc.DB.QueryRow(`SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1 AND rolcreaterole = 't' )`, tt.args.newUsername).Scan(&exists)
			if err != nil {
				t.Errorf("\t%s ManageSuperUserRole error = %v", failed, err)
				return
			}
			if exists {
				t.Errorf("\t%s user %v has create role  access enabled, wantErr %v", failed, tt.args.newUsername, tt.want)
				return
			}
			t.Logf("\t%s Disable createrole passed", succeed)

			t.Logf("Enable createrole access")
			err = pc.ManageCreateRole(tt.args.newUsername, true)
			if (err != nil) != tt.wantErr {
				t.Errorf("\t%s ManageCreateRole() error = %v, wantErr %v", failed, err, tt.wantErr)
				return
			}

			err = pc.DB.QueryRow(`SELECT EXISTS (SELECT 1
				FROM pg_roles 
				WHERE rolname = $1 
				and rolcreaterole = 'f' )`, tt.args.newUsername).Scan(&exists)
			if err != nil {
				t.Errorf("\t%s ManageCreateRole error = %v", failed, err)
				return
			}
			if exists {
				t.Errorf("\t%s user %v has createrole access disabled, wantErr %v", failed, tt.args.newUsername, tt.want)
				return
			}
			t.Logf("\t%s Enable createrole access passed", succeed)

			t.Logf("UpdatePassword() can't be updated, want error")
			wantErr := true
			err = pc.UpdatePassword(tt.args.username, tt.args.newPassword)
			if (err != nil) != wantErr {
				t.Errorf("\t%s UpdatePassword() error = %v, wantErr %v", failed, err, wantErr)
				return
			}
			t.Logf("\t%s UpdatePassword() can't be updated, want error, is passed", succeed)

			t.Logf("UpdatePassword()")

			err = pc.UpdatePassword(tt.args.newUsername, tt.args.newPassword)
			if (err != nil) != tt.wantErr {
				t.Errorf("\t%s UpdatePassword() error = %v, wantErr %v", failed, err, tt.wantErr)
				return
			}
			t.Logf("\t%s UpdatePassword() is passed", succeed)

			// login as user
			db, err := testDB.OpenUser(tt.args.dbName, tt.args.newUsername, tt.args.newPassword)
			if err != nil {
				t.Errorf("could not login with updated password %s", err)
			}

			t.Logf("\t%s OpenUser() is passed", succeed)

			t.Logf("create table")

			_, err = db.Exec("CREATE TABLE IF NOT EXISTS test_table (id SERIAL PRIMARY KEY, name VARCHAR(255) NOT NULL)")
			if err != nil {
				t.Errorf("\t%s create table error = %v", failed, err)
			} else {
				t.Logf("\t%s create table is passed", succeed)
			}
			db.Close()

			// login as user
			db, err = testDB.OpenUserWithURI(tt.args.dbName, tt.args.newUsername, tt.args.newPassword)
			if err != nil {
				t.Errorf("could not login with updated password %s", err)
			}
			db.Close()
			t.Logf("\t%s OpenUserWithURI() is passed", succeed)
			// login as user
			db, err = testDB.OpenUserWithConnectionString(tt.args.dbName, tt.args.newUsername, tt.args.newPassword)
			if err != nil {
				t.Errorf("could not login with updated password %s", err)
			}
			t.Logf("\t%s OpenUserWithURI() is passed", succeed)

			defer db.Close()
			if _, err := db.Exec("CREATE EXTENSION IF NOT EXISTS citext"); err != nil {
				t.Errorf("citext is not created: %s", err)
			}
			t.Logf("create system functions")
			functions := map[string]string{
				"ib_realm":     "eu",
				"ib_env":       "box-3",
				"ib_lifecycle": "dev",
			}
			err = pc.ManageSystemFunctions(tt.args.dbName, functions)
			if (err != nil) != tt.wantErr {
				t.Errorf("\t%s ManageSystemFunctions() error = %v, wantErr %v", failed, err, tt.wantErr)
			} else {
				t.Logf("\t%s create system functions passed", succeed)
			}
			t.Logf("re-create same system functions")
			err = pc.ManageSystemFunctions(tt.args.dbName, functions)
			if (err != nil) != tt.wantErr {
				t.Errorf("\t%s re-create ManageSystemFunctions() error = %v, wantErr %v", failed, err, tt.wantErr)
			} else {
				t.Logf("\t%s create same system functions passed", succeed)
			}
			t.Logf("re-create different system functions")
			functions = map[string]string{
				"ib_realm":     "us",
				"ib_env":       "box-4",
				"ib_lifecycle": "stage",
			}
			err = pc.ManageSystemFunctions(tt.args.dbName, functions)
			if (err != nil) != tt.wantErr {
				t.Errorf("\t%s re-create ManageSystemFunctions() error = %v, wantErr %v", failed, err, tt.wantErr)
			} else {
				t.Logf("\t%s create different system functions passed", succeed)
			}

		})
	}
}

func TestConnectionString(t *testing.T) {
	type args struct {
		host     string
		port     string
		user     string
		password string
		dbName   string
		sslmode  string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			"Test Connection string",
			args{
				host:     "test-host",
				port:     "1234",
				user:     "test_user",
				password: "password",
				dbName:   "test_db",
				sslmode:  "disable",
			},
			"host=test-host port=1234 user=test_user password=password dbname=test_db sslmode=disable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PostgresConnectionString(tt.args.host, tt.args.port, tt.args.user, tt.args.password, tt.args.dbName, tt.args.sslmode); got != tt.want {
				t.Errorf("PostgresConnectionString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPostgresURI(t *testing.T) {
	type args struct {
		host     string
		port     string
		user     string
		password string
		dbname   string
		sslmode  string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "create db URI test",
			args: args{
				host:     "test-host",
				port:     "1234",
				user:     "test_user",
				password: "pa@ss$){[d~&!@#$%^*()_+`-={}|[]:<>?,./",
				dbname:   "test_db",
				sslmode:  "disable",
			},
			want: `postgres://test_user:pa%40ss%24%29%7B%5Bd~%26%21%40%23%24%25%5E%2A%28%29_%2B%60-%3D%7B%7D%7C%5B%5D%3A%3C%3E%3F%2C.%2F@test-host:1234/test_db?sslmode=disable`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PostgresURI(fmt.Sprintf("%s:%s", tt.args.host, tt.args.port), tt.args.user, tt.args.password, tt.args.dbname, tt.args.sslmode); got != tt.want {
				t.Errorf("\n   got: %s\nwanted: %s", got, tt.want)
			}
		})
	}
}
