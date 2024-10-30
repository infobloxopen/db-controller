package dbclient

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/lib/pq"

	"github.com/infobloxopen/db-controller/pkg/metrics"
)

const (
	PostgresType       = "postgres"
	RDSReplicationRole = "rds_replication"
	GCPReplicationRole = "alloydbreplica"
	RDSSuperUserRole   = "rds_superuser"
)

var defaultExtensions = []string{"citext", "uuid-ossp",
	"pgcrypto", "hstore", "pg_stat_statements",
	"plpgsql", "hll"}

var specialExtensionsMap = map[string]func(*client, string, string) error{
	"pg_partman": (*client).CreatePgPartmanExtension,
	"pg_cron":    (*client).CreatePgCronExtension,
}

type Client = client

type client struct {
	// cloud is `aws` or `gcp`
	cloud   string
	dbType  string
	dsn     string
	DB      *sql.DB
	adminDB *sql.DB
	log     logr.Logger
}

func (p *client) GetDB() *sql.DB {
	return p.DB
}

func (p *client) PingContext(ctx context.Context) error {
	err := p.DB.PingContext(ctx)
	if err != nil {
		return fmt.Errorf("ping_failed: %s %w", p.SanitizeDSN(), err)
	}
	return nil
}

func (p *client) getDB(dbname string) (*sql.DB, error) {
	if p.DB == nil {
		panic(p.DB)
	}

	return p.DB, nil
}

type Config struct {
	// Cloud is `aws` or `gcp`
	Cloud  string
	Log    logr.Logger
	DBType string // type of DB, only postgres
	// connection string to connect to DB ie. postgres://username:password@1.2.3.4:5432/mydb?sslmode=verify-full
	// FQDN to connect to database including username and password if not using an IAM enabled user
	DSN string
	// UseIAM bool
	UseIAM bool // attempt to connect with IAM user provided in DSN
}

var clientPool = make(map[string]struct{})

var _ Clienter = &Client{}

// New creates a new client for postgres.
func New(cfg Config) (*Client, error) {

	cfg.Log.Info("sql_open_pool", "size", len(clientPool))

	cli, err := newPostgresClient(context.TODO(), cfg)
	if err == nil {
		clientPool[cfg.DSN] = struct{}{}
	}

	return cli, err
}

// newPostgresClient creates a new client for postgres.
func newPostgresClient(ctx context.Context, cfg Config) (*client, error) {

	if cfg.Cloud == "" {
		cfg.Cloud = "aws"
	}

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

	// create a new connection to the database to run admin commands.

	dsnURL, err := url.Parse(authedDSN)
	if err != nil {
		return nil, fmt.Errorf("could not parse DSN: %w", err)
	}
	dsnURL.Path = "/postgres"

	adminDB, err := sql.Open(PostgresType, dsnURL.String())
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	err = adminDB.PingContext(ctx)
	cancel()
	if err != nil {
		adminDB.Close()
		return nil, fmt.Errorf("could not connect to admin database: %w", err)
	}

	return &client{
		cloud:   cfg.Cloud,
		dbType:  PostgresType,
		DB:      db,
		adminDB: adminDB,
		log:     cfg.Log,
		dsn:     cfg.DSN,
	}, nil
}

func PostgresConnectionString(host, port, user, password, dbname, sslmode string) string {
	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s", host,
		port, escapeValue(user), escapeValue(password), escapeValue(dbname), sslmode)
}

// PostgresURI returns a URI for a postgres connection
func PostgresURI(host, port, user, password, dbname, sslmode string) string {
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
		url.QueryEscape(user), url.QueryEscape(password), host, port, url.QueryEscape(dbname), sslmode)
}

// SanitizeDSN returns a redacted version of the DSN.
func (p *client) SanitizeDSN() string {
	u, err := url.Parse(p.dsn)
	if err != nil {
		return ""
	}
	return u.Redacted()
}

// unit test override
var getDefaulExtensions = func() []string {
	return defaultExtensions
}

// CreateDatabase creates a database if it does not exist.
func (p *client) CreateDatabase(dbName string) (bool, error) {
	var exists bool
	log := p.log.WithValues("database", dbName, "dsn", p.SanitizeDSN())

	query := `SELECT EXISTS(SELECT datname FROM pg_catalog.pg_database WHERE datname = $1)`
	err := p.adminDB.QueryRow(query, dbName).Scan(&exists)
	if err != nil {
		// TODO: use error codes provided by the pq driver.
		if strings.Contains(err.Error(), "does not exist") {
			return p.createDatabase(dbName, log)
		}

		log.Error(err, "could not query for database name", "query", fmt.Sprintf(`SELECT EXISTS(SELECT datname FROM pg_catalog.pg_database WHERE datname = '%s')`, dbName))
		metrics.DBProvisioningErrors.WithLabelValues("read error").Inc()
		return false, err
	}

	if exists {
		return false, nil
	}

	return p.createDatabase(dbName, log)
}

func (p *client) createDatabase(dbName string, log logr.Logger) (bool, error) {
	_, err := p.adminDB.Exec(fmt.Sprintf("CREATE DATABASE %s", pq.QuoteIdentifier(dbName)))
	if err != nil {
		log.Error(err, "could not create database")
		metrics.DBProvisioningErrors.WithLabelValues("create error").Inc()
		return false, err
	}

	metrics.DBCreated.Inc()
	return true, nil
}

func (p *client) CheckExtension(extName string) (bool, error) {
	var exists bool
	db := p.DB
	err := db.QueryRow("SELECT EXISTS(SELECT * FROM pg_extension WHERE extname = $1)", extName).Scan(&exists)
	if err != nil {
		p.log.Error(err, "could not query for extension")
		return exists, err
	}
	return exists, nil
}

func (p *client) CreateDefaultExtensions(dbName string) error {
	db, err := p.getDB(dbName)
	if err != nil {
		p.log.Error(err, "could not connect to db", "database", dbName)
		return err
	}
	p.log.Info("connected to " + dbName)

	for _, s := range getDefaulExtensions() {
		ok, err := p.CheckExtension(s)
		if err != nil {
			return fmt.Errorf("psql_extension_query %s: %w", s, err)
		}
		if !ok {
			continue
		}

		if _, err = db.Exec(fmt.Sprintf("CREATE EXTENSION IF NOT EXISTS %s", pq.QuoteIdentifier(s))); err != nil {
			return fmt.Errorf("could not create extension %s: %s", s, err)
		}
		p.log.Info("created extension " + s)
	}

	return nil
}

func (p *client) CreateSpecialExtensions(dbName string, role string) error {
	for functionName := range specialExtensionsMap {
		if err := specialExtensionsMap[functionName](p, dbName, role); err != nil {
			p.log.Error(err, "error creating extention", "extension", functionName)
			return err
		}
	}
	return nil
}

func (p *client) CreatePgCronExtension(dbName string, role string) error {
	// create extension pg_cron and grant usage to public
	db, err := p.getDB(dbName)
	if err != nil {
		p.log.Error(err, "Failed to connect to database", "database", dbName)
		return err
	}

	_, err = db.Exec(fmt.Sprintf(`
		CREATE EXTENSION IF NOT EXISTS pg_cron;
		GRANT USAGE ON SCHEMA cron TO %s;
	`, pq.QuoteIdentifier(role)))
	if err != nil {
		p.log.Error(err, "Failed to create extension pg_cron and grant usage on schema cron to public", "database", dbName)
		return fmt.Errorf("failed to create extension pg_cron and grant usage on schema cron to public: %w", err)
	}

	return nil
}

func (p *client) CreatePgPartmanExtension(dbName string, role string) error {

	db, err := p.getDB(dbName)
	if err != nil {
		p.log.Error(err, "could not connect to db", "database", dbName)
		return err
	}

	createPartman := fmt.Sprintf(`
		CREATE SCHEMA IF NOT EXISTS partman;
		CREATE EXTENSION IF NOT EXISTS pg_partman WITH SCHEMA partman;
		GRANT ALL ON SCHEMA partman TO %s;
		GRANT ALL ON ALL TABLES IN SCHEMA partman TO %s;
		GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA partman TO %s;
		GRANT EXECUTE ON ALL PROCEDURES IN SCHEMA partman TO %s;
	`, pq.QuoteIdentifier(role), pq.QuoteIdentifier(role),
		pq.QuoteIdentifier(role), pq.QuoteIdentifier(role))

	_, err = db.Exec(createPartman)
	if err != nil {
		p.log.Error(err, "could not create extension pg_partman")
		return err
	}
	p.log.Info("Created pg_partmann extension and granted usage on schema partman", "role", role)

	return nil
}

func (p *client) ManageSystemFunctions(dbName string, functions map[string]string) error {
	db, err := p.getDB(dbName)
	if err != nil {
		p.log.Error(err, "could not connect to db", "database", dbName)
		return err
	}

	//check if schema ib exists
	var exists bool
	err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM information_schema.schemata WHERE schema_name = 'ib')").Scan(&exists)
	if err != nil {
		p.log.Error(err, "could not query for schema ib")
		return err
	}
	if !exists {
		createSchema := `
			CREATE SCHEMA IF NOT EXISTS ib;
			REVOKE ALL ON SCHEMA ib FROM PUBLIC;
			GRANT USAGE ON SCHEMA ib TO PUBLIC;
			REVOKE ALL ON ALL TABLES IN SCHEMA ib FROM PUBLIC ;
			GRANT SELECT ON ALL TABLES IN SCHEMA ib TO PUBLIC;
		`
		//create schema ib
		_, err = db.Exec(createSchema)
		if err != nil {
			p.log.Error(err, "could not create schema ib")
			return err
		}
	}
	var currVal string

	var schema string
	sep := "_" // separator between schema and function name
	for f, v := range functions {

		// split ib_lifecycle into schema and function name. eg. ib_life_cycle -> ib.life_cycle
		f_parts := strings.Split(f, sep)
		if len(f_parts) == 1 {
			schema = "public"
		} else {
			schema = f_parts[0]
			f = pq.QuoteIdentifier(strings.Join(f_parts[1:], sep))
		}

		err := db.QueryRow(fmt.Sprintf("SELECT %s.%s()", schema, f)).Scan(&currVal)
		//check if error contains "does not exist"
		if err != nil {
			if !strings.Contains(err.Error(), "does not exist") {
				return err
			}
		}
		if currVal == v {
			continue
		}

		createFunction := `
				CREATE OR REPLACE FUNCTION %s.%s() 
				RETURNS text AS $$SELECT text '%s'$$ 
				LANGUAGE sql IMMUTABLE PARALLEL SAFE;
			`
		_, err = db.Exec(fmt.Sprintf(createFunction, schema, f, v))
		if err != nil {
			p.log.Error(err, "could not create function", "database_name",
				dbName, "function", f, "value", v)
			return err
		}

	}
	return nil
}

func (p *client) SchemaExists(schemaName string) (bool, error) {
	var exists bool
	err := p.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM information_schema.schemata WHERE schema_name = $1)", schemaName).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("schema_exists %s: %w", schemaName, err)
	}
	return exists, nil
}

func (p *client) CreateSchema(schemaName string) (bool, error) {
	createSchema := strings.Replace(`
			CREATE SCHEMA IF NOT EXISTS "%schema%";
			REVOKE ALL ON SCHEMA "%schema%" FROM PUBLIC;
			GRANT USAGE ON SCHEMA "%schema%" TO PUBLIC;			
			REVOKE ALL ON ALL TABLES IN SCHEMA "%schema%" FROM PUBLIC ;
			GRANT SELECT ON ALL TABLES IN SCHEMA "%schema%" TO PUBLIC;
		`, "%schema%", schemaName, -1)
	//create schema
	_, err := p.DB.Exec(createSchema)
	if err != nil {
		p.log.Error(err, "could not create schema "+schemaName)
		return false, err
	}
	return true, nil
}

func (p *client) RoleExists(roleName string) (bool, error) {
	var exists bool

	err := p.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM pg_catalog.pg_roles where pg_roles.rolname = $1)", roleName).Scan(&exists)
	if err != nil {
		p.log.Error(err, "could not query for role "+roleName)
		return false, err
	}
	return exists, nil
}

func (p *client) CreateRole(dbName, rolename, schema string) (bool, error) {
	if schema == "" {
		schema = "public"
	}

	start := time.Now()
	var exists bool
	created := false

	err := p.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM pg_catalog.pg_roles where pg_roles.rolname = $1)", rolename).Scan(&exists)
	if err != nil {
		p.log.Error(err, "could not query for role")
		metrics.UsersCreatedErrors.WithLabelValues("read error").Inc()
		return created, err
	}

	if !exists {
		grantDB := `
			GRANT ALL PRIVILEGES ON DATABASE %s TO %s;
			GRANT ALL ON SCHEMA %s TO %s;
			GRANT ALL ON ALL TABLES IN SCHEMA %s TO %s;
		`
		grantSchemaPrivileges := `
			GRANT ALL ON SCHEMA %s TO %s;
		`
		_, err = p.DB.Exec(fmt.Sprintf("CREATE ROLE %s WITH NOLOGIN", pq.QuoteIdentifier(rolename)))
		if err != nil {
			p.log.Error(err, "could not create role "+rolename)
			metrics.UsersCreatedErrors.WithLabelValues("create error").Inc()
			return created, err
		}

		_, err := p.DB.Exec(
			fmt.Sprintf(grantDB,
				pq.QuoteIdentifier(dbName),
				pq.QuoteIdentifier(rolename),
				pq.QuoteIdentifier(schema),
				pq.QuoteIdentifier(rolename),
				pq.QuoteIdentifier(schema),
				pq.QuoteIdentifier(rolename)))
		if err != nil {
			p.log.Error(err, "could not set permissions to role "+rolename)
			metrics.UsersCreatedErrors.WithLabelValues("grant error").Inc()
			return created, err
		}

		db, err := p.getDB(dbName)
		if err != nil {
			p.log.Error(err, "could not connect to db", "database", dbName)
			return created, err
		}

		//grant schema privileges
		_, err = db.Exec(fmt.Sprintf(grantSchemaPrivileges, pq.QuoteIdentifier(schema), pq.QuoteIdentifier(rolename)))
		if err != nil {
			p.log.Error(err, "could not set schema privileges to role "+rolename)
			metrics.UsersCreatedErrors.WithLabelValues("grant error").Inc()
			return created, err
		}

		created = true
		metrics.UsersCreated.Inc()
		duration := time.Since(start)
		metrics.UsersCreateTime.Observe(duration.Seconds())
	}

	return created, nil
}

func (p *client) CreateAdminRole(dbName, rolename, schema string) (bool, error) {
	created, err := p.CreateRole(dbName, rolename, schema)

	if err != nil {
		return created, err
	}

	if created {
		grantSchemaPrivileges := `
			GRANT ALL ON SCHEMA %s TO %s;
			GRANT CONNECT ON DATABASE %s TO %s;
		`
		db, err := p.getDB(dbName)
		if err != nil {
			p.log.Error(err, "could not connect to db", "database", dbName)
			return created, err
		}

		//grant schema privileges
		_, err = db.Exec(fmt.Sprintf(grantSchemaPrivileges, pq.QuoteIdentifier(schema), pq.QuoteIdentifier(rolename), pq.QuoteIdentifier(dbName), pq.QuoteIdentifier(rolename)))
		if err != nil {
			p.log.Error(err, "could not set schema privileges to role "+rolename)
			metrics.UsersCreatedErrors.WithLabelValues("grant error").Inc()
			return created, err
		}
	}

	return created, nil
}

func (p *client) CreateRegularRole(dbName, rolename, schema string) (bool, error) {
	created, err := p.CreateRole(dbName, rolename, schema)

	if err != nil {
		return created, err
	}

	if created {
		grantSchemaPrivileges := `
		    GRANT CONNECT ON DATABASE %s TO %s;
			GRANT USAGE, CREATE ON SCHEMA %s TO %s;
			GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA %s TO %s;
			ALTER DEFAULT PRIVILEGES IN SCHEMA %s GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO %s;
			GRANT USAGE ON ALL SEQUENCES IN SCHEMA %s TO %s;
			ALTER DEFAULT PRIVILEGES IN SCHEMA %s GRANT USAGE ON SEQUENCES TO %s;
		`
		db, err := p.getDB(dbName)
		if err != nil {
			p.log.Error(err, "could not connect to db", "database", dbName)
			return created, err
		}

		//grant schema privileges
		_, err = db.Exec(
			fmt.Sprintf(grantSchemaPrivileges,
				pq.QuoteIdentifier(dbName),
				pq.QuoteIdentifier(rolename),
				pq.QuoteIdentifier(schema),
				pq.QuoteIdentifier(rolename),
				pq.QuoteIdentifier(schema),
				pq.QuoteIdentifier(rolename),
				pq.QuoteIdentifier(schema),
				pq.QuoteIdentifier(rolename),
				pq.QuoteIdentifier(schema),
				pq.QuoteIdentifier(rolename),
				pq.QuoteIdentifier(schema),
				pq.QuoteIdentifier(rolename),
			))
		if err != nil {
			p.log.Error(err, "could not set schema privileges to role "+rolename)
			metrics.UsersCreatedErrors.WithLabelValues("grant error").Inc()
			return created, err
		}
	}

	return created, nil
}

func (p *client) CreateReadOnlyRole(dbName, rolename, schema string) (bool, error) {
	created, err := p.CreateRole(dbName, rolename, schema)

	if err != nil {
		return created, err
	}

	if created {
		grantSchemaPrivileges := `
		    GRANT CONNECT ON DATABASE %s TO %s;
			GRANT USAGE ON SCHEMA %s TO %s;
			GRANT SELECT ON ALL TABLES IN SCHEMA %s TO %s;
			ALTER DEFAULT PRIVILEGES IN SCHEMA %s GRANT SELECT ON TABLES TO %s;
		`
		db, err := p.getDB(dbName)
		if err != nil {
			p.log.Error(err, "could not connect to db", "database", dbName)
			return created, err
		}

		//grant schema privileges
		_, err = db.Exec(
			fmt.Sprintf(grantSchemaPrivileges,
				pq.QuoteIdentifier(dbName),
				pq.QuoteIdentifier(rolename),
				pq.QuoteIdentifier(schema),
				pq.QuoteIdentifier(rolename),
				pq.QuoteIdentifier(schema),
				pq.QuoteIdentifier(rolename),
				pq.QuoteIdentifier(schema),
				pq.QuoteIdentifier(rolename),
			))
		if err != nil {
			p.log.Error(err, "could not set schema privileges to role "+rolename)
			metrics.UsersCreatedErrors.WithLabelValues("grant error").Inc()
			return created, err
		}
	}

	return created, nil
}

func (p *client) AssignRoleToUser(username, rolename string) error {
	db := p.DB
	if _, err := db.Exec(fmt.Sprintf("ALTER ROLE %s SET ROLE TO %s;GRANT %s TO %s;", pq.QuoteIdentifier(username), pq.QuoteIdentifier(rolename), pq.QuoteIdentifier(rolename), pq.QuoteIdentifier(username))); err != nil {
		return err
	}

	return nil
}

func (p *client) UserExists(userName string) (bool, error) {
	var exists bool

	err := p.DB.QueryRow("SELECT EXISTS(SELECT pg_user.usename FROM pg_catalog.pg_user where pg_user.usename = $1)", userName).Scan(&exists)
	if err != nil {
		p.log.Error(err, "could not query for user name")
		return false, err
	}
	return exists, nil
}

func (p *client) DeleteUser(dbName, username, reassignToUser string) error {
	if username == "" {
		err := fmt.Errorf("an empty username")
		p.log.Error(err, "error occurred")
		metrics.UsersDeleted.Inc()
		return err
	}

	var exists bool

	err := p.DB.QueryRow("SELECT EXISTS(SELECT pg_user.usename FROM pg_catalog.pg_user where pg_user.usename = $1)", username).Scan(&exists)
	if err != nil {
		p.log.Error(err, "could not query for user name")
		return err
	}

	if !exists {
		p.log.Info("user [" + username + "] does not exist.")
		return nil
	}

	p.log.Info("delete user", "user:", username)
	_, err = p.DB.Exec(fmt.Sprintf(
		"GRANT %s TO %s;REASSIGN OWNED BY %s TO %s;REVOKE ALL ON DATABASE %s FROM %s;DROP ROLE IF EXISTS %s;",
		pq.QuoteIdentifier(username),
		pq.QuoteIdentifier(reassignToUser),
		pq.QuoteIdentifier(username),
		pq.QuoteIdentifier(reassignToUser),
		pq.QuoteIdentifier(dbName),
		pq.QuoteIdentifier(username),
		pq.QuoteIdentifier(username)))
	if err != nil {
		p.log.Error(err, "could not delete user: "+username)
		metrics.UsersDeletedErrors.WithLabelValues("drop user error").Inc()
	}

	metrics.UsersDeleted.Inc()

	return nil
}

func (p *client) CreateUser(ctx context.Context, userName, roleName, userPassword string) (bool, error) {
	start := time.Now()
	var exists bool
	created := false

	err := p.DB.QueryRowContext(ctx, "SELECT EXISTS(SELECT pg_user.usename FROM pg_catalog.pg_user where pg_user.usename = $1)", userName).Scan(&exists)
	if err != nil {
		p.log.Error(err, "could not query for user name")
		metrics.UsersCreatedErrors.WithLabelValues("read error").Inc()
		return created, err
	}

	if !exists {
		p.log.V(1).Info("creating a user", "user", userName)
		var sql string
		if roleName != "" {
			sql = fmt.Sprintf("CREATE ROLE %s with encrypted password %s LOGIN IN ROLE %s", pq.QuoteIdentifier(userName), pq.QuoteLiteral(userPassword), pq.QuoteIdentifier(roleName))
		} else {
			sql = fmt.Sprintf("CREATE USER %s with encrypted password %s", pq.QuoteIdentifier(userName), pq.QuoteLiteral(userPassword))
		}
		_, err = p.DB.Exec(sql)
		if err != nil {
			p.log.Error(err, "could not create user "+userName)
			metrics.UsersCreatedErrors.WithLabelValues("create error").Inc()
			return created, err
		}
		if roleName != "" {
			if err := p.AssignRoleToUser(userName, roleName); err != nil {
				p.log.Error(err, fmt.Sprintf("could not set role %s to user %s", roleName, userName))
				metrics.UsersCreatedErrors.WithLabelValues("grant error").Inc()

				return created, err
			}
		}

		created = true
		p.log.Info("user has been created", "user", userName)
		metrics.UsersCreated.Inc()
		duration := time.Since(start)
		metrics.UsersCreateTime.Observe(duration.Seconds())
	}

	return created, nil
}

func (p *client) RenameUser(oldUsername string, newUsername string) error {
	var exists bool
	err := p.DB.QueryRow("SELECT EXISTS(SELECT pg_roles.rolname FROM pg_catalog.pg_roles where pg_roles.rolname = $1)", oldUsername).Scan(&exists)

	if err != nil {
		p.log.Error(err, "could not query for user name")
		return err
	}

	if exists {
		p.log.Info(fmt.Sprintf("renaming user %v to %v", oldUsername, newUsername))

		_, err = p.DB.Exec(fmt.Sprintf("ALTER USER %s RENAME TO %s", pq.QuoteIdentifier(oldUsername), pq.QuoteIdentifier(newUsername)))
		if err != nil {
			p.log.Error(err, "could not rename user "+oldUsername)
			return err
		}
	}

	return nil
}

func (p *client) UpdateUser(oldUsername, newUsername, rolename, password string) error {
	start := time.Now()
	var exists bool

	err := p.DB.QueryRow("SELECT EXISTS(SELECT pg_roles.rolname FROM pg_catalog.pg_roles where pg_roles.rolname = $1)", oldUsername).Scan(&exists)
	if err != nil {
		p.log.Error(err, "could not query for user name")
		metrics.UsersUpdatedErrors.WithLabelValues("read error").Inc()
		return err
	}

	if exists {
		p.log.Info(fmt.Sprintf("updating user %s", oldUsername))
		if err := p.RenameUser(oldUsername, newUsername); err != nil {
			return err
		}

		if err := p.AssignRoleToUser(newUsername, rolename); err != nil {
			p.log.Error(err, fmt.Sprintf("could not set role %s to user %s", rolename, newUsername))
			metrics.UsersCreatedErrors.WithLabelValues("grant error").Inc()

			return err
		}

		if err := p.UpdatePassword(newUsername, password); err != nil {
			return err
		}

		p.log.Info("user has been updated", "user", newUsername)
		metrics.UsersUpdated.Inc()
		duration := time.Since(start)
		metrics.UsersUpdateTime.Observe(duration.Seconds())
	}

	return nil
}

func (p *client) UpdatePassword(username string, userPassword string) error {
	start := time.Now()
	if userPassword == "" {
		err := fmt.Errorf("an empty password")
		p.log.Error(err, "error_updating_password")
		metrics.PasswordRotatedErrors.WithLabelValues("empty password").Inc()
		return err
	}

	p.log.Info("update user password", "user:", username, "password", userPassword)
	_, err := p.DB.Exec(fmt.Sprintf("ALTER ROLE %s with encrypted password %s", pq.QuoteIdentifier(username), pq.QuoteLiteral(userPassword)))
	if err != nil {
		if !strings.Contains(err.Error(), "already exists") {
			p.log.Error(err, "could not alter user "+username)
			metrics.PasswordRotatedErrors.WithLabelValues("alter error").Inc()
			return err
		}
	}
	metrics.PasswordRotated.Inc()
	duration := time.Since(start)
	metrics.PasswordRotateTime.Observe(duration.Seconds())

	return nil
}

// ManageCreateRole manages the create role permission for a user when not
// using cloud database roles
// If running in the cloud, this method does nothing.
func (p *client) ManageCreateRole(role string, enableCreateRole bool) error {
	var (
		exists        bool
		hasCreateRole bool
	)
	//return nil if username does not exists in postgres
	pgRoleStmt, err := p.DB.Prepare("SELECT EXISTS(select  1 from pg_roles where rolname = $1)")
	if err != nil {
		return err
	}
	err = pgRoleStmt.QueryRow(role).Scan(&exists)
	if err != nil {
		return err
	}
	// If in the cloud, this search will fail and return early
	if !exists {
		//username does not exist, nothing to do
		p.log.Info("role does not exist, no need to manageCreateRole", "role", role)
		return nil
	}
	createRoleStmt, err := p.DB.Prepare(`SELECT EXISTS(
								SELECT 1 FROM pg_roles
								WHERE rolcreaterole = 't'
								AND rolname = $1 )`)
	if err != nil {
		return err
	}

	err = createRoleStmt.QueryRow(role).Scan(&hasCreateRole)
	if err != nil {
		return err
	}
	if hasCreateRole {
		if !enableCreateRole {
			// remove create role
			_, err = p.DB.Exec(fmt.Sprintf("ALTER ROLE %s NOCREATEROLE", pq.QuoteIdentifier(role)))
			if err != nil {
				p.log.Error(err, "could not remove create role")
				return err
			}
		}
	} else {
		if enableCreateRole {
			// add create role
			_, err = p.DB.Exec(fmt.Sprintf("ALTER ROLE  %s CREATEROLE", pq.QuoteIdentifier(role)))
			if err != nil {
				p.log.Error(err, "could not add create role")
				return err
			}
		}
	}
	return nil
}

func (p *client) ManageSuperUserRole(username string, enableSuperUser bool) error {
	var (
		err error
	)

	if enableSuperUser {
		p.log.Info("enabling superuser to [" + username + "] user")
		_, err = p.DB.Exec(fmt.Sprintf("GRANT %s TO  %s", RDSSuperUserRole, pq.QuoteIdentifier(username)))
		return err
	}

	_, err = p.DB.Exec(fmt.Sprintf("REVOKE %s FROM  %s", RDSSuperUserRole, pq.QuoteIdentifier(username)))
	return err
}

func (p *client) ManageReplicationRole(username string, enableReplicationRole bool) error {
	var (
		exists bool
	)
	// FIXME: Check if connection can grant replication

	// Check if our rotating users exist
	pgRoleStmt, err := p.DB.Prepare("SELECT EXISTS(select 1 from pg_roles where rolname = $1)")
	if err != nil {
		return err
	}

	err = pgRoleStmt.QueryRow(username).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		// FIXME: this should probably throw an error since manage replication failed
		//username does not exist, nothing to do
		return nil
	}

	var isCloud bool
	role := RDSReplicationRole
	if p.cloud == "gcp" {
		role = GCPReplicationRole
	}
	err = pgRoleStmt.QueryRow(role).Scan(&isCloud)
	if err != nil {
		return err
	}

	if isCloud {
		if enableReplicationRole {
			// add replication role
			_, err := p.DB.Exec(fmt.Sprintf("GRANT %s TO %s", role, pq.QuoteIdentifier(username)))
			return err
		}
		// remove replication role
		_, err := p.DB.Exec(fmt.Sprintf("REVOKE %s FROM %s", role, pq.QuoteIdentifier(username)))
		return err
	}

	// not AWS, use normal mechanism
	if enableReplicationRole {
		// add replication role
		_, err = p.DB.Exec(fmt.Sprintf("ALTER ROLE %s REPLICATION", pq.QuoteIdentifier(username)))
		return err
	}
	// remove replication role
	_, err = p.DB.Exec(fmt.Sprintf("ALTER ROLE %s NOREPLICATION", pq.QuoteIdentifier(username)))
	return err
}

func (p *client) GetUserRoles(ctx context.Context, username string) ([]string, error) {

	rows, err := p.DB.QueryContext(ctx, `SELECT rolname as schema FROM pg_roles WHERE
   pg_has_role( $1, oid, 'member') and rolname != $1;`, username)

	if err != nil {
		p.log.Error(err, "could not query for roles from user  "+username)
		return nil, err
	}

	defer rows.Close()

	var schemas []string = make([]string, 0)
	for rows.Next() {
		var schema string
		if err = rows.Scan(&schema); err != nil {
			return nil, err
		}
		schemas = append(schemas, schema)
	}

	return schemas, nil
}

func (p *client) RevokeAccessToRole(username, rolename string) error {

	_, err := p.DB.Exec(fmt.Sprintf("REVOKE %s FROM  %s", pq.QuoteIdentifier(rolename), pq.QuoteIdentifier(username)))
	if err != nil {
		p.log.Error(err, "could not revoke ["+rolename+"] role from ["+username+"]")
		return err
	}

	return nil
}

func (p *client) Close() error {
	delete(clientPool, p.dsn)
	return p.DB.Close()
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
		case '`':
			encoded = append(encoded, '\\')
			encoded = append(encoded, '`')
		case '$':
			encoded = append(encoded, '\\')
			encoded = append(encoded, '$')
		case '!':
			encoded = append(encoded, '\\')
			encoded = append(encoded, '!')
		default:
			encoded = append(encoded, c)
		}
	}
	return string(encoded)
}
