package hostparams

import (
	"reflect"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
	"github.com/spf13/viper"
)

func TestHostParams_Hash(t *testing.T) {
	type fields struct {
		Engine                          string
		Shape                           string
		InstanceClass                   string
		MinStorageGB                    int
		EngineVersion                   string
		MasterUsername                  string
		SkipFinalSnapshotBeforeDeletion bool
		PubliclyAccessible              bool
		EnableIAMDatabaseAuthentication bool
		DeletionPolicy                  xpv1.DeletionPolicy
		Port                            int64
		isDefaultEngine                 bool
		isDefaultShape                  bool
		isDefaultStorage                bool
		isDefaultVersion                bool
	}
	tests := []struct {
		name   string
		fields fields
		want   string
	}{
		{name: "test_Execute_ok", want: "d391a72a",
			fields: fields{Engine: "postgres",
				Shape:         "db.t4g.medium",
				InstanceClass: "db.t4g.medium",
				MinStorageGB:  20,
				EngineVersion: "12.11",
			},
		},
		{name: "test_Execute_engine_change", want: "0cd64830",
			fields: fields{Engine: "postgres",
				Shape:         "db.t4g.medium",
				InstanceClass: "db.t4g.medium",
				MinStorageGB:  20,
				EngineVersion: "13.11",
			},
		},
		{name: "test_Execute_shape_change", want: "ef75846d",
			fields: fields{Engine: "postgres",
				Shape:         "db.t2.medium",
				InstanceClass: "db.t2.medium",
				MinStorageGB:  20,
				EngineVersion: "12.11",
			},
		},
		{name: "test_Execute_storage_change", want: "d391a72a",
			fields: fields{Engine: "postgres",
				Shape:         "db.t4g.medium",
				InstanceClass: "db.t4g.medium",
				MinStorageGB:  21,
				EngineVersion: "12.11",
			},
		},
		{name: "test_Execute_aurora", want: "362c4cd6",
			fields: fields{Engine: "aurora-postgresql",
				Shape:         "db.t4g.medium!io1",
				InstanceClass: "db.t4g.medium",
				MinStorageGB:  20,
				EngineVersion: "12.11",
			},
		},
		{name: "test_Execute_storage_change_aurora", want: "362c4cd6",
			fields: fields{Engine: "aurora-postgresql",
				Shape:         "db.t4g.medium",
				InstanceClass: "db.t4g.medium",
				MinStorageGB:  20000,
				EngineVersion: "12.11",
			},
		},
		{name: "test_Execute_postgres_15.7_tg4.medium", want: "85aebd71",
			fields: fields{Engine: "postgres",
				Shape:         "db.t4g.medium",
				InstanceClass: "db.t4g.medium",
				MinStorageGB:  20000,
				EngineVersion: "15.7",
			},
		},
		{name: "test_Execute_postgres_15.6_tg4.medium", want: "1c34bd73",
			fields: fields{Engine: "postgres",
				Shape:         "db.t4g.medium",
				InstanceClass: "db.t4g.medium",
				MinStorageGB:  20000,
				EngineVersion: "15.6",
			},
		},
		{name: "test_Execute_postgres_15.5_tg4.medium", want: "1d9fb876",
			fields: fields{Engine: "postgres",
				Shape:         "db.t4g.medium",
				InstanceClass: "db.t4g.medium",
				MinStorageGB:  20000,
				EngineVersion: "15.5",
			},
		},
		{name: "test_Execute_postgres_15.3_tg4.medium", want: "1ec9b27c",
			fields: fields{Engine: "postgres",
				Shape:         "db.t4g.medium",
				InstanceClass: "db.t4g.medium",
				MinStorageGB:  20000,
				EngineVersion: "15.3",
			},
		}, {name: "test_Execute_postgres_15_tg4.medium", want: "416e183c",
			fields: fields{Engine: "postgres",
				Shape:         "db.t4g.medium",
				InstanceClass: "db.t4g.medium",
				MinStorageGB:  20000,
				EngineVersion: "15",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &HostParams{
				Type:                            tt.fields.Engine,
				Shape:                           tt.fields.Shape,
				InstanceClass:                   tt.fields.InstanceClass,
				MinStorageGB:                    tt.fields.MinStorageGB,
				DBVersion:                       tt.fields.EngineVersion,
				MasterUsername:                  tt.fields.MasterUsername,
				SkipFinalSnapshotBeforeDeletion: tt.fields.SkipFinalSnapshotBeforeDeletion,
				PubliclyAccessible:              tt.fields.PubliclyAccessible,
				EnableIAMDatabaseAuthentication: tt.fields.EnableIAMDatabaseAuthentication,
				DeletionPolicy:                  tt.fields.DeletionPolicy,
				Port:                            tt.fields.Port,
				isDefaultEngine:                 tt.fields.isDefaultEngine,
				isDefaultShape:                  tt.fields.isDefaultShape,
				isDefaultStorage:                tt.fields.isDefaultStorage,
				IsDefaultVersion:                tt.fields.isDefaultVersion,
			}
			if got := p.Hash(); got != tt.want {
				t.Errorf("HostParams.Hash() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHostParams_IsUpgradeRequested(t *testing.T) {
	type args struct {
		config           *viper.Viper
		dbClaim          *persistancev1.DatabaseClaim
		activeHostParams *HostParams
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "no-change-with-default", want: false,
			args: args{
				config:  testViper,
				dbClaim: &persistancev1.DatabaseClaim{Spec: persistancev1.DatabaseClaimSpec{}},
				activeHostParams: &HostParams{Type: "postgres",
					Shape:        "db.t4g.medium",
					MinStorageGB: 20,
					DBVersion:    "12.11",
				},
			},
		},
		{
			name: "shape-different", want: true,
			args: args{
				config: testViper,
				dbClaim: &persistancev1.DatabaseClaim{Spec: persistancev1.DatabaseClaimSpec{
					Type:         "postgres",
					Shape:        "db.t4g.medium",
					MinStorageGB: 20,
					DBVersion:    "12.11",
				}},
				activeHostParams: &HostParams{Type: "postgres",
					Shape:         "db.t2.small",
					InstanceClass: "db.t2.small",
					MinStorageGB:  200,
					DBVersion:     "12.11",
				},
			},
		},
		{
			name: "shape-different-default-shape", want: false,
			args: args{
				config: testViper,
				dbClaim: &persistancev1.DatabaseClaim{Spec: persistancev1.DatabaseClaimSpec{
					Type:         "postgres",
					MinStorageGB: 20,
					DBVersion:    "12.11",
				}},
				activeHostParams: &HostParams{Type: "postgres",
					Shape:         "db.t4g.medium",
					InstanceClass: "db.t4g.medium",
					MinStorageGB:  200,
					DBVersion:     "12.11",
				},
			},
		},
		{
			name: "version-different", want: true,
			args: args{
				config: testViper,
				dbClaim: &persistancev1.DatabaseClaim{Spec: persistancev1.DatabaseClaimSpec{
					Type:         "postgres",
					Shape:        "db.t4g.medium",
					MinStorageGB: 20,
					DBVersion:    "12.11",
				}},
				activeHostParams: &HostParams{Type: "postgres",
					Shape:         "db.t4g.medium",
					InstanceClass: "db.t4g.medium",
					MinStorageGB:  200,
					DBVersion:     "13.11",
				},
			},
		},
		{
			name: "version-different-default-version", want: false,
			args: args{
				config:  testViper,
				dbClaim: &persistancev1.DatabaseClaim{Spec: persistancev1.DatabaseClaimSpec{}},
				activeHostParams: &HostParams{Type: "postgres",
					Shape:         "db.t4g.medium",
					InstanceClass: "db.t4g.medium",
					MinStorageGB:  200,
					DBVersion:     "13.11",
				},
			},
		},
		{
			name: "engine-different", want: true,
			args: args{
				config: testViper,
				dbClaim: &persistancev1.DatabaseClaim{Spec: persistancev1.DatabaseClaimSpec{
					Type:  "aurora-postgresql",
					Shape: "db.t4g.different",
				}},
				activeHostParams: &HostParams{Type: "postgres",
					Shape:         "db.t4g.medium",
					InstanceClass: "db.t4g.medium",
					MinStorageGB:  200,
					DBVersion:     "13.11",
				},
			},
		},
		{
			name: "storage-different-postgres", want: false,
			args: args{
				config: testViper,
				dbClaim: &persistancev1.DatabaseClaim{Spec: persistancev1.DatabaseClaimSpec{
					Type:         "postgres",
					Shape:        "db.t4g.medium",
					MinStorageGB: 20,
				}},
				activeHostParams: &HostParams{Type: "postgres",
					Shape:         "db.t4g.medium",
					InstanceClass: "db.t4g.medium",
					MinStorageGB:  200,
					DBVersion:     "13.11",
				},
			},
		},
		{
			name: "storage-different-aurora", want: false,
			args: args{
				config: testViper,
				dbClaim: &persistancev1.DatabaseClaim{Spec: persistancev1.DatabaseClaimSpec{
					Type:         "aurora-postgresql",
					Shape:        "db.t4g.medium",
					MinStorageGB: 20,
				}},
				activeHostParams: &HostParams{Type: "aurora-postgresql",
					Shape:         "db.t4g.medium",
					InstanceClass: "db.t4g.medium",
					MinStorageGB:  200,
					DBVersion:     "13.11",
				},
			},
		},
		{
			name: "storage-different-aurora-with-io-shape", want: false,
			args: args{
				config: testViper,
				dbClaim: &persistancev1.DatabaseClaim{Spec: persistancev1.DatabaseClaimSpec{
					Type:         "aurora-postgresql",
					Shape:        "db.t4g.medium!io1",
					MinStorageGB: 20,
				}},
				activeHostParams: &HostParams{Type: "aurora-postgresql",
					Shape:         "db.t4g.medium!io1",
					InstanceClass: "db.t4g.medium",
					MinStorageGB:  200,
					DBVersion:     "13.11",
				},
			},
		},
		{
			name: "dbversion-empty-must-read-status.dbversion", want: false,
			args: args{
				config: testViper,
				dbClaim: &persistancev1.DatabaseClaim{
					Spec: persistancev1.DatabaseClaimSpec{
						Type:         "aurora-postgresql",
						Shape:        "db.t4g.medium!io1",
						MinStorageGB: 20,
					},
					Status: persistancev1.DatabaseClaimStatus{
						ActiveDB: persistancev1.Status{
							DBVersion: "15.7",
						},
					},
				},
				activeHostParams: &HostParams{Type: "aurora-postgresql",
					Shape:         "db.t4g.medium!io1",
					InstanceClass: "db.t4g.medium",
					MinStorageGB:  200,
					DBVersion:     "15.7",
				},
			},
		},
		{
			name: "dbversion-empty-must-read-status.dbversion", want: false,
			args: args{
				config: testViper,
				dbClaim: &persistancev1.DatabaseClaim{
					Spec: persistancev1.DatabaseClaimSpec{
						Type:         "aurora-postgresql",
						Shape:        "db.t4g.medium!io1",
						MinStorageGB: 20,
					},
					Status: persistancev1.DatabaseClaimStatus{
						ActiveDB: persistancev1.Status{
							DBVersion: "",
						},
					},
				},
				activeHostParams: &HostParams{Type: "aurora-postgresql",
					Shape:         "db.t4g.medium!io1",
					InstanceClass: "db.t4g.medium",
					MinStorageGB:  200,
					DBVersion:     "15.3",
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hp, _ := New(tt.args.config, tt.args.dbClaim)
			if got := hp.IsUpgradeRequested(tt.args.activeHostParams); got != tt.want {
				t.Errorf("IsUpgradeRequested() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetActiveHostParams(t *testing.T) {
	type args struct {
		dbClaim *persistancev1.DatabaseClaim
	}
	tests := []struct {
		name string
		args args
		want *HostParams
	}{
		{name: "ok",
			args: args{dbClaim: &persistancev1.DatabaseClaim{
				Spec: persistancev1.DatabaseClaimSpec{},
				Status: persistancev1.DatabaseClaimStatus{
					ActiveDB: persistancev1.Status{
						Type:         "aurora-postgresql",
						DBVersion:    "12.11",
						Shape:        "db.t4g.medium",
						MinStorageGB: 20,
					},
					NewDB: persistancev1.Status{
						DbState: persistancev1.InProgress,
					},
					MigrationState: "something in progress",
				},
			},
			},
			want: &HostParams{Type: "aurora-postgresql",
				Shape:         "db.t4g.medium",
				InstanceClass: "db.t4g.medium",
				MinStorageGB:  20,
				DBVersion:     "12.11",
			},
		},
		{name: "ok - with instance class",
			args: args{dbClaim: &persistancev1.DatabaseClaim{
				Spec: persistancev1.DatabaseClaimSpec{},
				Status: persistancev1.DatabaseClaimStatus{
					ActiveDB: persistancev1.Status{
						Type:         "aurora-postgresql",
						DBVersion:    "12.11",
						Shape:        "db.t4g.medium",
						MinStorageGB: 20,
					},
					NewDB: persistancev1.Status{
						DbState: persistancev1.InProgress,
					},
					MigrationState: "something in progress",
				},
			},
			},
			want: &HostParams{Type: "aurora-postgresql",
				Shape:         "db.t4g.medium",
				InstanceClass: "db.t4g.medium",
				MinStorageGB:  20,
				DBVersion:     "12.11",
			},
		},
		{name: "ok - with instance class and storage type",
			args: args{dbClaim: &persistancev1.DatabaseClaim{
				Spec: persistancev1.DatabaseClaimSpec{},
				Status: persistancev1.DatabaseClaimStatus{
					ActiveDB: persistancev1.Status{
						Type:         "aurora-postgresql",
						DBVersion:    "12.11",
						Shape:        "db.t4g.medium!io1",
						MinStorageGB: 20,
					},
					NewDB: persistancev1.Status{
						DbState: persistancev1.InProgress,
					},
					MigrationState: "something in progress",
				},
			},
			},
			want: &HostParams{Type: "aurora-postgresql",
				Shape:         "db.t4g.medium!io1",
				InstanceClass: "db.t4g.medium",
				MinStorageGB:  20,
				DBVersion:     "12.11",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetActiveHostParams(tt.args.dbClaim); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetActiveHostParams() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNew(t *testing.T) {
	type args struct {
		config  *viper.Viper
		dbClaim *persistancev1.DatabaseClaim
	}
	tests := []struct {
		name    string
		args    args
		want    *HostParams
		wantErr bool
	}{
		{
			name: "use_no_default_ok",
			args: args{
				config: testViper,
				dbClaim: &persistancev1.DatabaseClaim{Spec: persistancev1.DatabaseClaimSpec{
					Type:         "aurora-postgresql",
					DBVersion:    "12.11",
					Shape:        "db.t4g.medium",
					MinStorageGB: 20,
				}},
			},
			want: &HostParams{Type: "aurora-postgresql",
				Shape:         "db.t4g.medium",
				MinStorageGB:  20,
				DBVersion:     "12.11",
				InstanceClass: "db.t4g.medium",
				StorageType:   "aurora",
			},
			wantErr: false,
		},
		{
			name: "aurora_with_io_ok",
			args: args{
				config: testViper,
				dbClaim: &persistancev1.DatabaseClaim{Spec: persistancev1.DatabaseClaimSpec{
					Type:         "aurora-postgresql",
					DBVersion:    "12.11",
					Shape:        "db.t4g.medium!io1",
					MinStorageGB: 20,
				}},
			},
			want: &HostParams{Type: "aurora-postgresql",
				Shape:         "db.t4g.medium!io1",
				MinStorageGB:  20,
				DBVersion:     "12.11",
				InstanceClass: "db.t4g.medium",
				StorageType:   "aurora-iopt1",
			},
			wantErr: false,
		},
		{
			name: "postgres_ok",
			args: args{
				config: testViper,
				dbClaim: &persistancev1.DatabaseClaim{Spec: persistancev1.DatabaseClaimSpec{
					Type:         "postgres",
					DBVersion:    "12.11",
					Shape:        "db.t4g.large",
					MinStorageGB: 20,
				}},
			},
			want: &HostParams{Type: "postgres",
				Shape:         "db.t4g.large",
				MinStorageGB:  20,
				DBVersion:     "12.11",
				InstanceClass: "db.t4g.large",
				StorageType:   "gp3",
			},
			wantErr: false,
		},
		{
			name: "aurora_with_io_not_ok",
			args: args{
				config: testViper,
				dbClaim: &persistancev1.DatabaseClaim{Spec: persistancev1.DatabaseClaimSpec{
					Type:         "aurora-postgresql",
					DBVersion:    "12.11",
					Shape:        "db.t4g.medium!xxx",
					MinStorageGB: 20,
				}},
			},
			want:    &HostParams{},
			wantErr: true,
		},

		{
			name: "use_default_ok",
			args: args{
				config:  testViper,
				dbClaim: &persistancev1.DatabaseClaim{Spec: persistancev1.DatabaseClaimSpec{}},
			},
			want: &HostParams{Type: "postgres",
				Shape:         "db.t4g.medium",
				InstanceClass: "db.t4g.medium",
				MinStorageGB:  42,
				DBVersion:     "15",
			},
			wantErr: false,
		},
		{
			name: "maxStorage-less",
			args: args{
				config: testViper,
				dbClaim: &persistancev1.DatabaseClaim{Spec: persistancev1.DatabaseClaimSpec{
					Type:         "postgres",
					DBVersion:    "12.11",
					Shape:        "db.t4g.medium!xxx",
					MinStorageGB: 20,
					MaxStorageGB: 10,
				}},
			},
			want:    &HostParams{},
			wantErr: true,
		}, {
			name: "maxStorage-reduced",
			args: args{
				config: testViper,
				dbClaim: &persistancev1.DatabaseClaim{Spec: persistancev1.DatabaseClaimSpec{
					Type:         "postgres",
					DBVersion:    "12.11",
					Shape:        "db.t4g.medium!xxx",
					MinStorageGB: 20,
					MaxStorageGB: 30,
				},
					Status: persistancev1.DatabaseClaimStatus{
						ActiveDB: persistancev1.Status{
							MaxStorageGB: 40,
						},
					},
				},
			},
			want:    &HostParams{},
			wantErr: true,
		}, {
			name: "maxStorage_increased",
			args: args{
				config: testViper,
				dbClaim: &persistancev1.DatabaseClaim{Spec: persistancev1.DatabaseClaimSpec{
					Type:         "postgres",
					DBVersion:    "12.11",
					Shape:        "db.t4g.medium",
					MinStorageGB: 20,
					MaxStorageGB: 40,
				}, Status: persistancev1.DatabaseClaimStatus{
					ActiveDB: persistancev1.Status{
						MaxStorageGB: 30,
					},
				}},
			},
			want: &HostParams{Type: "postgres",
				Shape:         "db.t4g.medium",
				MinStorageGB:  20,
				MaxStorageGB:  40,
				DBVersion:     "12.11",
				InstanceClass: "db.t4g.medium",
				StorageType:   "gp3",
				Port:          5432,
			},
			wantErr: false,
		}, {
			name: "maxStorage-equel-minStorage",
			args: args{
				config: testViper,
				dbClaim: &persistancev1.DatabaseClaim{Spec: persistancev1.DatabaseClaimSpec{
					Type:         "postgres",
					DBVersion:    "12.11",
					Shape:        "db.t4g.medium!xxx",
					MinStorageGB: 20,
					MaxStorageGB: 20,
				},
					Status: persistancev1.DatabaseClaimStatus{
						ActiveDB: persistancev1.Status{
							MaxStorageGB: 0,
						},
					},
				},
			},
			want:    &HostParams{},
			wantErr: true,
		},
		{
			name: "maxStorage-not-specified-freash-dbc",
			args: args{
				config: testViper,
				dbClaim: &persistancev1.DatabaseClaim{Spec: persistancev1.DatabaseClaimSpec{
					Type:         "postgres",
					DBVersion:    "12.11",
					Shape:        "db.t4g.medium",
					MinStorageGB: 20,
				},
					Status: persistancev1.DatabaseClaimStatus{
						ActiveDB: persistancev1.Status{
							MaxStorageGB: 0,
						},
					},
				},
			},
			want: &HostParams{Type: "postgres",
				Shape:         "db.t4g.medium",
				MinStorageGB:  20,
				MaxStorageGB:  0,
				DBVersion:     "12.11",
				InstanceClass: "db.t4g.medium",
				StorageType:   "gp3",
				Port:          5432,
			},
			wantErr: false,
		},
		{
			name: "maxStorage-not-specified-after-enabled",
			args: args{
				config: testViper,
				dbClaim: &persistancev1.DatabaseClaim{Spec: persistancev1.DatabaseClaimSpec{
					Type:         "postgres",
					DBVersion:    "12.11",
					Shape:        "db.t4g.medium!xxx",
					MinStorageGB: 20,
				},
					Status: persistancev1.DatabaseClaimStatus{
						ActiveDB: persistancev1.Status{
							MaxStorageGB: 100,
						},
					},
				},
			},
			want:    &HostParams{},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := New(tt.args.config, tt.args.dbClaim)
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got.String() != tt.want.String() {
				t.Errorf("New() = %v, want %v", got, tt.want)
			}
		})
	}
}
func TestDeletionPolicy(t *testing.T) {
	type args struct {
		config  *viper.Viper
		dbClaim *persistancev1.DatabaseClaim
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "test_default_deletion_policy",
			args: args{
				config: testViper,
				dbClaim: &persistancev1.DatabaseClaim{Spec: persistancev1.DatabaseClaimSpec{
					Type:         "aurora-postgresql",
					DBVersion:    "12.11",
					Shape:        "db.t4g.medium",
					MinStorageGB: 20,
				}},
			},
			want: "Orphan",
		},
		{
			name: "test_delete_deletion_policy",
			args: args{
				config: testViper,
				dbClaim: &persistancev1.DatabaseClaim{Spec: persistancev1.DatabaseClaimSpec{
					Type:           "aurora-postgresql",
					DBVersion:      "12.11",
					Shape:          "db.t4g.medium",
					MinStorageGB:   20,
					DeletionPolicy: "Delete",
				}},
			},
			want: "Delete",
		},
		{
			name: "test_orphan_deletion_policy",
			args: args{
				config: testViper,
				dbClaim: &persistancev1.DatabaseClaim{Spec: persistancev1.DatabaseClaimSpec{
					Type:           "postgres",
					DBVersion:      "12.11",
					Shape:          "db.t4g.medium",
					MinStorageGB:   20,
					DeletionPolicy: "Orphan",
				}},
			},
			want: "Orphan",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := New(tt.args.config, tt.args.dbClaim)
			if string(got.DeletionPolicy) != tt.want {
				t.Errorf("DeletionPolicy = %v, want %v", got.DeletionPolicy, tt.want)
			}
		})
	}
}

func TestDBVersions(t *testing.T) {
	RegisterFailHandler(Fail)

	dbVersion1 := "15"

	Expect(strings.Split(dbVersion1, ".")[0]).To(Equal("15"))

	dbVersion2 := "15.2"

	Expect(strings.Split(dbVersion2, ".")[0]).To(Equal("15"))

	dbVersion3 := "15.1.2"

	Expect(strings.Split(dbVersion3, ".")[0]).To(Equal("15"))

	dbVersion4 := ""

	Expect(strings.Split(dbVersion4, ".")[0]).To(Equal(""))

}

var testViper *viper.Viper

func init() {
	testViper = viper.NewWithOptions(viper.KeyDelimiter(":"))
	testViper.SetConfigType("yaml")
	testViper.ReadConfig(strings.NewReader(`
defaultMasterPort: 5432
defaultMasterUsername: root
defaultSslMode: require
defaultMinStorageGB: 42
sample-connection:
storageType: gp3
defaultDeletionPolicy: orphan
`))

}

func TestNew_dbversion(t *testing.T) {

	var tests = []struct {
		name    string
		claim   *persistancev1.DatabaseClaim
		want    *HostParams
		wantErr bool
	}{
		{
			name: "default_version",
			claim: &persistancev1.DatabaseClaim{
				Status: persistancev1.DatabaseClaimStatus{
					ActiveDB: persistancev1.Status{},
				},
			},
			want: &HostParams{
				DBVersion: defaultMajorVersion,
			},
		},
		{
			name: "implied_version",
			claim: &persistancev1.DatabaseClaim{
				Status: persistancev1.DatabaseClaimStatus{
					ActiveDB: persistancev1.Status{
						DBVersion: "12.11",
					},
				},
			},
			want: &HostParams{
				DBVersion: "12.11",
			},
		},
		{
			name: "specified_version",
			claim: &persistancev1.DatabaseClaim{
				Spec: persistancev1.DatabaseClaimSpec{
					DBVersion: "12.11",
				},
			},
			want: &HostParams{
				DBVersion: "12.11",
			},
		},
		{
			name: "new_version_to_migrate_to",
			claim: &persistancev1.DatabaseClaim{
				Spec: persistancev1.DatabaseClaimSpec{
					DBVersion: "16",
				},
				Status: persistancev1.DatabaseClaimStatus{
					ActiveDB: persistancev1.Status{
						DBVersion: "12.11",
					},
				},
			},
			want: &HostParams{
				DBVersion: "16",
			},
		},
	}

	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			got, err := New(testViper, tt.claim)
			if (err != nil) != tt.wantErr {
				t.Fatalf("unexpected err: %v", err)
			}
			if got.DBVersion != tt.want.DBVersion {
				t.Errorf("DBVersion = %v, want %v", got.DBVersion, tt.want.DBVersion)
			}
		})
	}

}
