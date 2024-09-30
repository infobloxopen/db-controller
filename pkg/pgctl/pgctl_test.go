package pgctl

import (
	"cmp"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/infobloxopen/db-controller/internal/dockerdb"
	"github.com/lib/pq"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	SourceDBAdminDsn     string
	SourceDBUserDsn      string
	TargetDBAdminDsn     string
	TargetDBUserDsn      string
	dropSchemaDBAdminDsn string

	ExportFilePath = "/tmp/"
	repository     = "postgres"
	sourceVersion  = "13.14"
	targetVersion  = "15.6"
	sourcePort     = "15435"
	targetPort     = "15436"
	testDBNetwork  = "testDBNetwork"
)

type PgInfo struct {
	user     string
	password string
	db       string
	dbHost   string
	port     string
	version  string
	dialect  string
	dsn      string
}

var logger logr.Logger

func TestMain(m *testing.M) {
	//need to do this trick to avoid os.Exit bypassing defer logic
	//with this silly setup, defer is called in realTestMain before the exit is called in this func
	os.Exit(setupAndRunTests(m))
}

func setupAndRunTests(m *testing.M) int {
	opts := zap.Options{
		Development: true,
	}
	logger = zap.New(zap.UseFlagOptions(&opts))

	networkName := "pgctl"
	removeNetwork := dockerdb.StartNetwork(networkName)
	defer removeNetwork()

	// FIXME: randomly generate network name
	_, sourceDSN, sourceClose := dockerdb.Run(dockerdb.Config{
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

	_, targetDSN, targetClose := dockerdb.Run(dockerdb.Config{
		HostName:  "subHost",
		DockerTag: targetVersion,
		Database:  "sub",
		Username:  "targetAdmin",
		Password:  "targetSecret",
		Network:   networkName,
	})
	defer targetClose()

	_, dropSchemaDSN, dropSchemaClose := dockerdb.Run(dockerdb.Config{
		HostName:  "dropSchemaHost",
		DockerTag: targetVersion,
		Database:  "sub",
		Username:  "dropSchemaAdmin",
		Password:  "dropSchemaSecret",
		Network:   networkName,
	})
	defer dropSchemaClose()

	if err := loadTargetTestData(targetDSN); err != nil {
		panic(err)
	}

	if _, err := url.Parse(sourceDSN); err != nil {
		panic(err)
	}

	// Set a bunch of package variables. This should be done in the test setup
	TargetDBAdminDsn = targetDSN
	SourceDBAdminDsn = sourceDSN
	dropSchemaDBAdminDsn = dropSchemaDSN

	TargetDBUserDsn = changeUserInfo(TargetDBAdminDsn, "appuser_b", "secret")
	SourceDBUserDsn = changeUserInfo(SourceDBAdminDsn, "appuser_a", "secret")

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

func setWalLevel(repo string, tag string, port string) error {
	panic("nope, dont restart the container")
	// _, err := Exec("./test/change_wal_level.sh", repo, tag, port)
	// if err != nil {
	// 	return err
	// }
	// return nil
}

func TestStateTransitions(t *testing.T) {
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

// TODO: this is not used anywhere in the code, should it be removed?
func testEndToEnd(t *testing.T) {

	config := Config{
		Log:              logger,
		SourceDBAdminDsn: SourceDBAdminDsn,
		SourceDBUserDsn:  SourceDBUserDsn,
		TargetDBUserDsn:  TargetDBUserDsn,
		TargetDBAdminDsn: TargetDBAdminDsn,
		ExportFilePath:   ExportFilePath,
	}

	var (
		s   State
		err error
	)
	oldGetSourceAdminDSN := getSourceDbAdminDSNForCreateSubscription
	oldGrantSuper := grantSuperUserAccess
	oldRevokeSuper := revokeSuperUserAccess
	defer func() {
		getSourceDbAdminDSNForCreateSubscription = oldGetSourceAdminDSN
		grantSuperUserAccess = oldGrantSuper
		revokeSuperUserAccess = oldRevokeSuper
	}()
	// This overrides a var getSourceDbAdminDSNForCreateSubscription to handle the special case of unit test
	// where 2 docker databases has to communicate using docker bridge network and needs the host name and port as
	// defined in docker.
	getSourceDbAdminDSNForCreateSubscription = func(c *Config) string {
		return "postgres://sourceAdmin:sourceSecret@pubHost:5432/pub?sslmode=disable"
	}
	// FIXME: this does not test actual functionality, replace these tests to call [grant|revoke]SuperUserAccess
	grantSuperUserAccess = func(DBAdmin *sql.DB, role string, cloud string) error {
		_, err := DBAdmin.Exec(fmt.Sprintf("ALTER ROLE %s WITH SUPERUSER;", pq.QuoteIdentifier(role)))
		return err
	}
	revokeSuperUserAccess = func(DBAdmin *sql.DB, role string, cloud string) error {
		_, err := DBAdmin.Exec(fmt.Sprintf("ALTER ROLE %s WITH NOSUPERUSER;", pq.QuoteIdentifier(role)))
		return err
	}

	//s = &initial_state{config}
	s, err = GetReplicatorState("", config)
	if err != nil {
		t.Error(err)
	}

loop:
	for {
		next, err := s.Execute()
		if err != nil {
			t.Error(err)
			break
		}
		switch next.Id() {
		case S_Completed:
			fmt.Println("Completed FSM")
			break loop
		case S_Retry:
			fmt.Println("Retry called")
			time.Sleep(5 * time.Second)
		default:
			s = next
		}
	}
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

func changeUserInfo(dsn string, username string, password string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		panic(err)
	}
	u.User = url.UserPassword(username, password)
	return u.String()
}
