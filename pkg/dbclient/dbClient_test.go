package dbclient

import (
	"database/sql"
	"fmt"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/go-logr/logr"
	intg "github.com/infobloxopen/atlas-app-toolkit/integration"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

const succeed = "\u2713"
const failed = "\u2717"

var sqlDB *sql.DB

func setupSqlDB(t *testing.T) func(t *testing.T) {
	t.Log("Setting up an instance of PostgreSQL DB with dockertest")

	host := "localhost"
	port, err := intg.GetOpenPortInRange(50000, 60000)
	if err != nil {
		t.Fatalf("Unable to find an opened port for DB: %v", err)
	}
	t.Log("Got available port for DB: ", port)
	user := "test"
	pass := "test"

	// Create a new pool for docker containers
	pool, err := dockertest.NewPool("")
	if err != nil {
		t.Fatalf("Could not connect to docker: %s", err)
	}

	resource, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "postgres",
		Tag:        "10",
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
		dbConnStr := fmt.Sprintf("host=%s port=%d user=%s dbname=postgres password=%s sslmode=disable",
			host, port, user, pass)
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

	return func(t *testing.T) {
		t.Log("Tearing down dockertest resource of PostgreSQL DB")
		if err := pool.Purge(resource); err != nil {
			t.Errorf("Could not purge resource: %s", err)
		}
	}
}

var logger = zap.New(zap.UseDevMode(true))

func TestPostgresClientOperations(t *testing.T) {
	teardownSqlDB := setupSqlDB(t)
	defer teardownSqlDB(t)

	type mockClient struct {
		dbType string
		DB     *sql.DB
		log    logr.Logger
	}
	type args struct {
		dbName       string
		username     string
		userPassword string
		rotationTime time.Duration
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
				log:    logger,
			},
			args{
				"test_db",
				"test_user",
				"test_password",
				time.Duration(60),
			},
			true,
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pc := &PostgresClient{
				dbType: tt.fields.dbType,
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

			t.Logf("CreateUser()")
			got, err = pc.CreateUser(tt.args.dbName, tt.args.username, tt.args.userPassword)
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

			t.Logf("RemoveExpiredUsers() test")
			t.Logf("\n add test users")
			curTime := time.Now().Truncate(time.Minute)
			var prevUser string
			for i := 0; i < 10; i++ {
				duration := time.Duration(60 * i)
				expiredTime := curTime.Add(-duration * time.Minute)
				timeStr := fmt.Sprintf("%d%02d%02d%02d%02d", expiredTime.Year(), expiredTime.Month(), expiredTime.Day(), expiredTime.Hour(), expiredTime.Minute())
				usernamePrefix := fmt.Sprintf("dbctl_%s_", tt.args.username)
				newUsername := usernamePrefix + timeStr
				if i == 1 {
					prevUser = newUsername
				}
				_, err := pc.CreateUser(tt.args.dbName, newUsername, tt.args.userPassword)
				if err != nil {
					t.Errorf("\tcreating user error %v", newUsername)
					return
				}
			}
			t.Logf("\n removing stale test users")
			timeStr := fmt.Sprintf("%d%02d%02d%02d%02d", curTime.Year(), curTime.Month(), curTime.Day(), curTime.Hour(), curTime.Minute())
			usernamePrefix := fmt.Sprintf("dbctl_%s_", tt.args.username)
			newUsername := usernamePrefix + timeStr
			_, err = pc.CreateUser(tt.args.dbName, newUsername, tt.args.userPassword)
			if err != nil {
				t.Errorf("\tcreating user error %v", newUsername)
				return
			}

			if err := pc.RemoveExpiredUsers(tt.args.dbName, newUsername, prevUser, usernamePrefix); err != nil {
				t.Errorf("\tremoving expired users error = %v", err)
				return
			}

			var count int
			err = pc.DB.QueryRow("SELECT count(pg_user.usename) FROM pg_catalog.pg_user where pg_user.usename LIKE '" + usernamePrefix + "%'").Scan(&count)
			if err != nil {
				t.Errorf("\tcan't get user count %v", err)
				return
			}
			countWant := 2
			if count != countWant {
				t.Errorf("\tuser count mismatch want %v got %v", countWant, count)
				return
			}

			expectedNames := []string{
				prevUser,
				newUsername,
			}
			gotNames := make([]string, 0, 0)

			rows, err := pc.DB.Query("SELECT pg_user.usename FROM pg_catalog.pg_user where pg_user.usename LIKE '" + usernamePrefix + "%' ORDER BY usename")
			defer rows.Close()

			for rows.Next() {
				var curUsername string
				err = rows.Scan(&curUsername)
				if err != nil {
					t.Errorf("\tcan't get user names %v", err)
					return
				}
				gotNames = append(gotNames, curUsername)
			}
			if !reflect.DeepEqual(gotNames, expectedNames) {
				t.Logf("\t%s user names mismatch got %v want %v", failed, gotNames, expectedNames)
				return
			}

			t.Logf("\t%s RemoveExpiredUsers test is passed", succeed)
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
				password: `test-pas\sword'`,
				dbName:   "test_db",
				sslmode:  "disable",
			},
			`host='test-host' port='1234' user='test_user' password='test-pas\\sword\'' dbname='test_db' sslmode='disable'`,
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
				password: `pas\s)'d`,
				dbname:   "test_db",
				sslmode:  "disable",
			},
			want: `postgres://test_user:pas%5Cs%29%27d@test-host:1234/test_db?sslmode=disable`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PostgresURI(tt.args.host, tt.args.port, tt.args.user, tt.args.password, tt.args.dbname, tt.args.sslmode); got != tt.want {
				t.Errorf("PostgresURI() = %v, want %v", got, tt.want)
			}
		})
	}
}
