package pgctl

import (
	"cmp"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/infobloxopen/db-controller/internal/dockerdb"
	"github.com/lib/pq"
	"go.uber.org/zap/zapcore"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	SourceDBAdminDsn     string
	SourceDBUserDsn      string
	TargetDBAdminDsn     string
	TargetDBUserDsn      string
	dropSchemaDBAdminDsn string

	DataTestSourceAdminDsn string
	DataTestSourceUserDsn  string
	DataTestTargetAdminDsn string
	DataTestTargetUserDsn  string

	ExportFilePath = "/tmp/"
	repository     = "postgres"
	sourceVersion  = "13.14"
	targetVersion  = "15.6"
	sourcePort     = "15435"
	targetPort     = "15436"
	testDBNetwork  = "testDBNetwork"
)

var logger logr.Logger

func TestMain(m *testing.M) {
	//need to do this trick to avoid os.Exit bypassing defer logic
	//with this sill setup, defer is called in realTestMain before the exit is called in this func
	os.Exit(setupAndRunTests(m))
}

func setupAndRunTests(m *testing.M) int {
	opts := zap.Options{
		Development: true,
		Level:       zapcore.InfoLevel,
	}
	logger = zap.New(zap.UseFlagOptions(&opts))

	networkName := "pgctl"
	removeNetwork := dockerdb.StartNetwork(networkName)
	defer removeNetwork()

	// -----------------------------------------------------------------------
	// Set up source and target databases for unit testing each step of the
	// migration.

	// FIXME: randomly generate network name
	_, sourceDSN, sourceClose := dockerdb.Run(logger, dockerdb.Config{
		HostName:  "pubHost",
		DockerTag: sourceVersion,
		Database:  "pub",
		Username:  "sourceAdmin",
		Password:  "sourceSecret",
		Network:   networkName,
	})
	defer sourceClose()

	if err := loadSourceTestData(sourceDSN); err != nil {
		panic(err)
	}

	_, targetDSN, targetClose := dockerdb.Run(logger, dockerdb.Config{
		HostName:  "subHost",
		DockerTag: targetVersion,
		Database:  "sub",
		Username:  "targetAdmin",
		Password:  "targetSecret",
		Network:   networkName,
	})
	defer targetClose()

	if err := loadTargetTestData(targetDSN); err != nil {
		panic(err)
	}

	if _, err := url.Parse(sourceDSN); err != nil {
		panic(err)
	}

	TargetDBAdminDsn = targetDSN
	SourceDBAdminDsn = sourceDSN

	TargetDBUserDsn = changeUserInfo(TargetDBAdminDsn, "appuser_b", "secret")
	SourceDBUserDsn = changeUserInfo(SourceDBAdminDsn, "appuser_a", "secret")

	// -----------------------------------------------------------------------
	// Set up source and target databases for unit testing each step of the
	// migration.

	_, dataTestSourceAdminDSN, dataTestSourceClose := dockerdb.Run(logger, dockerdb.Config{
		HostName:  "dataTestSourceHost",
		DockerTag: sourceVersion,
		Database:  "dataTestSource",
		Username:  "dataTestSourceAdmin",
		Password:  "dataTestSourceSecret",
		Network:   networkName,
	})
	defer dataTestSourceClose()

	if err := loadSourceTestData(dataTestSourceAdminDSN); err != nil {
		panic(err)
	}

	_, dataTestTargetAdminDSN, dataTestTargetClose := dockerdb.Run(logger, dockerdb.Config{
		HostName:  "dataTestTargetHost",
		DockerTag: targetVersion,
		Database:  "dataTestTarget",
		Username:  "dataTestTargetAdmin",
		Password:  "dataTestTargetSecret",
		Network:   networkName,
	})
	defer dataTestTargetClose()

	if err := loadTargetTestData(dataTestTargetAdminDSN); err != nil {
		panic(err)
	}

	if _, err := url.Parse(dataTestSourceAdminDSN); err != nil {
		panic(err)
	}

	DataTestSourceAdminDsn = dataTestSourceAdminDSN
	DataTestTargetAdminDsn = dataTestTargetAdminDSN

	DataTestSourceUserDsn = changeUserInfo(DataTestSourceAdminDsn, "appuser_a", "secret")
	DataTestTargetUserDsn = changeUserInfo(DataTestTargetAdminDsn, "appuser_b", "secret")

	// -----------------------------------------------------------------------
	// Set up a database for testing the drop schema functionality.

	_, dropSchemaDSN, dropSchemaClose := dockerdb.Run(logger, dockerdb.Config{
		HostName:  "dropSchemaHost",
		DockerTag: targetVersion,
		Database:  "sub",
		Username:  "dropschemaadmin",
		Password:  "dropSchemaSecret",
		Network:   networkName,
	})
	defer dropSchemaClose()

	dropSchemaDBAdminDsn = dropSchemaDSN

	// -----------------------------------------------------------------------

	return m.Run()
}

func loadSourceTestData(dsn string) error {
	_, err := Exec("psql", dsn, "-f", "./test/pgctl_source_test_data.sql", "-v", "end=50")
	if err != nil {
		return err
	}
	return nil
}

func loadTargetTestData(dsn string) error {
	_, err := Exec("psql", dsn, "-f", "./test/pgctl_target_test_data.sql")
	if err != nil {
		return err
	}
	return nil
}

func TestEachStateTransitions(t *testing.T) {
	testInitialState(t)
	testValidateConnectionStateExecute(t)
	testCreatePublicationStateExecute(t)
	testCopySchemaStateExecute(t)
	testCreateSubscriptionStateExecute(t)
	testEnableSubscriptionStateExecute(t)
	testCutOverReadinessCheckStateExecute(t)
	testResetTargetSequenceStateExecute(t)
	testRerouteTargetSecretStateExecute(t)
	testWaitToDisableSourceStateExecute(t)
	testDisableSourceAccessStateExecute(t)
	testValidateMigrationStatusStateExecute(t)
	testDisableSubscriptionStateExecute(t)
	testDeleteSubscriptionStateExecute(t)
	testDeletePublicationStateExecute(t)
}

func testInitialState(t *testing.T) {
	type testcase struct {
		args          Config
		name          string
		eErr          error
		expectedErr   bool
		expectedState StateEnum
	}

	tests := []testcase{
		{name: "testInitialState_empty", expectedErr: true, args: Config{}},
		{name: "target Admin empty", expectedErr: true,
			args: Config{
				Log:              logger,
				SourceDBAdminDsn: SourceDBAdminDsn,
				SourceDBUserDsn:  SourceDBUserDsn,
				TargetDBUserDsn:  TargetDBUserDsn,
			},
		},
		{name: "testInitialState_target User empty", expectedErr: true,
			args: Config{
				Log:              logger,
				SourceDBAdminDsn: SourceDBAdminDsn,
				SourceDBUserDsn:  SourceDBUserDsn,
				TargetDBAdminDsn: TargetDBAdminDsn,
			},
		},
		{name: "testInitialState_Source Admin empty", expectedErr: true,
			args: Config{
				Log:              logger,
				SourceDBUserDsn:  SourceDBUserDsn,
				TargetDBAdminDsn: TargetDBAdminDsn,
				TargetDBUserDsn:  TargetDBUserDsn,
			},
		},
		{name: "testInitialState_Source User empty", expectedErr: true,
			args: Config{
				Log: logger,

				SourceDBAdminDsn: SourceDBAdminDsn,
				TargetDBAdminDsn: TargetDBAdminDsn,
				TargetDBUserDsn:  TargetDBUserDsn,
			},
		},
		{name: "testInitialState_ok", expectedErr: false, expectedState: S_ValidateConnection,
			args: Config{
				Log:              logger,
				SourceDBAdminDsn: SourceDBAdminDsn,
				SourceDBUserDsn:  SourceDBUserDsn,
				TargetDBUserDsn:  TargetDBUserDsn,
				TargetDBAdminDsn: TargetDBAdminDsn,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &initial_state{config: tt.args}

			nextState, err := s.Execute()
			if tt.expectedErr && err == nil {
				t.Fatalf("test case  %s: expected error got nil", tt.name)
			}

			if tt.expectedErr == false && err != nil {
				t.Fatalf("test case %s: expected no error, got %s", tt.name, err)
			}
			if nextState != nil {
				if tt.expectedState != nextState.Id() {
					t.Fatalf(" test case %s: expected %s, got %s", tt.name, tt.expectedState, nextState.Id())
				}
			}
		})
	}
}

func testValidateConnectionStateExecute(t *testing.T) {
	type fields struct {
		config Config
	}
	tests := []struct {
		name    string
		fields  fields
		want    StateEnum
		wantErr bool
	}{
		{name: "test_validate_connection_state_Execute_no_admin_access", wantErr: true,
			fields: fields{Config{
				Log: logger,
				// SourceDBAdminDsn: "postgres://noadminaccess:secret@localhost:" + sourcePort + "/pub?sslmode=disable",
				SourceDBAdminDsn: changeUserInfo(SourceDBAdminDsn, "noadminaccess", "secret"),
				SourceDBUserDsn:  SourceDBUserDsn,
				TargetDBUserDsn:  TargetDBUserDsn,
				TargetDBAdminDsn: TargetDBAdminDsn,
			}},
		},
		{name: "test_validate_connection_state_Execute_ok", wantErr: false, want: S_CreatePublication,
			fields: fields{Config{
				Log:              logger,
				SourceDBAdminDsn: SourceDBAdminDsn,
				SourceDBUserDsn:  SourceDBUserDsn,
				TargetDBUserDsn:  TargetDBUserDsn,
				TargetDBAdminDsn: TargetDBAdminDsn,
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &validate_connection_state{
				config: tt.fields.config,
			}
			got, err := s.Execute()

			if (err != nil) != tt.wantErr {
				t.Fatalf("validate_connection_state.Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err != nil {
				return
			}
			if cmp.Compare(got.Id(), tt.want) != 0 {
				t.Errorf("validate_connection_state.Execute() = %v, want %v", got.Id(), tt.want)
			}
		})
	}
}

func testCreatePublicationStateExecute(t *testing.T) {
	type fields struct {
		config Config
	}
	tests := []struct {
		name    string
		fields  fields
		want    StateEnum
		wantErr bool
	}{
		{name: "test_create_publication_state_Execute_ok", wantErr: false, want: S_CopySchema,
			fields: fields{Config{
				Log:              logger,
				SourceDBAdminDsn: SourceDBAdminDsn,
				SourceDBUserDsn:  SourceDBUserDsn,
				TargetDBUserDsn:  TargetDBUserDsn,
				TargetDBAdminDsn: TargetDBAdminDsn,
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &create_publication_state{
				config: tt.fields.config,
			}
			got, err := s.Execute()
			if (err != nil) != tt.wantErr {
				t.Fatalf("create_publication_state.Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
			if cmp.Compare(got.Id(), tt.want) != 0 {
				t.Errorf("create_publication_state.Execute() = %v, want %v", got.Id(), tt.want)
			}
		})
	}
}

func testCopySchemaStateExecute(t *testing.T) {

	oldGrantSuper := grantSuperUserAccess
	oldRevokeSuper := revokeSuperUserAccess
	defer func() {
		grantSuperUserAccess = oldGrantSuper
		revokeSuperUserAccess = oldRevokeSuper
	}()

	// FIXME: dont do this
	// This overrides a var grantSuperUserAccess to handle the special case in unit test.
	// Unit test uses different methods to set and unset superuser permission.
	// The difference is related to using postgres vs RDS - the superuser permission is handled differently in AWS RDS.
	grantSuperUserAccess = func(DBAdmin *sql.DB, role string, cloud string) error {
		_, err := DBAdmin.Exec(fmt.Sprintf("ALTER ROLE %s WITH SUPERUSER;", pq.QuoteIdentifier(role)))
		return err
	}
	revokeSuperUserAccess = func(DBAdmin *sql.DB, role string, cloud string) error {
		_, err := DBAdmin.Exec(fmt.Sprintf("ALTER ROLE %s WITH NOSUPERUSER;", pq.QuoteIdentifier(role)))
		return err
	}

	type fields struct {
		config Config
	}

	tests := []struct {
		name    string
		fields  fields
		want    StateEnum
		wantErr bool
	}{
		{name: "test_copy_schema_state_Execute_ok", wantErr: false, want: S_CreateSubscription,
			fields: fields{Config{
				Log:              logger,
				SourceDBAdminDsn: SourceDBAdminDsn,
				SourceDBUserDsn:  SourceDBUserDsn,
				TargetDBUserDsn:  TargetDBUserDsn,
				TargetDBAdminDsn: TargetDBAdminDsn,
				ExportFilePath:   ExportFilePath,
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &copy_schema_state{
				config: tt.fields.config,
			}
			got, err := s.Execute()
			if (err != nil) != tt.wantErr {
				t.Fatalf("copy_schema_state.Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
			if cmp.Compare(got.Id(), tt.want) != 0 {
				t.Errorf("copy_schema_state.Execute() = %v, want %v", got.Id(), tt.want)
			}
		})
	}
}

func testCreateSubscriptionStateExecute(t *testing.T) {
	type fields struct {
		config Config
	}
	tests := []struct {
		name    string
		fields  fields
		want    StateEnum
		wantErr bool
	}{
		{name: "test_create_subscription_state_Execute_ok", wantErr: false, want: S_EnableSubscription,
			fields: fields{Config{
				Log: logger,
				// During subscription creation, SourceDBAdminDsn is used to configure subscription in the target database to connect to source
				// pub postgres db. In the unit test scenario, since the docker is setup with bridge network (during pool setup time), the SourceDBAdminDsn is set with the docker host name and port.
				// In a real scenario - the regular DSN will be good enough
				SourceDBAdminDsn: "postgres://sourceAdmin:sourceSecret@pubHost:5432/pub?sslmode=disable",
				SourceDBUserDsn:  SourceDBUserDsn,
				TargetDBUserDsn:  TargetDBUserDsn,
				TargetDBAdminDsn: TargetDBAdminDsn,
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &create_subscription_state{
				config: tt.fields.config,
			}
			got, err := s.Execute()
			if (err != nil) != tt.wantErr {
				t.Fatalf("create_subscription_state.Execute() error = %v, wantErr %v", err, tt.wantErr)
			}

			if cmp.Compare(got.Id(), tt.want) != 0 {
				t.Errorf("create_subscription_state.Execute() = %v, want %v", got.Id(), tt.want)
			}
		})
	}
}

func testEnableSubscriptionStateExecute(t *testing.T) {
	type fields struct {
		config Config
	}
	tests := []struct {
		name    string
		fields  fields
		want    StateEnum
		wantErr bool
	}{
		{name: "test_enable_subscription_state_Execute_ok", wantErr: false, want: S_CutOverReadinessCheck,
			fields: fields{Config{
				Log:              logger,
				SourceDBAdminDsn: SourceDBAdminDsn,
				SourceDBUserDsn:  SourceDBUserDsn,
				TargetDBUserDsn:  TargetDBUserDsn,
				TargetDBAdminDsn: TargetDBAdminDsn,
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &enable_subscription_state{
				config: tt.fields.config,
			}
			got, err := s.Execute()
			if (err != nil) != tt.wantErr {
				t.Fatalf("enable_subscription_state.Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(got.Id(), tt.want) {
				t.Fatalf("enable_subscription_state.Execute() = %v, want %v", got.Id(), tt.want)
			}
		})
	}
}

func testCutOverReadinessCheckStateExecute(t *testing.T) {
	type fields struct {
		config Config
	}
	tests := []struct {
		name    string
		fields  fields
		want    StateEnum
		wantErr bool
	}{
		{
			name:    "test_cut_over_readiness_check_state_Executeok",
			wantErr: false,
			want:    S_Retry,
			fields: fields{Config{
				Log:              logger,
				SourceDBAdminDsn: SourceDBAdminDsn,
				SourceDBUserDsn:  SourceDBUserDsn,
				TargetDBUserDsn:  TargetDBUserDsn,
				TargetDBAdminDsn: TargetDBAdminDsn,
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &cut_over_readiness_check_state{
				config: tt.fields.config,
			}
			got, err := s.Execute()

			if (err != nil) != tt.wantErr {
				t.Fatalf("error: %v wantErr: %v", err, tt.wantErr)
				return
			}

			if cmp.Compare(got.Id(), tt.want) != 0 {
				t.Fatalf("   got: %v\nwanted: %v", got.Id(), tt.want)
			}
		})
	}
}

func testResetTargetSequenceStateExecute(t *testing.T) {
	type fields struct {
		config Config
	}
	tests := []struct {
		name    string
		fields  fields
		want    StateEnum
		wantErr bool
	}{
		{name: "test_reset_target_sequence_state_Execute_ok", wantErr: false, want: S_RerouteTargetSecret,
			fields: fields{Config{
				Log:              logger,
				SourceDBAdminDsn: SourceDBAdminDsn,
				SourceDBUserDsn:  SourceDBUserDsn,
				TargetDBUserDsn:  TargetDBUserDsn,
				TargetDBAdminDsn: TargetDBAdminDsn,
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &reset_target_sequence_state{
				config: tt.fields.config,
			}
			got, err := s.Execute()
			if (err != nil) != tt.wantErr {
				t.Fatalf("reset_target_sequence_state.Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
			if cmp.Compare(got.Id(), tt.want) != 0 {
				t.Fatalf("reset_target_sequence_state.Execute() = %v, want %v", got.Id(), tt.want)
			}
		})
	}
}

func testRerouteTargetSecretStateExecute(t *testing.T) {
	type fields struct {
		config Config
	}
	tests := []struct {
		name    string
		fields  fields
		want    StateEnum
		wantErr bool
	}{
		{name: "test_reroute_target_secret_state_Execute_ok", wantErr: false, want: S_WaitToDisableSource,
			fields: fields{Config{
				Log:              logger,
				SourceDBAdminDsn: SourceDBAdminDsn,
				SourceDBUserDsn:  SourceDBUserDsn,
				TargetDBUserDsn:  TargetDBUserDsn,
				TargetDBAdminDsn: TargetDBAdminDsn,
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &reroute_target_secret_state{
				config: tt.fields.config,
			}
			got, err := s.Execute()
			if (err != nil) != tt.wantErr {
				t.Fatalf("reroute_target_secret_state.Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
			if cmp.Compare(got.Id(), tt.want) != 0 {
				t.Fatalf("reroute_target_secret_state.Execute() = %v, want %v", got.Id(), tt.want)
			}
		})
	}
}

func testValidateMigrationStatusStateExecute(t *testing.T) {
	type fields struct {
		config Config
	}
	tests := []struct {
		name    string
		fields  fields
		want    StateEnum
		wantErr bool
	}{
		{name: "test_validate_migration_status_state_ok", wantErr: false, want: S_DisableSubscription,
			fields: fields{Config{
				Log:              logger,
				SourceDBAdminDsn: SourceDBAdminDsn,
				SourceDBUserDsn:  SourceDBUserDsn,
				TargetDBUserDsn:  TargetDBUserDsn,
				TargetDBAdminDsn: TargetDBAdminDsn,
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &validate_migration_status_state{
				config: tt.fields.config,
			}
			dockerdb.RetryFn(t, func() error {
				got, err := s.Execute()
				if (err != nil) != tt.wantErr {
					t.Fatalf("validate_migration_status_state.Execute()\n   got: %s", err)
				}

				if e := tt.want; cmp.Compare(got.Id(), tt.want) != 0 {
					return fmt.Errorf("   got: %s\nwanted: %s", got.Id(), e)
				}
				return nil
			}, 500*time.Millisecond, 20*time.Second)
		})
	}
}

func testWaitToDisableSourceStateExecute(t *testing.T) {
	type fields struct {
		config Config
	}
	tests := []struct {
		name    string
		fields  fields
		want    StateEnum
		wantErr bool
	}{
		{name: "test_wait_to_disable_source_state_Execute_ok", wantErr: false, want: S_DisableSourceAccess,
			fields: fields{Config{
				Log:              logger,
				SourceDBAdminDsn: SourceDBAdminDsn,
				SourceDBUserDsn:  SourceDBUserDsn,
				TargetDBUserDsn:  TargetDBUserDsn,
				TargetDBAdminDsn: TargetDBAdminDsn,
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &wait_to_disable_source_state{
				config: tt.fields.config,
			}
			got, err := s.Execute()
			//simulate the wait
			if (err != nil) != tt.wantErr {
				t.Fatalf("test_wait_to_disable_source_state_Execute error = %v, wantErr %v", err, tt.wantErr)
			}
			if cmp.Compare(got.Id(), tt.want) != 0 {
				t.Fatalf("test_wait_to_disable_source_state_Execute = %v, want %v", got.Id(), tt.want)
			}
		})
	}
}

func testDisableSourceAccessStateExecute(t *testing.T) {
	type fields struct {
		config Config
	}
	tests := []struct {
		name    string
		fields  fields
		want    StateEnum
		wantErr bool
	}{
		{name: "test_disable_source_access_state_ok", wantErr: false, want: S_ValidateMigrationStatus,
			fields: fields{Config{
				Log:              logger,
				SourceDBAdminDsn: SourceDBAdminDsn,
				SourceDBUserDsn:  SourceDBUserDsn,
				TargetDBUserDsn:  TargetDBUserDsn,
				TargetDBAdminDsn: TargetDBAdminDsn,
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &disable_source_access_state{
				config: tt.fields.config,
			}
			got, err := s.Execute()
			if (err != nil) != tt.wantErr {
				t.Fatalf("disable_source_access_state.Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
			if cmp.Compare(got.Id(), tt.want) != 0 {
				t.Fatalf("disable_source_access_state.Execute() = %v, want %v", got.Id(), tt.want)
			}
		})
	}
}

func testDisableSubscriptionStateExecute(t *testing.T) {
	type fields struct {
		config Config
	}
	tests := []struct {
		name    string
		fields  fields
		want    StateEnum
		wantErr bool
	}{
		{name: "test_disable_subscription_state_Execute_ok", wantErr: false, want: S_DeleteSubscription,
			fields: fields{Config{
				Log:              logger,
				SourceDBAdminDsn: SourceDBAdminDsn,
				SourceDBUserDsn:  SourceDBUserDsn,
				TargetDBUserDsn:  TargetDBUserDsn,
				TargetDBAdminDsn: TargetDBAdminDsn,
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &disable_subscription_state{
				config: tt.fields.config,
			}
			got, err := s.Execute()
			if (err != nil) != tt.wantErr {
				t.Fatalf("enable_subscription_state.Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
			if cmp.Compare(got.Id(), tt.want) != 0 {
				t.Fatalf("enable_subscription_state.Execute() = %v, want %v", got.Id(), tt.want)
			}
		})
	}
}

func testDeleteSubscriptionStateExecute(t *testing.T) {
	type fields struct {
		config Config
	}
	tests := []struct {
		name    string
		fields  fields
		want    StateEnum
		wantErr bool
	}{
		{name: "test_delete_subscription_state_Execute_ok", wantErr: false, want: S_DeletePublication,
			fields: fields{Config{
				Log:              logger,
				SourceDBAdminDsn: SourceDBAdminDsn,
				SourceDBUserDsn:  SourceDBUserDsn,
				TargetDBUserDsn:  TargetDBUserDsn,
				TargetDBAdminDsn: TargetDBAdminDsn,
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &delete_subscription_state{
				config: tt.fields.config,
			}
			got, err := s.Execute()
			if (err != nil) != tt.wantErr {
				t.Fatalf("delete_subscription_state.Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
			if cmp.Compare(got.Id(), tt.want) != 0 {
				t.Fatalf("delete_subscription_state.Execute() = %v, want %v", got.Id(), tt.want)
			}
		})
	}
}

func testDeletePublicationStateExecute(t *testing.T) {
	type fields struct {
		config Config
	}
	tests := []struct {
		name    string
		fields  fields
		want    StateEnum
		wantErr bool
	}{
		{name: "test_delete_publication_state_Execute_ok", wantErr: false, want: S_Completed,
			fields: fields{Config{
				Log:              logger,
				SourceDBAdminDsn: SourceDBAdminDsn,
				SourceDBUserDsn:  SourceDBUserDsn,
				TargetDBUserDsn:  TargetDBUserDsn,
				TargetDBAdminDsn: TargetDBAdminDsn,
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &delete_publication_state{
				config: tt.fields.config,
			}
			got, err := s.Execute()
			if (err != nil) != tt.wantErr {
				t.Fatalf("delete_publication_state.Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
			if cmp.Compare(got.Id(), tt.want) != 0 {
				t.Fatalf("delete_publication_state.Execute() = %v, want %v", got.Id(), tt.want)
			}
		})
	}
}

func TestDataAfterMigration(t *testing.T) {
	// Setup Config for migration.
	config := Config{
		Log:              logger,
		SourceDBAdminDsn: DataTestSourceAdminDsn,
		SourceDBUserDsn:  DataTestSourceUserDsn,
		TargetDBUserDsn:  DataTestTargetUserDsn,
		TargetDBAdminDsn: DataTestTargetAdminDsn,
		ExportFilePath:   ExportFilePath,
	}

	oldGrantSuper := grantSuperUserAccess
	oldRevokeSuper := revokeSuperUserAccess

	// Get initial state and begin executing the migration.
	state, err := GetReplicatorState("", config)
	if err != nil {
		t.Fatalf("failed to get initial state: %v", err)
	}

	maxRetries := 10 // Limit the number of retries to avoid infinite loops.
	retryCount := 0
	for {
		if next := state.Id(); next == S_CopySchema {
			// This overrides a var grantSuperUserAccess to handle the special case in unit test.
			grantSuperUserAccess = func(DBAdmin *sql.DB, role string, cloud string) error {
				_, err := DBAdmin.Exec(fmt.Sprintf("ALTER ROLE %s WITH SUPERUSER;", pq.QuoteIdentifier(role)))
				return err
			}
			revokeSuperUserAccess = func(DBAdmin *sql.DB, role string, cloud string) error {
				_, err := DBAdmin.Exec(fmt.Sprintf("ALTER ROLE %s WITH NOSUPERUSER;", pq.QuoteIdentifier(role)))
				return err
			}
		}

		next, err := state.Execute()
		if err != nil {
			t.Fatalf("error during state execution: %v", err)
		}

		if next.Id() != S_CopySchema {
			// Reset grantSuperUserAccess and revokeSuperUserAccess to original functions.
			grantSuperUserAccess = oldGrantSuper
			revokeSuperUserAccess = oldRevokeSuper
		}

		// Check if the state has returned a retry.
		if _, isRetry := next.(*retry_state); isRetry {
			retryCount++
			if retryCount > maxRetries {
				t.Fatalf("exceeded maximum retry attempts")
			}
			t.Logf("retrying... attempt %d", retryCount)
			time.Sleep(2 * time.Second) // Add a delay between retries to avoid busy looping.
			continue
		}

		// Reset retry count if we move past a retry state.
		retryCount = 0

		// Check for completion.
		if next.Id() == S_Completed {
			break
		}

		state = next
	}

	// Validate data after migration is complete.
	testDataAfterMigration(t)
}

func testDataAfterMigration(t *testing.T) {
	srcDB, err := sql.Open("postgres", DataTestSourceAdminDsn)
	if err != nil {
		t.Fatalf("failed to connect to source: %v", err)
	}
	defer srcDB.Close()

	tgtDB, err := sql.Open("postgres", DataTestTargetAdminDsn)
	if err != nil {
		t.Fatalf("failed to connect to target: %v", err)
	}
	defer tgtDB.Close()

	// Check row counts for key tables.
	checkRowCount(t, srcDB, tgtDB, "tab_1")
	checkRowCount(t, srcDB, tgtDB, "tab_2")
	checkRowCount(t, srcDB, tgtDB, "tab_3")
	checkRowCount(t, srcDB, tgtDB, "tab_4")
	checkRowCount(t, srcDB, tgtDB, "tab_5")
	checkRowCount(t, srcDB, tgtDB, "tab_6")
	checkRowCount(t, srcDB, tgtDB, "tab_7")
	checkRowCount(t, srcDB, tgtDB, "tab_8")
	checkRowCount(t, srcDB, tgtDB, "tab_9")
	checkRowCount(t, srcDB, tgtDB, "tab_10")

	// Verify that views and materialized views exist.
	checkViewExists(t, tgtDB, "vw_tab_1_2")
	checkMaterializedViewExists(t, tgtDB, "mat_tab_1_2")

	// Check the existence of custom types.
	checkTypeExists(t, tgtDB, "blox_text")

	// Check triggers.
	checkTriggerExists(t, tgtDB, "tab_1", "price_trigger")

	// Validate a sample of specific data (using tab_1 and tab_2 as examples).
	checkSpecificData(t, srcDB, tgtDB, "tab_1", "id, name", "WHERE id = 1")
	checkSpecificData(t, srcDB, tgtDB, "tab_2", "id, name, tab_1_ref", "WHERE id = 1")
}

func checkRowCount(t *testing.T, srcDB, tgtDB *sql.DB, tableName string) {
	srcRow := srcDB.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName))
	tgtRow := tgtDB.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName))
	var srcCount, tgtCount int
	if err := srcRow.Scan(&srcCount); err != nil {
		t.Fatalf("failed to query source count for %s: %v", tableName, err)
	}
	if err := tgtRow.Scan(&tgtCount); err != nil {
		t.Fatalf("failed to query target count for %s: %v", tableName, err)
	}
	if srcCount != tgtCount {
		t.Fatalf("data mismatch in %s! source count: %d, target count: %d", tableName, srcCount, tgtCount)
	}
}

func checkViewExists(t *testing.T, db *sql.DB, viewName string) {
	var exists bool
	query := fmt.Sprintf("SELECT EXISTS (SELECT 1 FROM pg_views WHERE viewname = '%s')", viewName)
	if err := db.QueryRow(query).Scan(&exists); err != nil || !exists {
		t.Fatalf("view %s does not exist in the target database", viewName)
	}
}

func checkMaterializedViewExists(t *testing.T, db *sql.DB, matViewName string) {
	var exists bool
	query := fmt.Sprintf("SELECT EXISTS (SELECT 1 FROM pg_matviews WHERE matviewname = '%s')", matViewName)
	if err := db.QueryRow(query).Scan(&exists); err != nil || !exists {
		t.Fatalf("materialized view %s does not exist in the target database", matViewName)
	}
}

func checkTypeExists(t *testing.T, db *sql.DB, typeName string) {
	var exists bool
	query := fmt.Sprintf("SELECT EXISTS (SELECT 1 FROM pg_type WHERE typname = '%s')", typeName)
	if err := db.QueryRow(query).Scan(&exists); err != nil || !exists {
		t.Fatalf("type %s does not exist in the target database", typeName)
	}
}

func checkTriggerExists(t *testing.T, db *sql.DB, tableName, triggerName string) {
	var exists bool
	query := fmt.Sprintf("SELECT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname = '%s' AND tgrelid = '%s'::regclass)", triggerName, tableName)
	if err := db.QueryRow(query).Scan(&exists); err != nil || !exists {
		t.Fatalf("trigger %s on table %s does not exist in the target database", triggerName, tableName)
	}
}

func checkSpecificData(t *testing.T, srcDB, tgtDB *sql.DB, tableName, columns, condition string) {
	srcQuery := fmt.Sprintf("SELECT %s FROM %s %s", columns, tableName, condition)
	tgtQuery := fmt.Sprintf("SELECT %s FROM %s %s", columns, tableName, condition)

	// Run queries on both source and target databases.
	srcRow := srcDB.QueryRow(srcQuery)
	tgtRow := tgtDB.QueryRow(tgtQuery)

	// Determine the number of columns based on the columns parameter.
	columnNames := strings.Split(columns, ",")
	columnCount := len(columnNames)

	// Prepare slices to hold values for each column.
	srcValues := make([]interface{}, columnCount)
	tgtValues := make([]interface{}, columnCount)
	for i := range srcValues {
		var srcVal, tgtVal string
		srcValues[i] = &srcVal
		tgtValues[i] = &tgtVal
	}

	// Scan values into the prepared slices.
	if err := srcRow.Scan(srcValues...); err != nil {
		t.Fatalf("failed to query specific data from source: %v", err)
	}
	if err := tgtRow.Scan(tgtValues...); err != nil {
		t.Fatalf("failed to query specific data from target: %v", err)
	}

	// Compare each value between source and target.
	for i := 0; i < columnCount; i++ {
		if *srcValues[i].(*string) != *tgtValues[i].(*string) {
			t.Fatalf("data mismatch in %s! column: %s, source: %s, target: %s", tableName, columnNames[i], *srcValues[i].(*string), *tgtValues[i].(*string))
		}
	}
}

func changeUserInfo(dsn string, username string, password string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		panic(err)
	}
	u.User = url.UserPassword(username, password)
	return u.String()
}
