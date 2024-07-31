package pgctl

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"

	"github.com/go-logr/logr"
	"github.com/infobloxopen/db-controller/pkg/metrics"
	"github.com/lib/pq"
)

const (
	DefaultPubName = "dbc_temp_pub_for_upgrade"
	DefaultSubName = "dbc_temp_sub_for_upgrade"
)

type State interface {
	Execute() (State, error)
	Id() StateEnum
	String() string
}

type ExportFile struct {
	Path string
	Name string
}
type Config struct {
	Log              logr.Logger
	SourceDBAdminDsn string
	SourceDBUserDsn  string
	TargetDBAdminDsn string
	TargetDBUserDsn  string
	ExportFilePath   string
}

type initial_state struct{ config Config }
type create_publication_state struct{ config Config }
type validate_connection_state struct{ config Config }
type copy_schema_state struct{ config Config }
type create_subscription_state struct{ config Config }
type enable_subscription_state struct{ config Config }
type cut_over_readiness_check_state struct{ config Config }
type reset_target_sequence_state struct{ config Config }
type reroute_target_secret_state struct{ config Config }
type wait_to_disable_source_state struct{ config Config }
type disable_source_access_state struct{ config Config }
type validate_migration_status_state struct{ config Config }
type disable_subscription_state struct{ config Config }
type delete_subscription_state struct{ config Config }
type delete_publication_state struct{ config Config }
type completed_state struct{ config Config }
type retry_state struct{ config Config }

// validate that each class implements State interface
var _ State = &initial_state{}
var _ State = &create_publication_state{}
var _ State = &validate_connection_state{}
var _ State = &copy_schema_state{}
var _ State = &create_subscription_state{}
var _ State = &enable_subscription_state{}
var _ State = &cut_over_readiness_check_state{}
var _ State = &reset_target_sequence_state{}
var _ State = &reroute_target_secret_state{}
var _ State = &disable_subscription_state{}
var _ State = &wait_to_disable_source_state{}
var _ State = &disable_source_access_state{}
var _ State = &validate_migration_status_state{}
var _ State = &delete_subscription_state{}
var _ State = &delete_publication_state{}
var _ State = &completed_state{}
var _ State = &retry_state{}

func GetReplicatorState(name string, c Config) (State, error) {
	log := c.Log.WithValues("state", name)

	stateEnum, err := GetStateEnum(name)
	if err != nil {
		log.Error(err, "error while getting current state")
		return nil, err
	}

	switch stateEnum {
	case S_Initial:
		return &initial_state{config: c}, nil
	case S_ValidateConnection:
		return &validate_connection_state{config: c}, nil
	case S_CreatePublication:
		return &create_publication_state{config: c}, nil
	case S_CopySchema:
		return &copy_schema_state{config: c}, nil
	case S_CreateSubscription:
		return &create_subscription_state{config: c}, nil
	case S_EnableSubscription:
		return &enable_subscription_state{config: c}, nil
	case S_CutOverReadinessCheck:
		return &cut_over_readiness_check_state{config: c}, nil
	case S_ResetTargetSequence:
		return &reset_target_sequence_state{config: c}, nil
	case S_RerouteTargetSecret:
		return &reroute_target_secret_state{config: c}, nil
	case S_WaitToDisableSource:
		return &wait_to_disable_source_state{config: c}, nil
	case S_ValidateMigrationStatus:
		return &validate_migration_status_state{config: c}, nil
	case S_DisableSourceAccess:
		return &disable_source_access_state{config: c}, nil
	case S_DisableSubscription:
		return &disable_subscription_state{config: c}, nil
	case S_DeleteSubscription:
		return &delete_subscription_state{config: c}, nil
	case S_DeletePublication:
		return &delete_publication_state{config: c}, nil
	case S_Completed:
		return &completed_state{config: c}, nil
	}

	err = errors.New("unknown current state")
	log.Error(err, "", "state", stateEnum)
	return nil, err
}

func (s *initial_state) Execute() (State, error) {

	if s.config.SourceDBAdminDsn == "" {
		return nil, fmt.Errorf("source db admin dns is empty")
	}
	if s.config.TargetDBAdminDsn == "" {
		return nil, fmt.Errorf("target db admin dns is empty")
	}
	if s.config.SourceDBUserDsn == "" {
		return nil, fmt.Errorf("source db user dns is empty")
	}
	if s.config.TargetDBUserDsn == "" {
		return nil, fmt.Errorf("target db admin dns is empty")
	}
	return &validate_connection_state{
		config: s.config,
	}, nil
}
func (s *initial_state) Id() StateEnum {
	return S_Initial
}
func (s *initial_state) String() string {
	return S_Initial.String()
}

func (s *validate_connection_state) Execute() (State, error) {

	log := s.config.Log.WithValues("state", s.String())
	log.Info("started")

	var (
		err   error
		valid bool
	)

	sourceDBAdmin, err := getDB(s.config.SourceDBAdminDsn, nil)
	if err != nil {
		log.Error(err, "connection test failed for sourceDBAdmin")
		return nil, err
	}
	defer closeDB(log, sourceDBAdmin)

	sourceDBUser, err := getDB(s.config.SourceDBUserDsn, nil)
	if err != nil {
		log.Error(err, "connection test failed for sourceDBUser")
		return nil, err
	}
	defer closeDB(log, sourceDBUser)
	targetDBAdmin, err := getDB(s.config.TargetDBAdminDsn, nil)
	if err != nil {
		log.Error(err, "connection test failed for targetDBAdmin")
		return nil, err
	}
	defer closeDB(log, targetDBAdmin)
	targetDBUser, err := getDB(s.config.TargetDBUserDsn, nil)
	if err != nil {
		log.Error(err, "connection test failed for targetDBUser")
		return nil, err
	}
	defer targetDBUser.Close()

	if valid, err = isAdminUser(sourceDBAdmin); !valid || err != nil {
		return nil, fmt.Errorf("%w; Source DB Admin user lacks  required permission", err)
	}
	if valid, err = isAdminUser(targetDBAdmin); !valid || err != nil {
		return nil, fmt.Errorf("%w; Target DB Admin user lacks  required permission", err)
	}
	if valid, err = isLogical(sourceDBAdmin); !valid || err != nil {
		log.Error(err, "wal_level check failed")
		return nil, err
	}

	log.Info("completed")
	return &create_publication_state{
		config: s.config,
	}, nil
}
func (s *validate_connection_state) Id() StateEnum {
	return S_ValidateConnection
}
func (s *validate_connection_state) String() string {
	return S_ValidateConnection.String()
}

func (s *create_publication_state) Execute() (State, error) {
	log := s.config.Log.WithValues("state", s.String())
	log.Info("started")

	var (
		err           error
		sourceDBAdmin *sql.DB
	)

	if sourceDBAdmin, err = getDB(s.config.SourceDBAdminDsn, nil); err != nil {
		log.Error(err, "connection test failed for sourceDBAdmin")

		return nil, err
	}
	defer closeDB(log, sourceDBAdmin)

	var exists bool

	q := `
		SELECT EXISTS(
			SELECT pubname 
			FROM pg_catalog.pg_publication 
			WHERE pubname = $1)`

	// dynamically creating the table list to be included in the publication
	// this would avoid the issue related to partition extention tables. These schemas are getting
	// changed significantly between versions and the publication would fail if we include them.
	// Tried using schema name syntax but it is not supported in the older version of postgres
	// there are 13.x used by tide in EU prod
	// After all RDSs are upgraded, we can remove this logic and include schema based publications

	createPub := fmt.Sprintf(`
		DO $$
		DECLARE
			table_list TEXT := '';
		BEGIN
			SELECT INTO table_list string_agg(quote_ident(relname), ', ')
			FROM pg_class c
			JOIN pg_namespace n ON c.relnamespace = n.oid
			WHERE n.nspname = 'public' -- Only public schema
			AND c.relkind = 'r'        -- Only include ordinary tables
			AND c.relpersistence = 'p' -- Only include persistent tables
			AND NOT EXISTS (           -- Exclude foreign tables
				SELECT 1 FROM pg_foreign_table ft WHERE ft.ftrelid = c.oid
			)
			AND NOT EXISTS (           -- Exclude materialized views
				SELECT 1 FROM pg_matviews mv WHERE mv.matviewname = c.relname AND mv.schemaname = n.nspname
			);

			IF table_list != '' THEN
				EXECUTE 'CREATE PUBLICATION %s FOR TABLE ' || table_list;
			ELSE
				EXECUTE 'CREATE PUBLICATION %s';
			END IF;
		END $$`, DefaultPubName, DefaultPubName)

	err = sourceDBAdmin.QueryRow(q, DefaultPubName).Scan(&exists)
	if err != nil {
		log.Error(err, "could not query for publication name")
		return nil, err
	}
	if !exists {
		log.Info("creating publication:", "with name", DefaultPubName)
		if _, err := sourceDBAdmin.Exec(createPub); err != nil {
			log.Error(err, "create publication failed")
			return nil, err
		}
		log.Info("publication created", "name", DefaultPubName)
	} else {
		log.Info("publication already exists", "with name", DefaultPubName)
	}

	return &copy_schema_state{
		config: s.config,
	}, nil
}
func (s *create_publication_state) Id() StateEnum {
	return S_CreatePublication
}
func (s *create_publication_state) String() string {
	return S_CreatePublication.String()
}

// The following var is used only by copy_schema_state. This is provided so that it can be overridden during unit test.
var grantSuperUserAccess = func(DBAdmin *sql.DB, role string) error {
	_, err := DBAdmin.Exec(fmt.Sprintf("GRANT rds_superuser TO %s;", pq.QuoteIdentifier(getParentRole(role))))
	return err
}
var revokeSuperUserAccess = func(DBAdmin *sql.DB, role string) error {
	_, err := DBAdmin.Exec(fmt.Sprintf("REVOKE rds_superuser FROM %s;", pq.QuoteIdentifier(getParentRole(role))))
	return err
}

func (s *copy_schema_state) Execute() (State, error) {
	log := s.config.Log.WithValues("state", s.String())
	log.Info("started")

	//grant rds_superuser to target db user temporarily
	//this is required to copy schema from source to target and take ownership of the objects
	log.Info("grant temp superuser to ", "user", s.config.TargetDBUserDsn)

	var (
		err           error
		targetDBAdmin *sql.DB
	)

	if targetDBAdmin, err = getDB(s.config.TargetDBAdminDsn, nil); err != nil {
		log.Error(err, "connection test failed for targetDBAdmin")

		return nil, err
	}
	defer closeDB(log, targetDBAdmin)

	url, err := url.Parse(s.config.TargetDBUserDsn)
	if err != nil {
		return nil, err
	}
	rolename := url.User.Username()
	log.Info("granting super user acesss role to \"" + rolename + "\"")

	err = grantSuperUserAccess(targetDBAdmin, rolename)
	if err != nil {
		log.Error(err, "could not grant super user acesss role to \""+rolename+"\"")
		metrics.UsersUpdatedErrors.WithLabelValues("grant error").Inc()
		return nil, err
	}

	dump := NewDump(s.config.SourceDBAdminDsn)

	dump.SetupFormat("p")
	dump.SetPath(s.config.ExportFilePath)
	dump.EnableVerbose()
	dump.SetOptions([]string{
		"--schema-only",
		"--no-publication",
		"--no-subscriptions",
		"--no-privileges",
		"--no-owner",
		"--no-role-passwords",
		//"--exclude-schema=ib",
	})

	dumpExec := dump.Exec(ExecOptions{StreamPrint: true})
	log.Info("executed dump with", "full command", dumpExec.FullCommand)

	if dumpExec.Error != nil {
		return nil, dumpExec.Error.Err
	}
	if err = dump.modifyPgDumpInfo(); err != nil {
		log.Error(err, "failed to comment create policy")
		return nil, err
	}

	restore := NewRestore(s.config.TargetDBUserDsn)
	restore.EnableVerbose()
	restore.Path = s.config.ExportFilePath
	restoreExec := restore.Exec(dumpExec.FileName, ExecOptions{StreamPrint: true})

	log.Info("restored with", "full command", restoreExec.FullCommand)

	if restoreExec.Error != nil {
		return nil, restoreExec.Error.Err
	}
	err = revokeSuperUserAccess(targetDBAdmin, rolename)
	if err != nil {
		log.Error(err, "could not revoke super user acesss role from"+rolename)
		metrics.UsersUpdatedErrors.WithLabelValues("revoke error").Inc()
		return nil, err
	}

	log.Info("completed")
	return &create_subscription_state{
		config: s.config,
	}, nil

}
func (s *copy_schema_state) Id() StateEnum {
	return S_CopySchema
}
func (s *copy_schema_state) String() string {
	return S_CopySchema.String()
}

// The following var is used only by createSubscription. This is provided so that it can be overridden during unit test.
var getSourceDbAdminDSNForCreateSubscription = func(c *Config) string {
	return c.SourceDBAdminDsn
}

func (s *create_subscription_state) Execute() (State, error) {
	log := s.config.Log.WithValues("state", s.String())
	log.Info("started")
	var exists bool

	targetDBAdmin, err := getDB(s.config.TargetDBAdminDsn, nil)
	if err != nil {
		log.Error(err, "connection test failed for targetDBAdmin")
		return nil, err
	}
	defer closeDB(log, targetDBAdmin)
	q := `
		SELECT EXISTS(
			SELECT subname 
			FROM pg_catalog.pg_subscription 
			WHERE subname = $1
		)`
	createSub := fmt.Sprintf(`
		CREATE SUBSCRIPTION %s 
		CONNECTION '%s' 
		PUBLICATION %s 
		WITH (enabled=false)`, DefaultSubName,
		getSourceDbAdminDSNForCreateSubscription(&s.config),
		DefaultPubName)

	err = targetDBAdmin.QueryRow(q, DefaultSubName).Scan(&exists)
	if err != nil {
		log.Error(err, "could not query for subscription name", "stmt", createSub)
		return nil, err
	}
	if !exists {
		log.Info("creating subscription:", "with name", DefaultSubName)
		if _, err := targetDBAdmin.Exec(createSub); err != nil {
			log.Error(err, "could not create subscription")
			return nil, err
		}
		log.Info("subscription created", "name", DefaultSubName)
	}
	log.Info("completed")
	return &enable_subscription_state{
		config: s.config,
	}, nil
}
func (s *create_subscription_state) Id() StateEnum {
	return S_CreateSubscription
}
func (s *create_subscription_state) String() string {
	return S_CreateSubscription.String()
}

func (s *enable_subscription_state) Execute() (State, error) {
	log := s.config.Log.WithValues("state", s.String())
	log.Info("started")

	targetDBAdmin, err := getDB(s.config.TargetDBAdminDsn, nil)
	if err != nil {
		log.Error(err, "connection test failed for targetDBAdmin")
		return nil, err
	}
	defer closeDB(log, targetDBAdmin)

	q := `
		SELECT EXISTS(
			SELECT subname 
			FROM pg_catalog.pg_subscription 
			WHERE subname = $1)`

	alterSub := fmt.Sprintf("ALTER SUBSCRIPTION %s ENABLE", DefaultSubName)

	var exists bool

	err = targetDBAdmin.QueryRow(q, DefaultSubName).Scan(&exists)
	if err != nil {
		log.Error(err, "could not query for Subscription name")
		return nil, err
	}
	if exists {
		log.Info("enabling Subscription")
		if _, err := targetDBAdmin.Exec(alterSub); err != nil {
			log.Error(err, "could not enable Subscription")
			return nil, err
		}
	} else {
		return nil, fmt.Errorf("unable to enable subscription. subscription not found - %s", DefaultSubName)
	}

	log.Info("completed")

	return &cut_over_readiness_check_state{
		config: s.config,
	}, nil
}
func (s *enable_subscription_state) Id() StateEnum {
	return S_EnableSubscription
}
func (s *enable_subscription_state) String() string {
	return S_EnableSubscription.String()
}

func (s *cut_over_readiness_check_state) Execute() (State, error) {
	log := s.config.Log.WithValues("state", s.String())
	log.Info("started")
	var exists bool
	var count int

	sourceDBAdmin, err := getDB(s.config.SourceDBAdminDsn, nil)
	if err != nil {
		log.Error(err, "connection test failed for sourceDBAdmin")
		return nil, err
	}
	defer closeDB(log, sourceDBAdmin)

	targetDBAdmin, err := getDB(s.config.TargetDBAdminDsn, nil)
	if err != nil {
		log.Error(err, "connection test failed for targetDBAdmin")
		return nil, err
	}
	defer closeDB(log, targetDBAdmin)

	pubQuery := fmt.Sprintf(`
		SELECT EXISTS
		(
			SELECT 1 
			FROM pg_replication_slots 
			WHERE slot_type = 'logical' 
			AND slot_name like '%s_%%'
			AND temporary = 't'
		)`, DefaultPubName)
	// relExistsQuery := fmt.Sprintf(`
	// 	SELECT EXISTS
	// 	(
	// 		SELECT 1
	// 		FROM pg_subscription s, pg_subscription_rel sr
	// 		WHERE s.oid = sr.srsubid
	// 		AND s.subname = '%s'
	// 	)`, DefaultSubName)

	subQuery := fmt.Sprintf(`
		SELECT count(srrelid) 
		FROM pg_subscription s, pg_subscription_rel sr
		WHERE s.oid = sr.srsubid
		AND sr.srsubstate not in ('r', 's')
		AND s.subname = '%s'`, DefaultSubName)

	err = sourceDBAdmin.QueryRow(pubQuery).Scan(&exists)
	if err != nil {
		log.Error(err, "could not query for pg_replication_slots")
		return nil, err
	}
	if exists {
		log.Info("migration not complete in source - retry check in a few seconds")
		return retry(s.config), nil
	}

	// 15.5 seems to behave differently than previous versions regarding the subscription rel
	// err = targetDBAdmin.QueryRow(relExistsQuery).Scan(&exists)
	// if err != nil {
	// 	log.Error(err, "could not query for subscription rel for records present")
	// 	return nil, err
	// }
	// if !exists {
	// 	log.Info("target yet to start receiving data - retry check in a few seconds")
	// 	return retry(s.config), nil
	// }

	err = targetDBAdmin.QueryRow(subQuery).Scan(&count)
	if err != nil {
		log.Error(err, "could not query for subscription for completion")
		return nil, err
	}
	if count > 0 {
		log.Info("migration not complete in target - retry check in a few seconds")
		return retry(s.config), nil
	}
	log.Info("completed")
	return &reset_target_sequence_state{
		config: s.config,
	}, nil
}
func (s *cut_over_readiness_check_state) Id() StateEnum {
	return S_CutOverReadinessCheck
}
func (s *cut_over_readiness_check_state) String() string {
	return S_CutOverReadinessCheck.String()
}

func (s *reset_target_sequence_state) Execute() (State, error) {
	log := s.config.Log.WithValues("state", s.String())
	log.Info("started")

	type seqCount struct {
		seqName string
		lastVal int64
	}
	var (
		seqName string
		lastVal int64
		seqs    []seqCount
	)

	targetDBUser, err := getDB(s.config.TargetDBUserDsn, nil)
	if err != nil {
		log.Error(err, "connection test failed for targetDBUser")
		return nil, err
	}
	defer closeDB(log, targetDBUser)

	sourceDBUser, err := getDB(s.config.SourceDBUserDsn, nil)
	if err != nil {
		log.Error(err, "connection test failed for sourceDBAdmin")
		return nil, err
	}
	defer closeDB(log, sourceDBUser)

	seqsQ := "SELECT sequence_name FROM information_schema.sequences WHERE sequence_schema = 'public'"
	seqCountQ := "SELECT ROUND(last_value + 10000, -4)  FROM %s"

	rows, err := sourceDBUser.Query(seqsQ)
	if err != nil {
		log.Error(err, "failed getting sequence names", seqsQ)
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		rows.Scan(&seqName)
		err = sourceDBUser.QueryRow(fmt.Sprintf(seqCountQ, seqName)).Scan(&lastVal)
		if err != nil {
			log.Error(err, "Failed to get seq number - "+seqName)
			return nil, err
		}
		seqs = append(seqs, seqCount{seqName: seqName, lastVal: lastVal})
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for _, s := range seqs {
		_, err := targetDBUser.Exec(fmt.Sprintf("SELECT SETVAL('%s', %d)", s.seqName, s.lastVal))
		if err != nil {
			log.Error(err, "Failed to update", "seq number", seqName)
			return nil, err
		}
	}
	log.Info("completed")
	return &reroute_target_secret_state{
		config: s.config,
	}, nil
}
func (s *reset_target_sequence_state) Id() StateEnum {
	return S_ResetTargetSequence
}
func (s *reset_target_sequence_state) String() string {
	return S_ResetTargetSequence.String()
}

func (s *reroute_target_secret_state) Execute() (State, error) {
	//log := s.config.log.WithValues("state", s.String())
	return &wait_to_disable_source_state{
		config: s.config,
	}, nil
}
func (s *reroute_target_secret_state) Id() StateEnum {
	return S_RerouteTargetSecret
}
func (s *reroute_target_secret_state) String() string {
	return S_RerouteTargetSecret.String()
}

func (s *wait_to_disable_source_state) Execute() (State, error) {
	//log := s.config.log.WithValues("state", s.String())
	return &disable_source_access_state{
		config: s.config,
	}, nil
}
func (s *wait_to_disable_source_state) Id() StateEnum {
	return S_WaitToDisableSource
}
func (s *wait_to_disable_source_state) String() string {
	return S_WaitToDisableSource.String()
}

func (s *disable_source_access_state) Execute() (State, error) {
	log := s.config.Log.WithValues("state", s.String())
	log.Info("started")

	sourceDBAdmin, err := getDB(s.config.SourceDBAdminDsn, nil)
	if err != nil {
		log.Error(err, "connection test failed for sourceDBAdmin")
		return nil, err
	}
	defer closeDB(log, sourceDBAdmin)

	u, err := url.Parse(s.config.SourceDBUserDsn)
	if err != nil {
		log.Error(err, "parsing sourcedbuserdsn failed")
		return nil, err
	}

	//if rolename name is sample_user_a, the role inherited is sample_user
	//remove "_x" to get role name
	//the role has all the permission - which needs to be removed now
	rolename := getParentRole(u.User.Username())
	stmt := `
		REVOKE insert, delete, update
		ON ALL TABLES IN SCHEMA PUBLIC FROM %s;`

	_, err = sourceDBAdmin.Exec(fmt.Sprintf(stmt, pq.QuoteIdentifier(rolename)))
	if err != nil {
		log.Error(err, "failed revoking access for source db - "+rolename)
		return nil, err
	}
	log.Info("completed")
	return &validate_migration_status_state{
		config: s.config,
	}, nil
}
func (s *disable_source_access_state) Id() StateEnum {
	return S_DisableSourceAccess
}
func (s *disable_source_access_state) String() string {
	return S_DisableSourceAccess.String()
}

func (s *validate_migration_status_state) Execute() (State, error) {
	log := s.config.Log.WithValues("state", s.String())
	log.Info("started")

	var (
		sourceTableName  string
		sourceTableCount int64
		targetTableCount int64
	)

	targetDBUser, err := getDB(s.config.TargetDBUserDsn, nil)
	if err != nil {
		log.Error(err, "connection test failed for targetDBUser")
		return nil, err
	}
	defer closeDB(log, targetDBUser)

	sourceDBUser, err := getDB(s.config.SourceDBUserDsn, nil)
	if err != nil {
		log.Error(err, "connection test failed for sourceDBAdmin")
		return nil, err
	}
	defer closeDB(log, sourceDBUser)

	allTableCountQ := `
			WITH tbl AS (
				SELECT table_schema, table_name
				FROM information_schema.tables t, pg_catalog.pg_tables pt, pg_class cl
				WHERE t.TABLE_NAME not like 'pg_%'
					AND t.table_type = 'BASE TABLE'
					AND t.table_schema = 'public'
					AND t.table_name = pt.tablename
					AND t.table_name = cl.relname
					AND pt.tableowner != 'rdsadmin'
					AND relpersistence != 'u'
			) 
			SELECT 	table_name, 
					(xpath('/row/c/text()', 
						query_to_xml(format('select count(*) as c from %I.%I', table_schema, TABLE_NAME), FALSE, TRUE, '')
						)
					)[1]::text::int AS table_count 
			FROM tbl 
	`
	tableCountQ := "SELECT count(*) From %s"

	rows, err := sourceDBUser.Query(allTableCountQ)
	if err != nil {
		log.Error(err, "failed getting source table count", tableCountQ)
		return nil, err
	}
	defer rows.Close()

	deuce := true
	for rows.Next() {
		rows.Scan(&sourceTableName, &sourceTableCount)
		err = targetDBUser.QueryRow(fmt.Sprintf(tableCountQ, sourceTableName)).Scan(&targetTableCount)
		if err != nil {
			log.Error(err, "failed to query target table count - "+sourceTableName)
			return nil, err
		}
		if targetTableCount < sourceTableCount {
			deuce = false
			log.Error(fmt.Errorf("warning: table count not matching"),
				"intervention required if this message repeates idenfinetly",
				"tableName", sourceTableName,
				"sourceTableCount", sourceTableCount,
				"targetTableCount", targetTableCount,
			)
		} else {
			log.Info("table count looks ok",
				"tableName", sourceTableName,
				"sourceTableCount", sourceTableCount,
				"targetTableCount", targetTableCount,
			)
		}
	}
	if err = rows.Err(); err != nil {
		log.Error(err, "failed to iterate over source table rows")
		return nil, err
	}
	if deuce {
		log.Info("completed")
		return &disable_subscription_state{
			config: s.config,
		}, nil
	} else {
		log.Info("not complete. retrying")
		return &retry_state{
			config: s.config,
		}, nil
	}
}

func (s *validate_migration_status_state) Id() StateEnum {
	return S_ValidateMigrationStatus
}
func (s *validate_migration_status_state) String() string {
	return S_ValidateMigrationStatus.String()
}

func (s *disable_subscription_state) Execute() (State, error) {
	log := s.config.Log.WithValues("state", s.String())
	log.Info("started")

	var exists bool

	targetDBAdmin, err := getDB(s.config.TargetDBAdminDsn, nil)
	if err != nil {
		log.Error(err, "connection test failed for targetDBAdmin")

		return nil, err
	}
	defer closeDB(log, targetDBAdmin)

	q := "SELECT EXISTS(SELECT subname FROM pg_catalog.pg_subscription WHERE subname = $1)"

	err = targetDBAdmin.QueryRow(q, DefaultSubName).Scan(&exists)
	if err != nil {
		log.Error(err, "could not query for Subscription name")
		return nil, err
	}
	if exists {
		if _, err := targetDBAdmin.Exec(fmt.Sprintf("ALTER SUBSCRIPTION %s DISABLE", DefaultSubName)); err != nil {
			log.Error(err, "could not disable Subscription")
			return nil, err
		}
	} else {
		log.Info("subscription not found to disable - ignoring the request")
		return &delete_subscription_state{
			config: s.config,
		}, nil
	}

	log.Info("completed")

	return &delete_subscription_state{
		config: s.config,
	}, nil
}
func (s *disable_subscription_state) Id() StateEnum {
	return S_DisableSubscription
}
func (s *disable_subscription_state) String() string {
	return S_DisableSubscription.String()
}

func (s *delete_subscription_state) Execute() (State, error) {
	log := s.config.Log.WithValues("state", s.String())
	log.Info("started")

	var exists bool
	targetDBAdmin, err := getDB(s.config.TargetDBAdminDsn, nil)
	if err != nil {
		log.Error(err, "connection test failed for targetDBAdmin")
		return nil, err
	}
	defer closeDB(log, targetDBAdmin)
	q := `  
		SELECT EXISTS(
			SELECT subname 
			FROM pg_catalog.pg_subscription 
			WHERE subname = $1)`
	delete := fmt.Sprintf("DROP SUBSCRIPTION %s", (DefaultSubName))

	err = targetDBAdmin.QueryRow(q, DefaultSubName).Scan(&exists)

	if err != nil {
		log.Error(err, "failed to query subscription by name")
		return nil, err
	}
	if exists {
		log.Info("deleting subscription", "with", delete)
		if _, err := targetDBAdmin.Exec(delete); err != nil {
			log.Error(err, "could not delete subscription")
			return nil, err
		}
		log.Info("Subscription deleted")
	}
	log.Info("completed")
	return &delete_publication_state{
		config: s.config,
	}, nil
}
func (s *delete_subscription_state) Id() StateEnum {
	return S_DeleteSubscription
}
func (s *delete_subscription_state) String() string {
	return S_DeleteSubscription.String()
}

func (s *delete_publication_state) Execute() (State, error) {
	log := s.config.Log.WithValues("state", s.String())
	log.Info("started")

	var exists bool

	sourceDBAdmin, err := getDB(s.config.SourceDBAdminDsn, nil)
	if err != nil {
		log.Error(err, "connection test failed for sourceDBAdmin")
		return nil, err
	}
	defer closeDB(log, sourceDBAdmin)

	q := `
		SELECT EXISTS(
			SELECT pubname 
			FROM pg_catalog.pg_publication 
			WHERE pubname = $1)`

	err = sourceDBAdmin.QueryRow(q, DefaultPubName).Scan(&exists)

	if err != nil {
		log.Error(err, "query for publication name failed")
		return nil, err
	}
	if exists {
		log.Info("deleting publication")
		if _, err := sourceDBAdmin.Exec(fmt.Sprintf("DROP PUBLICATION %s", DefaultPubName)); err != nil {
			log.Error(err, "delete publication failed")
			return nil, err
		}
	} else {
		log.Info("publication not found. ignoring and moving on")
	}

	log.Info("completed")
	return &completed_state{
		config: s.config,
	}, nil
}
func (s *delete_publication_state) Id() StateEnum {
	return S_DeletePublication
}
func (s *delete_publication_state) String() string {
	return S_DeletePublication.String()
}

func (s *completed_state) Execute() (State, error) {

	return nil, nil
}
func (s *completed_state) Id() StateEnum {
	return S_Completed
}
func (s *completed_state) String() string {
	return S_Completed.String()
}

func (s *retry_state) Execute() (State, error) {
	return nil, nil
}
func (s *retry_state) Id() StateEnum {
	return S_Retry
}
func (s *retry_state) String() string {
	return S_Retry.String()
}

func retry(c Config) State {
	return &retry_state{
		config: c,
	}
}
