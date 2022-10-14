package pgctl

import (
	"database/sql"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	SourceDBAdminDsn string
	SourceDBUserDsn  string
	TargetDBAdminDsn string
	TargetDBUserDsn  string
	ExportFilePath   = "/tmp/"
	repository       = "postgres"
	sourceVersion    = "10"
	targetVersion    = "12.3"
	sourcePort       = "5435"
	targetPort       = "5436"
	testDBNetwork    = "testDBNetwork"
)

type PgInfo struct {
	user     string
	password string
	db       string
	dbHost   string
	version  string
	port     string
	dialect  string
	dsn      string
}

var logger logr.Logger

func TestMain(m *testing.M) {
	//need to do this trick to avoid os.Exit bypassing defer logic
	//with this silly setup, defer is called in realTestMain before the exit is called in this func
	os.Exit(realTestMain(m))
}

func realTestMain(m *testing.M) int {

	var targetResource, sourceResource *dockertest.Resource

	opts := zap.Options{
		Development: true,
	}

	logger = zap.New(zap.UseFlagOptions(&opts))

	pool, err := dockertest.NewPool("")
	if err != nil {
		fmt.Println(err)
		return 1
	}

	//validate that no other network is lingering around from a prev test
	networks, err := pool.NetworksByName(testDBNetwork)
	if err != nil {
		fmt.Println(err)
		return 1
	}
	if len(networks) != 0 {
		fmt.Printf("Expected 0 but got %v networks\n", len(networks))
		return 1
	}

	network, err := pool.CreateNetwork(testDBNetwork)
	if err != nil {
		fmt.Println(err)
		return 1
	}
	defer pool.RemoveNetwork(network)

	pool.MaxWait = 300 * time.Second
	if err != nil {
		logger.Error(err, "Could not connect to docker")
		return 1
	}

	TargetDBAdminDsn, targetResource, err = setUpTargetDatabase(pool)
	defer pool.Purge(targetResource)
	if err != nil {
		fmt.Println(err)
		return 1
	}

	TargetDBUserDsn = TargetDBAdminDsn

	SourceDBAdminDsn, sourceResource, err = setUpSourceDatabase(pool)
	defer pool.Purge(sourceResource)
	if err != nil {
		fmt.Println(err)
		return 1
	}
	SourceDBUserDsn = fmt.Sprintf("postgres://appuser:secret@localhost:%s/pub?sslmode=disable", sourcePort)

	if err = setWalLevel(repository, sourceVersion, sourcePort); err != nil {
		fmt.Println(err)
		return 1
	}

	if err = loadSourceTestData(SourceDBAdminDsn); err != nil {
		fmt.Println(err)
		return 1
	}

	rc := m.Run()

	if err := pool.Purge(targetResource); err != nil {
		fmt.Println(err)
		rc = 2
	}
	if err := pool.Purge(sourceResource); err != nil {
		fmt.Println(err)
		rc = 3
	}
	if err := pool.RemoveNetwork(network); err != nil {
		fmt.Println(err)
		rc = 4
	}

	return rc

}

func setUpTargetDatabase(pool *dockertest.Pool) (string, *dockertest.Resource, error) {

	pgInfo := PgInfo{
		user:     "postgres",
		password: "secret",
		db:       "sub",
		dbHost:   "subHost",
		version:  targetVersion,
		port:     targetPort,
		dialect:  "postgres",
		dsn:      "postgres://%s:%s@localhost:%s/%s?sslmode=disable"}

	var (
		dsn      string
		resource *dockertest.Resource
		err      error
	)

	if dsn, resource, err = setUpDatabase(pool, &pgInfo); err != nil {
		return "", resource, err
	}

	return dsn, resource, nil
}

func setUpSourceDatabase(pool *dockertest.Pool) (string, *dockertest.Resource, error) {

	pgInfo := PgInfo{
		user:     "postgres",
		password: "secret",
		db:       "pub",
		dbHost:   "pubHost",
		version:  sourceVersion,
		port:     sourcePort,
		dialect:  "postgres",
		dsn:      "postgres://%s:%s@localhost:%s/%s?sslmode=disable"}

	var (
		dsn      string
		resource *dockertest.Resource
		err      error
	)

	if dsn, resource, err = setUpDatabase(pool, &pgInfo); err != nil {
		return "", resource, err
	}

	return dsn, resource, nil
}

func setUpDatabase(pool *dockertest.Pool, pgInfo *PgInfo) (string, *dockertest.Resource, error) {

	networks, err := pool.NetworksByName(testDBNetwork)
	if err != nil {
		return "", nil, err
	}

	if len(networks) != 1 {
		return "", nil, fmt.Errorf("Expected 1 but got %v networks", len(networks))
	}

	opts := dockertest.RunOptions{
		Repository: repository,
		Tag:        pgInfo.version,
		Hostname:   pgInfo.dbHost,
		NetworkID:  networks[0].Network.ID,
		Env: []string{
			"POSTGRES_USER=" + pgInfo.user,
			"POSTGRES_PASSWORD=" + pgInfo.password,
			"POSTGRES_DB=" + pgInfo.db,
		},
		ExposedPorts: []string{"5432"},
		PortBindings: map[docker.Port][]docker.PortBinding{
			"5432": {
				{HostIP: "0.0.0.0", HostPort: pgInfo.port},
			},
		},
	}

	resource, err := pool.RunWithOptions(&opts)
	if err != nil {
		logger.Error(err, "could not start resource")
		return "", resource, err
	}
	var dbc *sql.DB
	pgInfo.dsn = fmt.Sprintf(pgInfo.dsn, pgInfo.user, pgInfo.password, pgInfo.port, pgInfo.db)
	resource.Expire(300) // Tell docker to hard kill the container in 300 seconds

	if err = pool.Retry(func() error {
		logger.Info("Connecting to database", "with url", pgInfo.dsn)
		dbc, err = sql.Open(pgInfo.dialect, pgInfo.dsn)
		if err != nil {
			return err
		}
		return dbc.Ping()
	}); err != nil {
		return "", resource, fmt.Errorf("Could not connect to docker: %s", err)
	}
	return pgInfo.dsn, resource, nil
}

func loadSourceTestData(dsn string) error {
	_, err := Exec("psql", dsn, "-f", "./test/pgctl_test.sql", "-v", "end=50")
	if err != nil {
		return err
	}
	return nil
}

func setWalLevel(repo string, tag string, port string) error {
	_, err := Exec("./test/change_wal_level.sh", repo, tag, port)
	if err != nil {
		return err
	}
	return nil
}

func TestWrapper(t *testing.T) {
	testInitalState(t)
	test_validate_connection_state_Execute(t)
	test_create_publication_state_Execute(t)
	test_copy_schema_state_Execute(t)
	test_create_subscription_state_Execute(t)
	test_enable_subscription_state_Execute(t)
	test_cut_over_readiness_check_state_Execute(t)
	test_reset_target_sequence_state_Execute(t)
	//test_reroute_target_secret_state_Execute(t)
	//test_validate_migration_status_state_Execute(t)
	test_disable_source_access_state_Execute(t)
	test_disable_subscription_state_Execute(t)
	test_delete_subscription_state_Execute(t)
	test_delete_publication_state_Execute(t)
}
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
	defer func() {
		getSourceDbAdminDSNForCreateSubscription = oldGetSourceAdminDSN
	}()
	// This overrides a var getSourceDbAdminDSNForCreateSubscription to handle the special case of unit test
	// where 2 docker databases has to communicated using docker bridge network and needs the host name and port as
	// defined in docker.
	getSourceDbAdminDSNForCreateSubscription = func(c *Config) string {
		return "postgres://postgres:secret@pubHost:5432/pub?sslmode=disable"
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

func testInitalState(t *testing.T) {

	fmt.Println("in initial state")

	type testcase struct {
		args          Config
		name          string
		expectedErr   bool
		expectedState StateEnum
	}
	tests := []testcase{
		{name: "testInitalState_empty", expectedErr: true, args: Config{}},
		{name: "target Admin empty", expectedErr: true,
			args: Config{
				Log:              logger,
				SourceDBAdminDsn: SourceDBAdminDsn,
				SourceDBUserDsn:  SourceDBUserDsn,
				TargetDBUserDsn:  TargetDBUserDsn,
			},
		},
		{name: "testInitalState_target User empty", expectedErr: true,
			args: Config{
				Log:              logger,
				SourceDBAdminDsn: SourceDBAdminDsn,
				SourceDBUserDsn:  SourceDBUserDsn,
				TargetDBAdminDsn: TargetDBAdminDsn,
			},
		},
		{name: "testInitalState_Source Admin empty", expectedErr: true,
			args: Config{
				Log:              logger,
				SourceDBUserDsn:  SourceDBUserDsn,
				TargetDBAdminDsn: TargetDBAdminDsn,
				TargetDBUserDsn:  TargetDBUserDsn,
			},
		},
		{name: "testInitalState_Source User empty", expectedErr: true,
			args: Config{
				Log: logger,

				SourceDBAdminDsn: SourceDBAdminDsn,
				TargetDBAdminDsn: TargetDBAdminDsn,
				TargetDBUserDsn:  TargetDBUserDsn,
			},
		},
		{name: "testInitalState_ok", expectedErr: false, expectedState: S_ValidateConnection,
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

			next_state, err := s.Execute()
			if tt.expectedErr && err == nil {
				t.Fatalf("test case  %s: expected error got nil", tt.name)
			}
			if tt.expectedErr == false && err != nil {
				t.Fatalf("test case %s: expected no error, got %s", tt.name, err)
			}
			if next_state != nil {
				if tt.expectedState != next_state.Id() {
					t.Fatalf(" test case %s: expected %s, got %s", tt.name, tt.expectedState, next_state.Id())
				}
			}
		})
	}
}

func test_validate_connection_state_Execute(t *testing.T) {
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
				Log:              logger,
				SourceDBAdminDsn: "postgres://invalid:secret@localhost:%s/pub?sslmode=disable",
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
				t.Errorf("validate_connection_state.Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err != nil {
				return
			}
			if !reflect.DeepEqual(got.Id(), tt.want) {
				t.Errorf("validate_connection_state.Execute() = %v, want %v", got.Id(), tt.want)
			}
		})
	}
}

func test_create_publication_state_Execute(t *testing.T) {
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
				t.Errorf("create_publication_state.Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got.Id(), tt.want) {
				t.Errorf("create_publication_state.Execute() = %v, want %v", got.Id(), tt.want)
			}
		})
	}
}

func test_copy_schema_state_Execute(t *testing.T) {

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
				t.Errorf("copy_schema_state.Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got.Id(), tt.want) {
				t.Errorf("copy_schema_state.Execute() = %v, want %v", got.Id(), tt.want)
			}
		})
	}
}

func test_create_subscription_state_Execute(t *testing.T) {
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
				SourceDBAdminDsn: "postgres://postgres:secret@pubHost:5432/pub?sslmode=disable",
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
				t.Errorf("create_subscription_state.Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got.Id(), tt.want) {
				t.Errorf("create_subscription_state.Execute() = %v, want %v", got.Id(), tt.want)
			}
		})
	}
}

func test_enable_subscription_state_Execute(t *testing.T) {
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
				t.Errorf("enable_subscription_state.Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got.Id(), tt.want) {
				t.Errorf("enable_subscription_state.Execute() = %v, want %v", got.Id(), tt.want)
			}
		})
	}
}

func test_cut_over_readiness_check_state_Execute(t *testing.T) {
	type fields struct {
		config Config
	}
	tests := []struct {
		name    string
		fields  fields
		want    StateEnum
		wantErr bool
	}{
		{name: "_test_cut_over_readiness_check_state_Executeok", wantErr: false, want: S_Retry,
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
				t.Errorf("cut_over_readiness_check_state.Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got.Id(), tt.want) {
				t.Errorf("cut_over_readiness_check_state.Execute() = %v, want %v", got.Id(), tt.want)
			}
		})
	}
}

func test_reset_target_sequence_state_Execute(t *testing.T) {
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
				t.Errorf("reset_target_sequence_state.Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got.Id(), tt.want) {
				t.Errorf("reset_target_sequence_state.Execute() = %v, want %v", got.Id(), tt.want)
			}
		})
	}
}

func test_reroute_target_secret_state_Execute(t *testing.T) {
	type fields struct {
		config Config
	}
	tests := []struct {
		name    string
		fields  fields
		want    StateEnum
		wantErr bool
	}{
		{name: "test_reroute_target_secret_state_Execute_ok", wantErr: false, want: S_ValidateMigrationStatus,
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
				t.Errorf("reroute_target_secret_state.Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got.Id(), tt.want) {
				t.Errorf("reroute_target_secret_state.Execute() = %v, want %v", got.Id(), tt.want)
			}
		})
	}
}

func test_validate_migration_status_state_Execute(t *testing.T) {
	type fields struct {
		config Config
	}
	tests := []struct {
		name    string
		fields  fields
		want    StateEnum
		wantErr bool
	}{
		{name: "test_validate_migration_status_state_ok", wantErr: false, want: S_DisableSourceAccess,
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
			got, err := s.Execute()
			if (err != nil) != tt.wantErr {
				t.Errorf("validate_migration_status_state.Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got.Id(), tt.want) {
				t.Errorf("validate_migration_status_state.Execute() = %v, want %v", got.Id(), tt.want)
			}
		})
	}
}

func test_disable_source_access_state_Execute(t *testing.T) {
	type fields struct {
		config Config
	}
	tests := []struct {
		name    string
		fields  fields
		want    StateEnum
		wantErr bool
	}{
		{name: "test_disable_source_access_state_ok", wantErr: false, want: S_DisableSubscription,
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
				t.Errorf("disable_source_access_state.Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got.Id(), tt.want) {
				t.Errorf("disable_source_access_state.Execute() = %v, want %v", got.Id(), tt.want)
			}
		})
	}
}

func test_disable_subscription_state_Execute(t *testing.T) {
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
				t.Errorf("enable_subscription_state.Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got.Id(), tt.want) {
				t.Errorf("enable_subscription_state.Execute() = %v, want %v", got.Id(), tt.want)
			}
		})
	}
}

func test_delete_subscription_state_Execute(t *testing.T) {
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
				t.Errorf("delete_subscription_state.Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got.Id(), tt.want) {
				t.Errorf("delete_subscription_state.Execute() = %v, want %v", got.Id(), tt.want)
			}
		})
	}
}

func test_delete_publication_state_Execute(t *testing.T) {
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
				t.Errorf("delete_publication_state.Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got.Id(), tt.want) {
				t.Errorf("delete_publication_state.Execute() = %v, want %v", got.Id(), tt.want)
			}
		})
	}
}
