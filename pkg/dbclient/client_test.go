package dbclient

import (
	"testing"

	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"github.com/infobloxopen/db-controller/internal/dockerdb"
	"go.uber.org/zap/zaptest"
)

const succeed = "\u2713"
const failed = "\u2717"

// NewTestLogger creates a new logr.Logger using a Zap logger that logs to testing.T
func NewTestLogger(t *testing.T) logr.Logger {
	zapTest := zaptest.NewLogger(t)
	return zapr.NewLogger(zapTest)
}

func TestPostgresClientOperations(t *testing.T) {

	testLogger := NewTestLogger(t)

	db, dsn, close := dockerdb.Run(testLogger, dockerdb.Config{
		Username: "test",
		Password: "pa@ss$){[d~&!@#$%^*()_+`-={}|[]:<>?,./",
		Database: "postgres",
	})

	defer close()

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
		args    args
		want    bool
		wantErr bool
	}{
		{
			name: "TEST DB client operations",
			args: args{
				"test_db",
				"test_role",
				"test-user",
				"test_password",
				"new-test-user",
				"password",
			},
			want:    true,
			wantErr: false,
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
				dbType:  "postgres",
				dbURL:   dsn,
				DB:      db,
				adminDB: db,
				log:     testLogger,
			}

			got, err := pc.CreateDatabase(tt.args.dbName)
			if (err != nil) != tt.wantErr {
				t.Fatalf("wantErr %t err: %s", tt.wantErr, err)
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

			t.Logf("CreateRole()")
			got, err = pc.CreateRole(tt.args.dbName, tt.args.role, "")
			if (err != nil) != tt.wantErr {
				t.Errorf("\t%s CreateRole() error = %v, wantErr %v", failed, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("\t%sCreateRole() got = %v, want %v", failed, got, tt.want)
			}

			err = pc.DB.QueryRow("SELECT EXISTS(SELECT pg_roles.rolname FROM pg_catalog.pg_roles where pg_roles.rolname = $1)", tt.args.role).Scan(&exists)
			if err != nil {
				t.Errorf("\t%s CreateRole error = %v", failed, err)
			}

			if exists {
				t.Logf("\t%s role %v has been created", succeed, tt.args.role)
			} else {
				t.Errorf("\t%s can't find user %v", failed, tt.args.role)
			}
			t.Logf("\t%s CreateRole() is passed", succeed)

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

			t.Logf("create table")

			_, err = db.Exec("CREATE TABLE IF NOT EXISTS test_table (id SERIAL PRIMARY KEY, name VARCHAR(255) NOT NULL)")
			if err != nil {
				t.Errorf("\t%s create table error = %v", failed, err)
			} else {
				t.Logf("\t%s create table is passed", succeed)
			}

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
			if got := PostgresURI(tt.args.host, tt.args.port, tt.args.user, tt.args.password, tt.args.dbname, tt.args.sslmode); got != tt.want {
				t.Errorf("\n   got: %s\nwanted: %s", got, tt.want)
			}
		})
	}
}
