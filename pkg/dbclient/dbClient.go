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
	RemoveExpiredUsers(dbName string, username, usernamePrefix, expiredUser string) error

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
	db, err := sql.Open(PostgresType, PostgresConnectionString(host, port, user, password, "postgres", sslmode))
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
		if _, err := db.Exec(fmt.Sprintf("create database %q", dbName)); err != nil {
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

func (pc *PostgresClient) RemoveExpiredUsers(dbName string, username, usernamePrefix, expiredUser string) error {
	log := pc.log
	db := pc.DB

	// clean up expired users
	log.Info("cleaning up expired users")
	rows, err := db.Query("SELECT usename FROM pg_catalog.pg_user WHERE usename LIKE '"+usernamePrefix+"%' AND usename < $1 ORDER BY usename DESC", expiredUser)
	if err != nil {
		log.Error(err, "could not select old usernames")

		return err
	}
	defer rows.Close()
	for rows.Next() {
		var thisUsername string
		err = rows.Scan(&thisUsername)
		log.Info("dropping user " + thisUsername)
		if err != nil {
			log.Error(err, "could not scan usernames")
			return err
		}

		if _, err := db.Exec("REASSIGN OWNED BY " + thisUsername + " TO " + username); err != nil {
			log.Error(err, "could not reassign owned by "+thisUsername)

			return err
		}

		if _, err := db.Exec("REVOKE ALL PRIVILEGES ON DATABASE " + fmt.Sprintf("%q", dbName) + " FROM " + thisUsername); err != nil {
			log.Error(err, "could not revoke privileges "+thisUsername)

			return err
		}
		log.Info("DROP USER " + thisUsername)
		if _, err := db.Exec("DROP USER " + thisUsername); err != nil {
			log.Error(err, "could not drop user: "+thisUsername)

			return err
		}

		log.Info("dropped user: " + thisUsername)
	}

	return nil
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
