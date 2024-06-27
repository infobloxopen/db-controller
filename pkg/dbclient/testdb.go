package dbclient

import (
	"database/sql"
	"fmt"
	"net/url"
	"strconv"

	intg "github.com/infobloxopen/atlas-app-toolkit/integration"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"

	"testing"
)

var sqlDB *sql.DB

type testDB struct {
	t        *testing.T
	Port     int
	username string
	password string
	resource *dockertest.Resource
	pool     *dockertest.Pool
}

func (t *testDB) URL() string {
	u := url.URL{
		Scheme: "postgres",
		Host:   fmt.Sprintf("localhost:%d", t.Port),
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
		t.Port, username, dbname, password)
	return sql.Open("postgres", dbConnStr)
}

func (t *testDB) OpenUserWithURI(dbname, username, password string) (*sql.DB, error) {
	if dbname == "" {
		dbname = "postgres"
	}

	dbConnStr := PostgresURI("localhost", strconv.Itoa(t.Port), username, password, dbname, "disable")
	t.t.Log("dbConnStr", dbConnStr)
	return sql.Open("postgres", dbConnStr)
}

func (t *testDB) OpenUserWithConnectionString(dbname, username, password string) (*sql.DB, error) {
	if dbname == "" {
		dbname = "postgres"
	}
	dbConnStr := PostgresConnectionString("localhost", strconv.Itoa(t.Port), username, password, dbname, "disable")
	t.t.Log("dbConnStr", dbConnStr)
	return sql.Open("postgres", dbConnStr)
}

func SetupSqlDB(t *testing.T, user, pass string) *testDB {
	t.Log("Setting up an instance of PostgreSQL DB with dockertest")

	host := "localhost"
	port, err := intg.GetOpenPortInRange(50000, 60000)
	if err != nil {
		t.Fatalf("Unable to find an opened port for DB: %v", err)
	}
	t.Log("Got available port for DB: ", port)

	// Create a new pool for docker containers
	pool, err := dockertest.NewPool("")
	if err != nil {
		t.Fatalf("Could not construct pool: %s", err)
	}

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
		dbConnStr := PostgresURI(host, strconv.Itoa(port), user, pass, "postgres", "disable")
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
		Port:     port,
		resource: resource,
		pool:     pool,
	}
}
