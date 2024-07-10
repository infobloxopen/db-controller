package dbclient

import (
	"database/sql"
	"fmt"
	"net/url"
	"strconv"

	intg "github.com/infobloxopen/atlas-app-toolkit/integration"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
)

var sqlDB *sql.DB

type TestDB struct {
	Port     int
	username string
	password string
	resource *dockertest.Resource
	pool     *dockertest.Pool
}

func (t *TestDB) URL() string {
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

func (t *TestDB) Close() error {
	fmt.Print("Tearing down dockertest resource of PostgreSQL DB")
	if t.pool != nil {
		if err := t.pool.Purge(t.resource); err != nil {
			fmt.Println("Could not purge resource")
			return err
		}
	}
	return nil
}

func (t *TestDB) OpenUser(dbname, username, password string) (*sql.DB, error) {
	if dbname == "" {
		dbname = "postgres"
	}
	dbConnStr := fmt.Sprintf("host=localhost port=%d user=%s dbname=%s password=%s sslmode=disable",
		t.Port, username, dbname, password)
	return sql.Open("postgres", dbConnStr)
}

func (t *TestDB) OpenUserWithURI(dbname, username, password string) (*sql.DB, error) {
	if dbname == "" {
		dbname = "postgres"
	}

	dbConnStr := PostgresURI("localhost", strconv.Itoa(t.Port), username, password, dbname, "disable")
	fmt.Println("dbConnStr", dbConnStr)
	return sql.Open("postgres", dbConnStr)
}

func (t *TestDB) OpenUserWithConnectionString(dbname, username, password string) (*sql.DB, error) {
	if dbname == "" {
		dbname = "postgres"
	}
	dbConnStr := PostgresConnectionString("localhost", strconv.Itoa(t.Port), username, password, dbname, "disable")
	fmt.Println("dbConnStr", dbConnStr)
	return sql.Open("postgres", dbConnStr)
}

func SetupSqlDB(user, pass string) (*TestDB, error) {
	fmt.Print("Setting up an instance of PostgreSQL DB with dockertest")

	host := "localhost"
	port, err := intg.GetOpenPortInRange(50000, 60000)
	if err != nil {
		fmt.Print("Unable to find an opened port for DB")
		return nil, err
	}
	fmt.Print("Got available port for DB: ", port)

	// Create a new pool for docker containers
	pool, err := dockertest.NewPool("")
	if err != nil {
		fmt.Print("Could not construct pool")
		return nil, err
	}

	// uses pool to try to connect to Docker
	err = pool.Client.Ping()
	if err != nil {
		fmt.Print("Could not connect to Docker")
		return nil, err
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
			fmt.Print("Database is not ready yet (it is booting up, wait for a few tries)...")
			return err
		}
		// Tests if database is reachable
		return sqlDB.Ping()
	}); err != nil {
		fmt.Print("Could not connect to docker")
		return nil, err
	}
	return &TestDB{
		username: user,
		password: pass,
		Port:     port,
		resource: resource,
		pool:     pool,
	}, nil
}
