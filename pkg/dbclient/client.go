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
	PostgresType       = "postgres"
	RDSReplicationRole = "rds_replication"
	RDSSuperUserRole   = "rds_superuser"
)

var defaultExtensions = []string{"citext", "uuid-ossp",
	"pgcrypto", "hstore", "pg_stat_statements",
	"plpgsql", "hll"}

var specialExtensionsMap = map[string]func(*client, string, string) error{
	"pg_partman": (*client).pg_partman,
	"pg_cron":    (*client).pg_cron,
}

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
	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s", host,
		port, escapeValue(user), escapeValue(password), escapeValue(dbname), sslmode)
}

// PostgresURI returns a URI for a postgres connection
func PostgresURI(addr, user, password, dbname, sslmode string) string {
	return fmt.Sprintf("postgres://%s:%s@%s/%s?sslmode=%s", url.QueryEscape(user), url.QueryEscape(password), addr, url.QueryEscape(dbname), sslmode)
}

// CreateDataBase implements typo func name incase anybody is using it
func (pc *client) CreateDataBase(name string) (bool, error) {
	pc.log.Error(errors.New("CreateDataBase called, use CreateDatabase"), "old_interface_called")
	return pc.CreateDatabase(name)
}

// unit test override
var getDefaulExtensions = func() []string {
	return defaultExtensions
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

func (pc *client) CreateDefaultExtensions(dbName string) error {
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

	return nil
}

func (pc *client) CreateSpecialExtensions(dbName string, role string) error {
	db, err := pc.getDB(dbName)
	if err != nil {
		pc.log.Error(err, "could not connect to db", "database", dbName)
		return err
	}
	pc.log.Info("connected to " + dbName)
	defer db.Close()
	for functionName := range specialExtensionsMap {
		if err := specialExtensionsMap[functionName](pc, dbName, role); err != nil {
			pc.log.Error(err, "error creating extention %s", functionName)
			return err
		}
	}
	return nil
}

func (pc *client) pg_cron(dbName string, role string) error {
	// create extension pg_cron and grant usage to public
	db, err := pc.getDB(dbName)
	if err != nil {
		pc.log.Error(err, "Failed to connect to database", "database", dbName)
		return err
	}
	defer db.Close()

	pc.log.Info("Connected to database", "database", dbName)

	_, err = db.Exec(fmt.Sprintf(`
		CREATE EXTENSION IF NOT EXISTS pg_cron;
		GRANT USAGE ON SCHEMA cron TO %s;
	`, pq.QuoteIdentifier(role)))
	if err != nil {
		pc.log.Error(err, "Failed to create extension pg_cron and grant usage on schema cron to public", "database", dbName)
		return fmt.Errorf("failed to create extension pg_cron and grant usage on schema cron to public: %w", err)
	}

	pc.log.Info("Created pg_cron extension and granted usage on schema cron", "role", role)

	return nil
}

func (pc *client) pg_partman(dbName string, role string) error {

	db, err := pc.getDB(dbName)
	if err != nil {
		pc.log.Error(err, "could not connect to db", "database", dbName)
		return err
	}
	defer db.Close()
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
		pc.log.Error(err, "could not create extension pg_partman")
		return err
	}
	pc.log.Info("Created pg_partmann extension and granted usage on schema partman", "role", role)

	return nil
}

func (pc *client) ManageSystemFunctions(dbName string, functions map[string]string) error {
	db, err := pc.getDB(dbName)
	if err != nil {
		pc.log.Error(err, "could not connect to db", "database", dbName)
		return err
	}
	pc.log.Info("ManageSystemFunctions - connected to " + dbName)
	defer db.Close()
	//check if schema ib exists
	var exists bool
	err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM information_schema.schemata WHERE schema_name = 'ib')").Scan(&exists)
	if err != nil {
		pc.log.Error(err, "could not query for schema ib")
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
			pc.log.Error(err, "could not create schema ib")
			return err
		}
	}
	var currVal string
	var create bool
	var schema string
	sep := "_" // separator between schema and function name
	for f, v := range functions {
		create = false
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
			if strings.Contains(err.Error(), "does not exist") {
				pc.log.Info("function does not exist - create it", "function", f)
				create = true
			} else {
				return err
			}
		} else {
			if currVal != v {
				pc.log.Info("function value is not correct - update it", "function", f)
				create = true
			}
		}
		if create {
			createFunction := `
				CREATE OR REPLACE FUNCTION %s.%s() 
				RETURNS text AS $$SELECT text '%s'$$ 
				LANGUAGE sql IMMUTABLE PARALLEL SAFE;
			`
			_, err = db.Exec(fmt.Sprintf(createFunction, schema, f, v))
			if err != nil {
				pc.log.Error(err, "could not create function", "database_name",
					dbName, "function", f, "value", v)
				return err
			}
		}
	}
	return nil
}

func (pc *client) SchemaExists(schemaName string) (bool, error) {
	var exists bool
	err := pc.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM information_schema.schemata WHERE schema_name = $1)", schemaName).Scan(&exists)
	if err != nil {
		pc.log.Error(err, "could not query for schema "+schemaName)
		return false, err
	}
	return exists, nil
}

func (pc *client) CreateSchema(schemaName string) (bool, error) {
	createSchema := strings.Replace(`
			CREATE SCHEMA IF NOT EXISTS %schema%;
			REVOKE ALL ON SCHEMA %schema% FROM PUBLIC;
			GRANT USAGE ON SCHEMA %schema% TO PUBLIC;			
			REVOKE ALL ON ALL TABLES IN SCHEMA %schema% FROM PUBLIC ;
			GRANT SELECT ON ALL TABLES IN SCHEMA %schema% TO PUBLIC;
		`, "%schema%", schemaName, -1)
	//create schema
	_, err := pc.DB.Exec(createSchema)
	if err != nil {
		pc.log.Error(err, "could not create schema "+schemaName)
		return false, err
	}
	return true, nil
}

func (pc *client) RoleExists(roleName string) (bool, error) {
	var exists bool

	err := pc.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM pg_catalog.pg_roles where pg_roles.rolname = $1)", roleName).Scan(&exists)
	if err != nil {
		pc.log.Error(err, "could not query for role "+roleName)
		return false, err
	}
	return exists, nil
}

func (pc *client) CreateRole(dbName, rolename, schema string) (bool, error) {
	if schema == "" {
		schema = "public"
	}

	start := time.Now()
	var exists bool
	created := false

	err := pc.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM pg_catalog.pg_roles where pg_roles.rolname = $1)", rolename).Scan(&exists)
	if err != nil {
		pc.log.Error(err, "could not query for role")
		metrics.UsersCreatedErrors.WithLabelValues("read error").Inc()
		return created, err
	}

	if !exists {
		pc.log.Info("creating a ROLE", "role", rolename)
		grantDB := `
			GRANT ALL PRIVILEGES ON DATABASE %s TO %s;
			GRANT ALL ON SCHEMA %s TO %s;
			GRANT ALL ON ALL TABLES IN SCHEMA %s TO %s;
		`
		grantSchemaPrivileges := `
			GRANT ALL ON SCHEMA %s TO %s;
		`
		_, err = pc.DB.Exec(fmt.Sprintf("CREATE ROLE %s WITH NOLOGIN", pq.QuoteIdentifier(rolename)))
		if err != nil {
			pc.log.Error(err, "could not create role "+rolename)
			metrics.UsersCreatedErrors.WithLabelValues("create error").Inc()
			return created, err
		}

		_, err := pc.DB.Exec(
			fmt.Sprintf(grantDB,
				pq.QuoteIdentifier(dbName),
				pq.QuoteIdentifier(rolename),
				pq.QuoteIdentifier(schema),
				pq.QuoteIdentifier(rolename),
				pq.QuoteIdentifier(schema),
				pq.QuoteIdentifier(rolename)))
		if err != nil {
			pc.log.Error(err, "could not set permissions to role "+rolename)
			metrics.UsersCreatedErrors.WithLabelValues("grant error").Inc()
			return created, err
		}

		db, err := pc.getDB(dbName)
		if err != nil {
			pc.log.Error(err, "could not connect to db", "database", dbName)
			return created, err
		}
		defer db.Close()
		//grant schema privileges
		_, err = db.Exec(fmt.Sprintf(grantSchemaPrivileges, pq.QuoteIdentifier(schema), pq.QuoteIdentifier(rolename)))
		if err != nil {
			pc.log.Error(err, "could not set schema privileges to role "+rolename)
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

func (pc *client) CreateAdminRole(dbName, rolename, schema string) (bool, error) {
	created, err := pc.CreateRole(dbName, rolename, schema)

	if err != nil {
		return created, err
	}

	if created {
		grantSchemaPrivileges := `
			GRANT ALL ON SCHEMA %s TO %s;
		`
		db, err := pc.getDB(dbName)
		if err != nil {
			pc.log.Error(err, "could not connect to db", "database", dbName)
			return created, err
		}
		defer db.Close()
		//grant schema privileges
		_, err = db.Exec(fmt.Sprintf(grantSchemaPrivileges, pq.QuoteIdentifier(schema), pq.QuoteIdentifier(rolename)))
		if err != nil {
			pc.log.Error(err, "could not set schema privileges to role "+rolename)
			metrics.UsersCreatedErrors.WithLabelValues("grant error").Inc()
			return created, err
		}
	}

	return created, nil
}

func (pc *client) CreateRegularRole(dbName, rolename, schema string) (bool, error) {
	created, err := pc.CreateRole(dbName, rolename, schema)

	if err != nil {
		return created, err
	}

	if created {
		grantSchemaPrivileges := `
			GRANT USAGE ON SCHEMA %s TO %s;
			GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA %s TO %s;
			ALTER DEFAULT PRIVILEGES IN SCHEMA %s GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO %s;
			ALTER DEFAULT PRIVILEGES IN SCHEMA %s GRANT USAGE ON SEQUENCES TO %s;
			ALTER DEFAULT PRIVILEGES IN SCHEMA %s GRANT USAGE ON SEQUENCES TO %s;
		`
		db, err := pc.getDB(dbName)
		if err != nil {
			pc.log.Error(err, "could not connect to db", "database", dbName)
			return created, err
		}
		defer db.Close()
		//grant schema privileges
		_, err = db.Exec(
			fmt.Sprintf(grantSchemaPrivileges,
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
			pc.log.Error(err, "could not set schema privileges to role "+rolename)
			metrics.UsersCreatedErrors.WithLabelValues("grant error").Inc()
			return created, err
		}
	}

	return created, nil
}

func (pc *client) CreateReadOnlyRole(dbName, rolename, schema string) (bool, error) {
	created, err := pc.CreateRole(dbName, rolename, schema)

	if err != nil {
		return created, err
	}

	if created {
		grantSchemaPrivileges := `
			GRANT USAGE ON SCHEMA %s TO %s;
			GRANT SELECT ON ALL TABLES IN SCHEMA %s TO %s;
			ALTER DEFAULT PRIVILEGES IN SCHEMA %s GRANT SELECT ON TABLES TO %s;
		`
		db, err := pc.getDB(dbName)
		if err != nil {
			pc.log.Error(err, "could not connect to db", "database", dbName)
			return created, err
		}
		defer db.Close()
		//grant schema privileges
		_, err = db.Exec(
			fmt.Sprintf(grantSchemaPrivileges,
				pq.QuoteIdentifier(schema),
				pq.QuoteIdentifier(rolename),
				pq.QuoteIdentifier(schema),
				pq.QuoteIdentifier(rolename),
				pq.QuoteIdentifier(schema),
				pq.QuoteIdentifier(rolename),
			))
		if err != nil {
			pc.log.Error(err, "could not set schema privileges to role "+rolename)
			metrics.UsersCreatedErrors.WithLabelValues("grant error").Inc()
			return created, err
		}
	}

	return created, nil
}

func (pc *client) assignRoleToUser(username, rolename string) error {
	db := pc.DB
	if _, err := db.Exec(fmt.Sprintf("ALTER ROLE %s SET ROLE TO %s", pq.QuoteIdentifier(username), pq.QuoteIdentifier(rolename))); err != nil {
		return err
	}

	return nil
}

func (pc *client) UserExists(userName string) (bool, error) {
	var exists bool

	err := pc.DB.QueryRow("SELECT EXISTS(SELECT pg_user.usename FROM pg_catalog.pg_user where pg_user.usename = $1)", userName).Scan(&exists)
	if err != nil {
		pc.log.Error(err, "could not query for user name")
		return false, err
	}
	return exists, nil
}

func (pc *client) CreateUser(userName, roleName, userPassword string) (bool, error) {
	start := time.Now()
	var exists bool
	db := pc.DB
	created := false

	err := db.QueryRow("SELECT EXISTS(SELECT pg_user.usename FROM pg_catalog.pg_user where pg_user.usename = $1)", userName).Scan(&exists)
	if err != nil {
		pc.log.Error(err, "could not query for user name")
		metrics.UsersCreatedErrors.WithLabelValues("read error").Inc()
		return created, err
	}

	if !exists {
		pc.log.Info("creating a user", "user", userName)

		s := fmt.Sprintf("CREATE ROLE %s with encrypted password %s LOGIN IN ROLE %s", pq.QuoteIdentifier(userName), pq.QuoteLiteral(userPassword), pq.QuoteIdentifier(roleName))
		_, err = pc.DB.Exec(s)
		if err != nil {
			pc.log.Error(err, "could not create user "+userName)
			metrics.UsersCreatedErrors.WithLabelValues("create error").Inc()
			return created, err
		}

		if err := pc.assignRoleToUser(userName, roleName); err != nil {
			pc.log.Error(err, fmt.Sprintf("could not set role %s to user %s", roleName, userName))
			metrics.UsersCreatedErrors.WithLabelValues("grant error").Inc()

			return created, err
		}

		created = true
		pc.log.Info("user has been created", "user", userName)
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

		if err := pc.assignRoleToUser(newUsername, rolename); err != nil {
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

func (pc *client) ManageSuperUserRole(username string, enableSuperUser bool) error {
	var (
		hasSuperUser bool
	)
	roleStmt, err := pc.DB.Prepare(`SELECT EXISTS(
								select 1 from pg_roles 
								WHERE pg_has_role( $1, oid, 'member') 
								AND rolname in ( $2))`)
	if err != nil {
		return err
	}
	err = roleStmt.QueryRow(username, RDSSuperUserRole).Scan(&hasSuperUser)
	if err != nil {
		pc.log.Error(err, "could not query pg_roles", "username", username, "role", RDSSuperUserRole)
		return err
	}
	if hasSuperUser {
		if !enableSuperUser {
			// remove superuser role
			_, err = pc.DB.Exec(fmt.Sprintf("REVOKE %s FROM  %s", RDSSuperUserRole, pq.QuoteIdentifier(username)))
			if err != nil {
				pc.log.Error(err, "could not revoke superuser role")
				return err
			}
		}
	} else {
		if enableSuperUser {
			// add superuser role
			// _, err = pc.DB.Exec(`GRANT $1 TO $2`, pq.QuoteIdentifier(RDSSuperUserRole), pq.QuoteIdentifier(username))
			_, err = pc.DB.Exec(fmt.Sprintf("GRANT %s TO  %s", RDSSuperUserRole, pq.QuoteIdentifier(username)))
			// _, err = pc.DB.Exec(`GRANT rds_superuser TO "new-test-user"`)
			if err != nil {
				pc.log.Error(err, "could not grant superuser role")
				return err
			}
		}
	}

	return nil
}

func (pc *client) ManageCreateRole(username string, enableCreateRole bool) error {
	var (
		exists        bool
		hasCreateRole bool
	)
	//return nil if username does not exists in postgres
	pgRoleStmt, err := pc.DB.Prepare("SELECT EXISTS(select  1 from pg_roles where rolname = $1)")
	if err != nil {
		return err
	}
	err = pgRoleStmt.QueryRow(username).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		//username does not exist, nothing to do
		pc.log.Info("username does not exist, no need to manageCreateRole", "username", username)
		return nil
	}
	createRoleStmt, err := pc.DB.Prepare(`SELECT EXISTS(
								SELECT 1 FROM pg_roles 
								WHERE rolcreaterole = 't'
								AND rolname = $1 )`)
	if err != nil {
		return err
	}

	err = createRoleStmt.QueryRow(username).Scan(&hasCreateRole)
	if err != nil {
		return err
	}
	if hasCreateRole {
		if !enableCreateRole {
			// remove create role
			_, err = pc.DB.Exec(fmt.Sprintf("ALTER ROLE  %s NOCREATEROLE", pq.QuoteIdentifier(username)))
			if err != nil {
				pc.log.Error(err, "could not remove create role")
				return err
			}
		}
	} else {
		if enableCreateRole {
			// add create role
			_, err = pc.DB.Exec(fmt.Sprintf("ALTER ROLE  %s CREATEROLE", pq.QuoteIdentifier(username)))
			if err != nil {
				pc.log.Error(err, "could not add create role")
				return err
			}
		}
	}
	return nil
}

func (pc *client) ManageReplicationRole(username string, enableReplicationRole bool) error {
	var (
		exists             bool
		hasReplicationRole bool
	)
	pgRoleStmt, err := pc.DB.Prepare("SELECT EXISTS(select  1 from pg_roles where rolname = $1)")
	if err != nil {
		return err
	}

	err = pgRoleStmt.QueryRow(username).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		//username does not exist, nothing to do
		return nil
	}
	err = pgRoleStmt.QueryRow(RDSReplicationRole).Scan(&exists)
	if err != nil {
		return err
	}

	if exists {

		// in AWS environment, we need to use a different mechanism to grant/revoke replication rules
		rdsPgRoleStmt, err := pc.DB.Prepare(`SELECT EXISTS(
								select 1 from pg_roles 
								WHERE pg_has_role( $1, oid, 'member') 
								AND rolname in ( $2 ))`)
		if err != nil {
			return err
		}

		err = rdsPgRoleStmt.QueryRow(username, RDSReplicationRole).Scan(&hasReplicationRole)

		if err != nil {
			return err
		}
		if hasReplicationRole {
			if !enableReplicationRole {
				// remove replication role
				_, err = pc.DB.Exec(fmt.Sprintf("REVOKE rds_replication FROM %s", pq.QuoteIdentifier(username)))
				if err != nil {
					return err
				}
			}
		} else {
			if enableReplicationRole {
				// add replication role
				_, err = pc.DB.Exec(fmt.Sprintf("GRANT rds_replication TO %s", pq.QuoteIdentifier(username)))
				if err != nil {
					return err
				}
			}
		}
	} else {
		// not AWS, use normal mechanism
		//check if username has replication role
		replEnabledStmt, err := pc.DB.Prepare("SELECT EXISTS(select 1 from pg_roles where rolreplication = 't' and rolname = $1)")
		if err != nil {
			return err
		}

		err = replEnabledStmt.QueryRow(username).Scan(&hasReplicationRole)
		if err != nil {
			pc.log.Error(err, "could not run query SELECT EXISTS(select 1 from pg_roles where rolreplication = 't' and rolname = $1",
				"$1", username)
			return err
		}
		if hasReplicationRole {
			if !enableReplicationRole {
				// remove replication role
				_, err = pc.DB.Exec(fmt.Sprintf("ALTER ROLE %s NOREPLICATION", pq.QuoteIdentifier(username)))
				if err != nil {
					return err
				}
			}
		} else {
			if enableReplicationRole {
				// add replication role
				_, err = pc.DB.Exec(fmt.Sprintf("ALTER ROLE %s REPLICATION", pq.QuoteIdentifier(username)))
				if err != nil {
					return err
				}
			}
		}
	}
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
