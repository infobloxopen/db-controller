package pgctl

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"

	"github.com/go-logr/logr"
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
type validate_migration_status_state struct{ config Config }
type disable_source_access_state struct{ config Config }
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
var _ State = &validate_migration_status_state{}
var _ State = &disable_source_access_state{}
var _ State = &delete_subscription_state{}
var _ State = &delete_publication_state{}
var _ State = &completed_state{}
var _ State = &retry_state{}

// func GetReplicatorState(log logr.Logger, sourceDBAdminDsn string, sourceDBUserDsn string, targetDBAdminDsn string, targetDBUserDsn string, stateName string) (State, error) {

// 	return getCurrentState(stateName, Config{Log: log,
// 		SourceDBAdminDsn: sourceDBAdminDsn,
// 		SourceDBUserDsn:  sourceDBUserDsn,
// 		TargetDBAdminDsn: targetDBAdminDsn,
// 		TargetDBUserDsn:  targetDBUserDsn})
// }

func GetReplicatorState(name string, c Config) (State, error) {
	log := c.Log.WithValues("state", name)

	stateEnum, err := getStateEnum(name)
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

	createPub := fmt.Sprintf(`
		CREATE PUBLICATION %s 
		FOR ALL TABLES`, DefaultPubName)

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

func (s *copy_schema_state) Execute() (State, error) {
	log := s.config.Log.WithValues("state", s.String())

	dump := NewDump(s.config.SourceDBAdminDsn)

	dump.SetupFormat("p")
	dump.SetPath(s.config.ExportFilePath)

	dump.EnableVerbose()

	dump.SetOptions([]string{"--schema-only", "--no-publication", "--no-subscriptions"})

	dumpExec := dump.Exec(ExecOptions{StreamPrint: true})
	log.Info("executing", "full command", dumpExec.FullCommand)

	if dumpExec.Error != nil {
		return nil, dumpExec.Error.Err
	}

	restore := NewRestore(s.config.TargetDBAdminDsn)
	restore.EnableVerbose()

	restore.Path = s.config.ExportFilePath
	restoreExec := restore.Exec(dumpExec.FileName, ExecOptions{StreamPrint: true})

	log.Info("restore", "full command", restoreExec.FullCommand)

	if restoreExec.Error != nil {
		return nil, restoreExec.Error.Err
	}

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
		log.Error(err, "could not query for subsription name", "stmt", createSub)
		return nil, err
	}
	if !exists {
		log.Info("creating subsription:", "with name", DefaultSubName)
		if _, err := targetDBAdmin.Exec(createSub); err != nil {
			log.Error(err, "could not create subsription")
			return nil, err
		}
		log.Info("subsription created", "name", DefaultSubName)
	}

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

	log.Info("Subscription enabled successfully")

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

	subQuery := fmt.Sprintf(`
		SELECT count(srrelid) 
		FROM pg_subscription s, pg_subscription_rel sr
		WHERE s.oid = sr.srsubid
		AND sr.srsubstate <> 'r'
		AND s.subname = '%s'`, DefaultSubName)

	err = sourceDBAdmin.QueryRow(pubQuery).Scan(&exists)
	if err != nil {
		log.Error(err, "could not query for pg_replication_slots")
		return nil, err
	}
	if exists {
		log.Info("Migration not complete in source - retry check in a few seconds")
		return retry(s.config), nil
	}

	err = targetDBAdmin.QueryRow(subQuery).Scan(&count)
	if err != nil {
		log.Error(err, "could not query for subscription for completion")
		return nil, err
	}
	if count > 0 {
		log.Info("Migration not complete in target - retry check in a few seconds")
		return retry(s.config), nil
	}

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

	seqsQ := "SELECT sequence_name FROM information_schema.sequences"
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
	return &validate_migration_status_state{
		config: s.config,
	}, nil
}
func (s *reroute_target_secret_state) Id() StateEnum {
	return S_RerouteTargetSecret
}
func (s *reroute_target_secret_state) String() string {
	return S_RerouteTargetSecret.String()
}

func (s *validate_migration_status_state) Execute() (State, error) {
	//log := s.config.log.WithValues("state", s.String())
	//place holder to implement any validation
	return &disable_source_access_state{
		config: s.config,
	}, nil
}
func (s *validate_migration_status_state) Id() StateEnum {
	return S_ValidateMigrationStatus
}
func (s *validate_migration_status_state) String() string {
	return S_ValidateMigrationStatus.String()
}

func (s *disable_source_access_state) Execute() (State, error) {
	log := s.config.Log.WithValues("state", s.String())
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

	stmt := `
		REVOKE insert, delete, update
		ON ALL TABLES IN SCHEMA PUBLIC FROM %s`

	_, err = sourceDBAdmin.Exec(fmt.Sprintf(stmt, u.User.Username()))
	if err != nil {
		log.Error(err, "failed revoking access for source db")
		return nil, err
	}

	return &disable_subscription_state{
		config: s.config,
	}, nil
}
func (s *disable_source_access_state) Id() StateEnum {
	return S_DisableSourceAccess
}
func (s *disable_source_access_state) String() string {
	return S_DisableSourceAccess.String()
}

func (s *disable_subscription_state) Execute() (State, error) {
	log := s.config.Log.WithValues("state", s.String())
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

	log.Info("Subscription disabled")

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
	log.Info("publication deleted")

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
