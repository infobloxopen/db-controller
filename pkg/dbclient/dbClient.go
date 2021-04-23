package dbclient

import (
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/go-logr/logr"
	_ "github.com/lib/pq"

	"github.com/infobloxopen/db-controller/pkg/metrics"
)

const (
	PostgresType = "postgres"
)

type DBClient interface {
	CreateDataBase(dbName string) (bool, error)
	CreateUser(dbName string, username, userPassword string) (bool, error)
	UpdateUser(oldUsername string, newUsername string) error
	UpdatePassword(username string, userPassword string) error
	DBCloser
}

type DBCloser interface {
	Close() error
}

type PostgresClient struct {
	dbType string
	DB     *sql.DB
	log    logr.Logger
}

func DBClientFactory(log logr.Logger, dbType, host, port, user, password, sslmode string) (DBClient, error) {
	switch dbType {
	case PostgresType:
		return NewPostgresClient(log, dbType, host, port, user, password, sslmode)
	default:
		return NewPostgresClient(log, dbType, host, port, user, password, sslmode)
	}
}

// creates postgres client
func NewPostgresClient(log logr.Logger, dbType, host, port, user, password, sslmode string) (*PostgresClient, error) {
	db, err := sql.Open(PostgresType, PostgresConnectionString(host, port, user, password, "", sslmode))
	if err != nil {
		return nil, err
	}
	return &PostgresClient{
		dbType: dbType,
		DB:     db,
		log:    log,
	}, nil
}

func PostgresConnectionString(host, port, user, password, dbname, sslmode string) string {
	return fmt.Sprintf("host='%s' port='%s' user='%s' password='%s' dbname='%s' sslmode='%s'", host,
		port, user, escapeValue(password), dbname, sslmode)
}

func PostgresURI(host, port, user, password, dbname, sslmode string) string {
	connURL := &url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(user, password),
		Host:     fmt.Sprintf("%s:%s", host, port),
		Path:     fmt.Sprintf("/%s", dbname),
		RawQuery: fmt.Sprintf("sslmode=%s", sslmode),
	}

	return connURL.String()
}

func (pc *PostgresClient) CreateUser(dbName string, username, userPassword string) (bool, error) {
	start := time.Now()

	var exists bool
	db := pc.DB
	created := false

	err := db.QueryRow("SELECT EXISTS(SELECT pg_user.usename FROM pg_catalog.pg_user where pg_user.usename = $1)", username).Scan(&exists)
	if err != nil {
		pc.log.Error(err, "could not query for user name")
		metrics.UsersCreatedErrors.WithLabelValues("read error").Inc()
		return created, err
	}

	if !exists {
		pc.log.Info("creating a user", "user", username)
		_, err = pc.DB.Exec("CREATE USER" + fmt.Sprintf("%q", username) + " with encrypted password '" + userPassword + "'")

		if err != nil {
			if !strings.Contains(err.Error(), "already exists") {
				pc.log.Error(err, "could not create user "+username)
				metrics.UsersCreatedErrors.WithLabelValues("create error").Inc()
				return created, err
			}
		}

		if _, err := db.Exec("GRANT ALL PRIVILEGES ON DATABASE " + fmt.Sprintf("%q", dbName) + " TO " + fmt.Sprintf("%q", username)); err != nil {
			pc.log.Error(err, "could not set permissions to user "+username)
			metrics.UsersCreatedErrors.WithLabelValues("grant error").Inc()
			return created, err
		}
		created = true
		pc.log.Info("user has been created", "user", username)
		metrics.UsersCreated.Inc()
		duration := time.Since(start)
		metrics.UsersCreateTime.Observe(duration.Seconds())
	}

	return created, nil
}

func (pc *PostgresClient) UpdateUser(oldUsername string, newUsername string) error {
	start := time.Now()
	var exists bool
	db := pc.DB

	err := db.QueryRow("SELECT EXISTS(SELECT pg_user.usename FROM pg_catalog.pg_user where pg_user.usename = $1)", oldUsername).Scan(&exists)

	if err != nil {
		pc.log.Error(err, "could not query for user name")
		metrics.UsersUpdatedErrors.WithLabelValues("read error").Inc()
		return err
	}

	if exists {
		pc.log.Info(fmt.Sprintf("renaming user %s to %s", oldUsername, newUsername))

		_, err = db.Exec("ALTER USER" + fmt.Sprintf("%q", oldUsername) + " RENAME TO  " + fmt.Sprintf("%q", newUsername))
		if err != nil {
			pc.log.Error(err, "could not rename user "+oldUsername)
			metrics.UsersUpdatedErrors.WithLabelValues("alter error").Inc()
			return err
		}

		pc.log.Info("user has been updated", "user", newUsername)
		metrics.UsersUpdated.Inc()
		duration := time.Since(start)
		metrics.UsersUpdateTime.Observe(duration.Seconds())
	}

	return nil
}

func (pc *PostgresClient) UpdatePassword(username string, userPassword string) error {
	start := time.Now()
	db := pc.DB
	if userPassword == "" {
		err := fmt.Errorf("an empty password")
		pc.log.Error(err, "error occurred")
		metrics.PasswordRotatedErrors.WithLabelValues("empty password").Inc()
		return err
	}

	pc.log.Info("update user password", "user:", username)
	_, err := db.Exec("ALTER ROLE" + fmt.Sprintf("%q", username) + " with encrypted password '" + userPassword + "'")
	if err != nil {
		if !strings.Contains(err.Error(), "already exists") {
			pc.log.Error(err, "could not alter user "+username)
			metrics.PasswordRotatedErrors.WithLabelValues("alter error").Inc()
			return err
		}
	}
	metrics.PasswordRotated.Inc()
	duration := time.Since(start)
	metrics.PasswordRotateTime.Observe(duration.Seconds())

	return nil
}

func (pc *PostgresClient) CreateDataBase(dbName string) (bool, error) {
	var exists bool
	created := false
	db := pc.DB
	err := db.QueryRow("SELECT EXISTS(SELECT datname FROM pg_catalog.pg_database WHERE datname = $1)", dbName).Scan(&exists)

	if err != nil {
		pc.log.Error(err, "could not query for database name")
		metrics.DBProvisioningErrors.WithLabelValues("read error")
		return created, err
	}
	if !exists {
		pc.log.Info("creating DB:", "database name", dbName)
		// create the database
		if _, err := db.Exec("create database " + fmt.Sprintf("%q", dbName)); err != nil {
			pc.log.Error(err, "could not create database")
			metrics.DBProvisioningErrors.WithLabelValues("create error")
			return created, err
		}
		created = true
		pc.log.Info("database has been created", "DB", dbName)
		metrics.DBCreated.Inc()
	}

	return created, nil
}

func (pc *PostgresClient) Close() error {
	if pc.DB != nil {
		return pc.DB.Close()
	}

	return fmt.Errorf("can't close nil DB")
}

func escapeValue(in string) string {
	// if we surround values to single quotes we must escape a single quote sign and backslash
	toEscape := []string{`\`, `'`}
	for _, e := range toEscape {
		in = strings.ReplaceAll(in, e, "\\"+e)
	}

	return in
}
