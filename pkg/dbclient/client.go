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

var extensions = []string{"citext", "uuid-ossp",
	"pgcrypto", "hstore", "pg_stat_statements",
	"plpgsql", "pg_partman", "hll"}

// TBD
//#pg_cron

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
	// FQDN to connect to database including username and password if not using an IAM enabled user
	DSN string
	// UseIAM bool
	UseIAM bool // attempt to connect with IAM user provided in DSN
}

func New(cfg Config) (Client, error) {

	return newPostgresClient(context.TODO(), cfg)
}

// DBClientFactory, deprecated don't use
// Unable to pass database name here, so database name is always "postgres"
func DBClientFactory(log logr.Logger, dbType, host, port, user, password, sslmode string) (DBClient, error) {
	log.Error(errors.New("db_client_factory"), "DBClientFactory is deprecated, use New() instead")
	switch dbType {
	case PostgresType:
	}
	return newPostgresClient(context.TODO(), Config{
		Log: log,
		DSN: PostgresConnectionString(host, port, user, password, "postgres", sslmode),
	})
}

// creates postgres client
func newPostgresClient(ctx context.Context, cfg Config) (*client, error) {

	authedDSN := cfg.DSN

	if cfg.UseIAM {
		var err error
		authedDSN, err = awsAuthBuilder(ctx, cfg)
		if err != nil {
			return nil, err
		}
	}

	db, err := sql.Open(PostgresType, authedDSN)
	if err != nil {
		return nil, err
	}
	return &client{
		dbType: PostgresType,
		DB:     db,
		log:    cfg.Log,
		dbURL:  cfg.DSN,
	}, nil
}

func PostgresConnectionString(host, port, user, password, dbname, sslmode string) string {
	return fmt.Sprintf("host='%s' port='%s' user='%s' password='%s' dbname='%s' sslmode='%s'", host,
		port, url.QueryEscape(user), url.QueryEscape(password), url.QueryEscape(dbname), sslmode)
}

func PostgresURI(host, port, user, password, dbname, sslmode string) string {
	return fmt.Sprintf("%s://%s:%s@%s:%s/%s?sslmode=%s", "postgres",
		url.QueryEscape(user), url.QueryEscape(password), host, port, url.QueryEscape(dbname), sslmode)
}

// CreateDataBase implements typo func name incase anybody is using it
func (pc *client) CreateDataBase(name string) (bool, error) {
	pc.log.Error(errors.New("CreateDataBase called, use CreateDatabase"), "old_interface_called")
	return pc.CreateDatabase(name)
}

var getDefaulExtensions = func() []string {
	return extensions
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

	return created, err
}

func (pc *client) CreateDefaultExtentions(dbName string) error {
	db, err := pc.getDB(dbName)
	if err != nil {
		pc.log.Error(err, "could not connect to db", "database", dbName)
		return err
	}
	pc.log.Info("connected to " + dbName)
	defer db.Close()
	for _, s := range getDefaulExtensions() {
		if _, err = db.Exec(fmt.Sprintf("CREATE EXTENSION IF NOT EXISTS %s", pq.QuoteIdentifier(s))); err != nil {
			pc.log.Error(err, "could not create extension", "database_name", dbName)
			return fmt.Errorf("could not create extension %s: %s", s, err)
		}
		pc.log.Info("created extension " + s)
	}
	return err
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
		grant := `
			GRANT ALL PRIVILEGES ON DATABASE %s TO %s;
			GRANT ALL ON ALL TABLES IN SCHEMA public TO %s
		`
		_, err = pc.DB.Exec(fmt.Sprintf("CREATE ROLE %s WITH NOLOGIN", pq.QuoteIdentifier(rolename)))
		if err != nil {
			pc.log.Error(err, "could not create role "+rolename)
			metrics.UsersCreatedErrors.WithLabelValues("create error").Inc()
			return created, err
		}

		if _, err := db.Exec(fmt.Sprintf(grant, pq.QuoteIdentifier(dbName),
			pq.QuoteIdentifier(rolename),
			pq.QuoteIdentifier(rolename))); err != nil {
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
