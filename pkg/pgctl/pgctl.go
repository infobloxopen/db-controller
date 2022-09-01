package pgctl

import (
	"database/sql"
	"fmt"

	"github.com/infobloxopen/db-controller/pkg/pgutil"
)

var errUnimplemented = fmt.Errorf("unimplemented")

type StateEnum int

const (
	S_Initial StateEnum = iota
	S_ValidateConnection
	S_CreatePublication
	S_DumpSourceSchema
	S_RestoreTargetSchema
	S_CreateSubscription
	S_EnableSubscription
	S_CutOverReadinessCheck
	S_ResetTargetSequence
	S_RerouteTargetSecret
	S_ValidateMigrationStatus
	S_DisableSourceAccess
	S_DisableSubscription
	S_DeleteSubscription
	S_DeletePublication
)

func (s StateEnum) String() string {
	switch s {
	case S_Initial:
		return "initial"
	case S_ValidateConnection:
		return "validate_connection"
	case S_CreatePublication:
		return "create_publication"
	case S_DumpSourceSchema:
		return "dump_source_schema"
	case S_RestoreTargetSchema:
		return "restore_target_schema"
	case S_CreateSubscription:
		return "create_subscription"
	case S_EnableSubscription:
		return "enable_subscription"
	case S_CutOverReadinessCheck:
		return "cut_over_readiness_check"
	case S_ResetTargetSequence:
		return "reset_target_sequence"
	case S_RerouteTargetSecret:
		return "reroute_target_secret"
	case S_ValidateMigrationStatus:
		return "validate_migration_status"
	case S_DisableSourceAccess:
		return "disable_source_access"
	case S_DisableSubscription:
		return "create_publication"
	case S_DeleteSubscription:
		return "delete_subscription"
	case S_DeletePublication:
		return "delete_publication"
	}

	return "unknown"
}

type State interface {
	Execute() (State, error)
	Id() StateEnum
	String() string
}

type Config struct {
	SourceDBAdminDsn string
	SourceDBUserDsn  string
	TargetDBAdminDsn string
	TargetDBUserDsn  string
	SourceDBAdmin    *sql.DB
	SourceDBUser     *sql.DB
	TargetDBAdmin    *sql.DB
	TargetDBUser     *sql.DB
}

type initial_state struct{ config Config }
type create_publication_state struct{ config Config }
type validate_connection_state struct{ config Config }
type dump_source_schema_state struct{ config Config }
type restore_target_schema_state struct{ config Config }
type create_subscription_state struct{ config Config }
type enable_subscription_state struct{ config Config }
type cut_over_readiness_check_state struct{ config Config }
type reset_target_sequence_state struct{ config Config }
type reroute_target_secret_state struct{ config Config }
type validate_migration_status_state struct{ config Config }
type disable_source_access_state struct{ config Config }
type delete_subscription_state struct{ config Config }
type delete_publication_state struct{ config Config }

// validate that each class implements State interface
var _ State = &initial_state{}
var _ State = &create_publication_state{}
var _ State = &validate_connection_state{}
var _ State = &dump_source_schema_state{}
var _ State = &restore_target_schema_state{}
var _ State = &create_subscription_state{}
var _ State = &enable_subscription_state{}
var _ State = &cut_over_readiness_check_state{}
var _ State = &reset_target_sequence_state{}
var _ State = &reroute_target_secret_state{}
var _ State = &validate_migration_status_state{}
var _ State = &disable_source_access_state{}
var _ State = &delete_subscription_state{}
var _ State = &delete_publication_state{}

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

	var err error
	if s.config.SourceDBAdmin, err = pgutil.TestDBConnection(s.config.SourceDBAdminDsn, s.config.SourceDBAdmin); err != nil {
		return nil, err
	}
	if s.config.SourceDBUser, err = pgutil.TestDBConnection(s.config.SourceDBUserDsn, s.config.SourceDBUser); err != nil {
		return nil, err
	}
	if s.config.TargetDBAdmin, err = pgutil.TestDBConnection(s.config.TargetDBAdminDsn, s.config.TargetDBAdmin); err != nil {
		return nil, err
	}
	if s.config.TargetDBUser, err = pgutil.TestDBConnection(s.config.TargetDBUserDsn, s.config.TargetDBUser); err != nil {
		return nil, err
	}
	if valid, err := pgutil.ValidatePermission(s.config.SourceDBAdmin); !valid {
		return nil, fmt.Errorf("%w; Source DB Admin user lacks  required permission", err)
	}
	if valid, err := pgutil.ValidatePermission(s.config.TargetDBAdmin); !valid {
		return nil, fmt.Errorf("%w; Target DB Admin user lacks  required permission", err)
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
	return &dump_source_schema_state{
		config: s.config,
	}, nil
}
func (s *create_publication_state) Id() StateEnum {
	return S_CreatePublication
}
func (s *create_publication_state) String() string {
	return S_CreatePublication.String()
}

func (s *dump_source_schema_state) Execute() (State, error) {
	return &restore_target_schema_state{
		config: s.config,
	}, nil
}
func (s *dump_source_schema_state) Id() StateEnum {
	return S_DumpSourceSchema
}
func (s *dump_source_schema_state) String() string {
	return S_DumpSourceSchema.String()
}

func (s *restore_target_schema_state) Execute() (State, error) {
	return &create_subscription_state{
		config: s.config,
	}, nil
}
func (s *restore_target_schema_state) Id() StateEnum {
	return S_RestoreTargetSchema
}
func (s *restore_target_schema_state) String() string {
	return S_RestoreTargetSchema.String()
}

func (s *create_subscription_state) Execute() (State, error) {
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
	return &validate_connection_state{
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
	return &validate_connection_state{
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
	return &validate_connection_state{
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
	return &validate_connection_state{
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
	return &validate_connection_state{
		config: s.config,
	}, nil
}
func (s *disable_source_access_state) Id() StateEnum {
	return S_DisableSourceAccess
}
func (s *disable_source_access_state) String() string {
	return S_DisableSourceAccess.String()
}

func (s *delete_subscription_state) Execute() (State, error) {
	return &validate_connection_state{
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
	return nil, errUnimplemented
}
func (s *delete_publication_state) Id() StateEnum {
	return S_DeletePublication
}
func (s *delete_publication_state) String() string {
	return S_DeletePublication.String()
}
