package dbclient

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/lib/pq"

	"github.com/infobloxopen/db-controller/pkg/metrics"
)

const (
	PostgresType = "postgres"
)

var extensions = []string{"citext", "uuid-ossp", "pgcrypto"}

type client struct {
	dbType string
	dbURL  string
	DB     *sql.DB
	log    logr.Logger
}

func (p *client) getDB(dbname string) (*sql.DB, error) {
	u, err := url.Parse(p.dbURL)
	if err != nil {
		return nil, err
	}
	u.Path = "/" + dbname
	return sql.Open("postgres", u.String())
}

type Config struct {
	Log    logr.Logger
	DBType string // type of DB, only postgres
	// connection string to connect to DB ie. postgres://username:password@1.2.3.4:5432/mydb?sslmode=verify-full
	// If username is blank, IRSA/instance principles are used to connect to Database
	DSN string
}

func New(config Config) (Client, error) {

	return newPostgresClient(context.TODO(), config.Log, config.DSN)
}

// DBClientFactory, deprecated don't use
// Unable to pass database name here, so database name is always "postgres"
func DBClientFactory(log logr.Logger, dbType, host, port, user, password, sslmode string) (DBClient, error) {
	log.Error(errors.New("db_client_factory"), "DBClientFactory is deprecated, use New() instead")
	switch dbType {
	case PostgresType:
	}
	return newPostgresClient(context.TODO(), log, PostgresConnectionString(host, port, user, password, "postgres", sslmode))
}

// creates postgres client
func newPostgresClient(ctx context.Context, log logr.Logger, DSN string) (*client, error) {
	db, err := sql.Open(PostgresType, DSN)
	if err != nil {
		return nil, err
	}
	return &client{
		dbType: PostgresType,
		DB:     db,
		log:    log,
		dbURL:  DSN,
	}, nil
}

func PostgresConnectionString(host, port, user, password, dbname, sslmode string) string {
	return fmt.Sprintf("host='%s' port='%s' user='%s' password='%s' dbname='%s' sslmode='%s'", host,
		port, escapeValue(user), escapeValue(password), escapeValue(dbname), sslmode)
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

// CreateDataBase implements typo func name incase anybody is using it
func (pc *client) CreateDataBase(name string) (bool, error) {
	pc.log.Error(errors.New("CreateDataBase called, use CreateDatabase"), "old_interface_called")
	return pc.CreateDatabase(name)
}

func (pc *client) CreateDatabase(dbName string) (bool, error) {
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
		if _, err := db.Exec(fmt.Sprintf("create database %s", pq.QuoteIdentifier(dbName))); err != nil {
			pc.log.Error(err, "could not create database")
			metrics.DBProvisioningErrors.WithLabelValues("create error")
			return created, err
		}
		created = true
		pc.log.Info("database has been created", "DB", dbName)
		metrics.DBCreated.Inc()
	}
	// db is now database specific connection
	db, err = pc.getDB(dbName)
	pc.log.Info("connected to " + dbName)
	if err != nil {
		pc.log.Error(err, "could not connect to db", "database", dbName)
		return created, err
	}
	defer db.Close()
	for _, s := range extensions {
		if _, err = db.Exec(fmt.Sprintf("CREATE EXTENSION IF NOT EXISTS %s", pq.QuoteIdentifier(s))); err != nil {
			pc.log.Error(err, "could not create extension", "database_name", dbName)
			return created, fmt.Errorf("could not create extension %s: %s", s, err)
		}
		pc.log.Info("created extension " + s)
	}
	return created, err
}

func (pc *client) CreateGroup(dbName, rolename string) (bool, error) {
	start := time.Now()
	var exists bool
	db := pc.DB
	created := false

	err := db.QueryRow("SELECT EXISTS(SELECT pg_roles.rolname FROM pg_catalog.pg_roles where pg_roles.rolname = $1)", rolename).Scan(&exists)
	if err != nil {
		pc.log.Error(err, "could not query for role")
		metrics.UsersCreatedErrors.WithLabelValues("read error").Inc()
		return created, err
	}

	if !exists {
		pc.log.Info("creating a ROLE", "role", rolename)
		_, err = pc.DB.Exec(fmt.Sprintf("CREATE ROLE %s WITH NOLOGIN", pq.QuoteIdentifier(rolename)))
		if err != nil {
			pc.log.Error(err, "could not create role "+rolename)
			metrics.UsersCreatedErrors.WithLabelValues("create error").Inc()
			return created, err
		}

		if _, err := db.Exec(fmt.Sprintf("GRANT ALL PRIVILEGES ON DATABASE %s TO %s", pq.QuoteIdentifier(dbName), pq.QuoteIdentifier(rolename))); err != nil {
			pc.log.Error(err, "could not set permissions to role "+rolename)
			metrics.UsersCreatedErrors.WithLabelValues("grant error").Inc()
			return created, err
		}
		created = true
		pc.log.Info("role has been created", "role", rolename)
		metrics.UsersCreated.Inc()
		duration := time.Since(start)
		metrics.UsersCreateTime.Observe(duration.Seconds())
	}

	return created, nil
}

func (pc *client) setGroup(username, rolename string) error {
	db := pc.DB
	if _, err := db.Exec(fmt.Sprintf("ALTER ROLE %s SET ROLE TO %s", pq.QuoteIdentifier(username), pq.QuoteIdentifier(rolename))); err != nil {
		return err
	}

	return nil
}

func (pc *client) CreateUser(username, rolename, userPassword string) (bool, error) {
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

		s := fmt.Sprintf("CREATE ROLE %s with encrypted password %s LOGIN IN ROLE %s", pq.QuoteIdentifier(username), pq.QuoteLiteral(userPassword), pq.QuoteIdentifier(rolename))
		_, err = pc.DB.Exec(s)
		if err != nil {
			pc.log.Error(err, "could not create user "+username)
			metrics.UsersCreatedErrors.WithLabelValues("create error").Inc()
			return created, err
		}

		if err := pc.setGroup(username, rolename); err != nil {
			pc.log.Error(err, fmt.Sprintf("could not set role %s to user %s", rolename, username))
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

func (pc *client) RenameUser(oldUsername string, newUsername string) error {
	var exists bool
	db := pc.DB

	err := db.QueryRow("SELECT EXISTS(SELECT pg_roles.rolname FROM pg_catalog.pg_roles where pg_roles.rolname = $1)", oldUsername).Scan(&exists)

	if err != nil {
		pc.log.Error(err, "could not query for user name")
		return err
	}

	if exists {
		pc.log.Info(fmt.Sprintf("renaming user %v to %v", oldUsername, newUsername))

		_, err = db.Exec(fmt.Sprintf("ALTER USER %s RENAME TO %s", pq.QuoteIdentifier(oldUsername), pq.QuoteIdentifier(newUsername)))
		if err != nil {
			pc.log.Error(err, "could not rename user "+oldUsername)
			return err
		}
	}

	return nil
}

func (pc *client) UpdateUser(oldUsername, newUsername, rolename, password string) error {
	start := time.Now()
	var exists bool
	db := pc.DB

	err := db.QueryRow("SELECT EXISTS(SELECT pg_roles.rolname FROM pg_catalog.pg_roles where pg_roles.rolname = $1)", oldUsername).Scan(&exists)
	if err != nil {
		pc.log.Error(err, "could not query for user name")
		metrics.UsersUpdatedErrors.WithLabelValues("read error").Inc()
		return err
	}

	if exists {
		pc.log.Info(fmt.Sprintf("updating user %s", oldUsername))
		if err := pc.RenameUser(oldUsername, newUsername); err != nil {
			return err
		}

		if err := pc.setGroup(newUsername, rolename); err != nil {
			pc.log.Error(err, fmt.Sprintf("could not set role %s to user %s", rolename, newUsername))
			metrics.UsersCreatedErrors.WithLabelValues("grant error").Inc()

			return err
		}

		if err := pc.UpdatePassword(newUsername, password); err != nil {
			return err
		}

		pc.log.Info("user has been updated", "user", newUsername)
		metrics.UsersUpdated.Inc()
		duration := time.Since(start)
		metrics.UsersUpdateTime.Observe(duration.Seconds())
	}

	return nil
}

func (pc *client) UpdatePassword(username string, userPassword string) error {
	start := time.Now()
	db := pc.DB
	if userPassword == "" {
		err := fmt.Errorf("an empty password")
		pc.log.Error(err, "error occurred")
		metrics.PasswordRotatedErrors.WithLabelValues("empty password").Inc()
		return err
	}

	pc.log.Info("update user password", "user:", username)
	_, err := db.Exec(fmt.Sprintf("ALTER ROLE %s with encrypted password %s", pq.QuoteIdentifier(username), pq.QuoteLiteral(userPassword)))
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

func (pc *client) Close() error {
	if pc.DB != nil {
		return pc.DB.Close()
	}

	return fmt.Errorf("can't close nil DB")
}

func escapeValue(in string) string {

	encoded := make([]rune, 0)
	for _, c := range in {
		switch c {
		case ' ':
			encoded = append(encoded, '\\')
			encoded = append(encoded, ' ')
		case '\\':
			encoded = append(encoded, '\\')
			encoded = append(encoded, '\\')
		case '\'':
			encoded = append(encoded, '\\')
			encoded = append(encoded, '\'')
		default:
			encoded = append(encoded, c)
		}
	}
	return string(encoded)
}
