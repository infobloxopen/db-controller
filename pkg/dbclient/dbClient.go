package dbclient

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	_ "github.com/lib/pq"
)

const (
	postgresType = "postgres"
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
	case postgresType:
		return NewPostgresClient(log, dbType, host, port, user, password, sslmode)
	default:
		return NewPostgresClient(log, dbType, host, port, user, password, sslmode)
	}
}

// creates postgres client
func NewPostgresClient(log logr.Logger, dbType, host, port, user, password, sslmode string) (*PostgresClient, error) {
	db, err := sql.Open(postgresType, ConnectionString(host, port, user, password, sslmode))
	if err != nil {
		return nil, err
	}
	return &PostgresClient{
		dbType: dbType,
		DB:     db,
		log:    log,
	}, nil
}

func ConnectionString(host, port, user, password, sslmode string) string {
	return fmt.Sprintf("host=%s port=%s user=%s password=%s sslmode=%s", host, port, user, password, sslmode)
}

func (pc *PostgresClient) CreateUser(dbName string, username, userPassword string) (bool, error) {
	var exists bool
	db := pc.DB
	created := false

	err := db.QueryRow("SELECT EXISTS(SELECT pg_user.usename FROM pg_catalog.pg_user where pg_user.usename = $1)", username).Scan(&exists)
	if err != nil {
		pc.log.Error(err, "could not query for user name")
		return created, err
	}

	if !exists {
		pc.log.Info("creating a user", "user", username)
		_, err = pc.DB.Exec("CREATE USER" + fmt.Sprintf("%q", username) + " with encrypted password '" + userPassword + "'")

		if err != nil {
			if !strings.Contains(err.Error(), "already exists") {
				pc.log.Error(err, "could not create user "+username)
				return created, err
			}
		}

		if _, err := db.Exec("GRANT ALL PRIVILEGES ON DATABASE " + fmt.Sprintf("%q", dbName) + " TO " + fmt.Sprintf("%q", username)); err != nil {
			pc.log.Error(err, "could not set permissions to user "+username)
			return created, err
		}
		created = true
		pc.log.Info("user has been created", "user", username)
	}

	return created, nil
}

func (pc *PostgresClient) UpdateUser(oldUsername string, newUsername string) error {
	var exists bool
	db := pc.DB

	err := db.QueryRow("SELECT EXISTS(SELECT pg_user.usename FROM pg_catalog.pg_user where pg_user.usename = $1)", oldUsername).Scan(&exists)

	if err != nil {
		pc.log.Error(err, "could not query for user name")
		return err
	}

	if exists {
		pc.log.Info(fmt.Sprintf("renaming user %s to %s", oldUsername, newUsername))

		_, err = db.Exec("ALTER USER" + fmt.Sprintf("%q", oldUsername) + " RENAME TO  " + fmt.Sprintf("%q", newUsername))
		if err != nil {
			pc.log.Error(err, "could not rename user "+oldUsername)
			return err
		}

		pc.log.Info("user has been updated", "user", newUsername)
	}

	return nil
}

func (pc *PostgresClient) UpdatePassword(username string, userPassword string) error {
	db := pc.DB
	if userPassword == "" {
		err := fmt.Errorf("an empty password")
		pc.log.Error(err, "error occurred")
		return err
	}

	pc.log.Info("update user password", "user:", username)
	_, err := db.Exec("ALTER ROLE" + fmt.Sprintf("%q", username) + " with encrypted password '" + userPassword + "'")
	if err != nil {
		if !strings.Contains(err.Error(), "already exists") {
			pc.log.Error(err, "could not alter user "+username)
			return err
		}
	}

	return nil
}

func (pc *PostgresClient) CreateDataBase(dbName string) (bool, error) {
	var exists bool
	created := false
	db := pc.DB
	err := db.QueryRow("SELECT EXISTS(SELECT datname FROM pg_catalog.pg_database WHERE datname = $1)", dbName).Scan(&exists)

	if err != nil {
		pc.log.Error(err, "could not query for database name")
		return created, err
	}
	if !exists {
		pc.log.Info("creating DB:", "database name", dbName)
		// create the database
		if _, err := db.Exec("create database " + fmt.Sprintf("%q", dbName)); err != nil {
			pc.log.Error(err, "could not create database")
			return created, err
		}
		created = true
		pc.log.Info("database has been created", "DB", dbName)
	}

	return created, nil
}

func (pc *PostgresClient) Close() error {
	if pc.DB != nil {
		return pc.DB.Close()
	}

	return fmt.Errorf("can't close nil DB")
}
