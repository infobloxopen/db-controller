package hostparams

import (
	"bytes"
	"reflect"
	"testing"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
	"github.com/spf13/viper"
)

func TestHostParams_Hash(t *testing.T) {
	type fields struct {
		Engine                          string
		Shape                           string
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
		{name: "test_Execute_ok", want: "1620c8de",
			fields: fields{Engine: "postgres",
				Shape:         "db.t4g.medium",
				MinStorageGB:  20,
				EngineVersion: "12.11",
			},
		},
		{name: "test_Execute_engine_change", want: "9993e156",
			fields: fields{Engine: "postgres",
				Shape:         "db.t4g.medium",
				MinStorageGB:  20,
				EngineVersion: "13.11",
			},
		},
		{name: "test_Execute_shape_change", want: "1e67c367",
			fields: fields{Engine: "postgres",
				Shape:         "db.t2.medium",
				MinStorageGB:  20,
				EngineVersion: "12.11",
			},
		},
		{name: "test_Execute_storage_change", want: "8fbac8dc",
			fields: fields{Engine: "postgres",
				Shape:         "db.t4g.medium",
				MinStorageGB:  21,
				EngineVersion: "12.11",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &HostParams{
				Engine:                          tt.fields.Engine,
				Shape:                           tt.fields.Shape,
				MinStorageGB:                    tt.fields.MinStorageGB,
				EngineVersion:                   tt.fields.EngineVersion,
				MasterUsername:                  tt.fields.MasterUsername,
				SkipFinalSnapshotBeforeDeletion: tt.fields.SkipFinalSnapshotBeforeDeletion,
				PubliclyAccessible:              tt.fields.PubliclyAccessible,
				EnableIAMDatabaseAuthentication: tt.fields.EnableIAMDatabaseAuthentication,
				DeletionPolicy:                  tt.fields.DeletionPolicy,
				Port:                            tt.fields.Port,
				isDefaultEngine:                 tt.fields.isDefaultEngine,
				isDefaultShape:                  tt.fields.isDefaultShape,
				isDefaultStorage:                tt.fields.isDefaultStorage,
				isDefaultVersion:                tt.fields.isDefaultVersion,
			}
			if got := p.Hash(); got != tt.want {
				t.Errorf("HostParams.Hash() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHostParams_IsUpgradeRequested(t *testing.T) {
	type fields struct {
		Engine                          string
		Shape                           string
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
	type args struct {
		np *HostParams
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   bool
	}{
		{name: "shape-different", want: true,
			fields: fields{Engine: "postgres",
				Shape:         "db.t4g.medium",
				MinStorageGB:  20,
				EngineVersion: "12.11",
			},
			args: args{np: &HostParams{Engine: "postgres",
				Shape:         "db.t2.small",
				MinStorageGB:  20,
				EngineVersion: "12.11",
			},
			},
		},
		{name: "shape-different-default-shape", want: false,
			fields: fields{Engine: "postgres",
				Shape:            "db.t4g.medium",
				MinStorageGB:     20,
				EngineVersion:    "12.11",
				isDefaultShape:   true,
				isDefaultStorage: false,
				isDefaultVersion: false,
			},
			args: args{np: &HostParams{Engine: "postgres",
				Shape:         "db.t2.small",
				MinStorageGB:  20,
				EngineVersion: "12.11",
			},
			},
		},
		{name: "version-different", want: true,
			fields: fields{Engine: "postgres",
				Shape:            "db.t4g.medium",
				MinStorageGB:     20,
				EngineVersion:    "12.11",
				isDefaultShape:   false,
				isDefaultStorage: false,
				isDefaultVersion: false,
			},
			args: args{np: &HostParams{Engine: "postgres",
				Shape:         "db.t4g.medium",
				MinStorageGB:  20,
				EngineVersion: "13.11",
			},
			},
		},
		{name: "version-different-default-version", want: false,
			fields: fields{Engine: "postgres",
				Shape:            "db.t4g.medium",
				MinStorageGB:     20,
				EngineVersion:    "12.11",
				isDefaultShape:   true,
				isDefaultStorage: false,
				isDefaultVersion: true,
			},
			args: args{np: &HostParams{Engine: "postgres",
				Shape:         "db.t4g.medium",
				MinStorageGB:  20,
				EngineVersion: "13.11",
			},
			},
		},
		{name: "engine-different", want: true,
			fields: fields{Engine: "postgres",
				Shape:         "db.t4g.medium",
				MinStorageGB:  20,
				EngineVersion: "12.11",
			},
			args: args{np: &HostParams{Engine: "aurora-postgresql",
				Shape:         "db.t4g.medium",
				MinStorageGB:  20,
				EngineVersion: "12.11",
			},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &HostParams{
				Engine:                          tt.fields.Engine,
				Shape:                           tt.fields.Shape,
				MinStorageGB:                    tt.fields.MinStorageGB,
				EngineVersion:                   tt.fields.EngineVersion,
				MasterUsername:                  tt.fields.MasterUsername,
				SkipFinalSnapshotBeforeDeletion: tt.fields.SkipFinalSnapshotBeforeDeletion,
				PubliclyAccessible:              tt.fields.PubliclyAccessible,
				EnableIAMDatabaseAuthentication: tt.fields.EnableIAMDatabaseAuthentication,
				DeletionPolicy:                  tt.fields.DeletionPolicy,
				Port:                            tt.fields.Port,
				isDefaultEngine:                 tt.fields.isDefaultEngine,
				isDefaultShape:                  tt.fields.isDefaultShape,
				isDefaultStorage:                tt.fields.isDefaultStorage,
				isDefaultVersion:                tt.fields.isDefaultVersion,
			}
			if got := p.IsUpgradeRequested(tt.args.np); got != tt.want {
				t.Errorf("HostParams.IsUpgradeRequested() = %v, want %v", got, tt.want)
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
						Status: "in-progress",
					},
					MigrationState: "something in progress",
				},
			},
			},
			want: &HostParams{Engine: "aurora-postgresql",
				Shape:         "db.t4g.medium",
				MinStorageGB:  20,
				EngineVersion: "12.11",
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

var testConfig = []byte(`defaultMasterPort: 5432
defaultMasterUsername: root
defaultSslMode: require
defaultEngineVersion: 13
defaultShape: xyz
defaultEngine: whocares
defaultMinStorageGB: 42
sample-connection:
`)

func TestNew(t *testing.T) {
	type args struct {
		config      *viper.Viper
		fragmentKey string
		dbClaim     *persistancev1.DatabaseClaim
	}
	tests := []struct {
		name    string
		args    args
		want    *HostParams
		wantErr bool
	}{
		{
			name: "fragmentKey_nil_use_no_default_ok",
			args: args{
				config:      NewConfig(testConfig),
				fragmentKey: "",
				dbClaim: &persistancev1.DatabaseClaim{Spec: persistancev1.DatabaseClaimSpec{
					Port:         "5432",
					Type:         "aurora-postgresql",
					DBVersion:    "12.11",
					Shape:        "db.t4g.medium",
					MinStorageGB: 20,
				}},
			},
			want: &HostParams{Engine: "aurora-postgresql",
				Shape:         "db.t4g.medium",
				MinStorageGB:  20,
				EngineVersion: "12.11",
			},
			wantErr: false,
		},
		{
			name: "fragmentKey_set_use_default_ok",
			args: args{
				config:      NewConfig(testConfig),
				fragmentKey: "sample-connection",
				dbClaim:     &persistancev1.DatabaseClaim{Spec: persistancev1.DatabaseClaimSpec{}},
			},
			want: &HostParams{Engine: "whocares",
				Shape:         "xyz",
				MinStorageGB:  42,
				EngineVersion: "13",
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := New(tt.args.config, tt.args.fragmentKey, tt.args.dbClaim)
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

func NewConfig(in []byte) *viper.Viper {
	c := viper.NewWithOptions(viper.KeyDelimiter(":"))
	c.SetConfigType("yaml")
	c.ReadConfig(bytes.NewBuffer(in))

	return c
}
