package dsnexec

import (
	"context"
	"database/sql/driver"
	"fmt"
	"reflect"
	"testing"

	"github.com/infobloxopen/db-controller/dsnexec/pkg/tdb"
)

func TestHandler_UpdateDSN(t *testing.T) {
	type fields struct {
		config Config
	}
	type args struct {
		path    string
		content string
	}
	tests := []struct {
		name              string
		fields            fields
		args              args
		wantErr           bool
		expectedExecCalls []tdb.ExecArgs
	}{
		// TODO: Add test cases.
		{
			name: "valid",
			fields: fields{
				config: Config{
					Sources: map[string]DBConnInfo{
						"test": {
							Driver: "postgres",
							DSN:    "postgres://user:pass@localhost:5432/dbname?sslmode=disable",
						},
					},
					Commands: []Command{
						{
							CommandStr: "select 1",
						},
					},
				},
			},
			expectedExecCalls: []tdb.ExecArgs{
				{
					Query: "select 1",
					Args:  []driver.Value{},
				},
			},
		},
		{
			name: "valid with args",
			fields: fields{
				config: Config{
					Sources: map[string]DBConnInfo{
						"test": {
							Driver: "postgres",
							DSN:    "postgres://user:pass@localhost:5432/dbname?sslmode=disable",
						},
					},
					Commands: []Command{
						{
							CommandStr: "select 1",
							Args: []string{
								"{{ .test.host }}",
								"int64:{{ .test.port }}",
							},
						},
					},
				},
			},
			expectedExecCalls: []tdb.ExecArgs{
				{
					Query: "select 1",
					Args: []driver.Value{
						"localhost",
						int64(5432),
					},
				},
			},
		},

		{
			name: "valid with raw dsn",
			fields: fields{
				config: Config{
					Sources: map[string]DBConnInfo{
						"test": {
							Driver: "postgres",
							DSN:    "postgres://user:pass@localhost:5432/dbname?sslmode=disable",
						},
					},
					Commands: []Command{
						{
							CommandStr: "select 1",
							Args: []string{
								"{{ .test.raw_dsn }}",
							},
						},
					},
				},
			},
			expectedExecCalls: []tdb.ExecArgs{
				{
					Query: "select 1",
					Args: []driver.Value{
						"postgres://user:pass@localhost:5432/dbname?sslmode=disable",
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &Handler{
				config: tt.fields.config,
			}
			// setup the destination DSN to use the test driver
			w.config.Destination = DBConnInfo{
				Driver: "tdb",
				DSN:    fmt.Sprintf("tdb://%s", t.Name()),
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// register the test case and get a function that returns the db calls
			execCallsFn := tdb.RegisterTestCase(t, ctx, w.config.Destination.DSN)
			if err := w.UpdateDSN(tt.args.path, tt.args.content); (err != nil) != tt.wantErr {
				t.Errorf("Handler.UpdateDSN() error = %v, wantErr %v", err, tt.wantErr)
			}

			// fetch the db calls and compare
			gotExecCalls := execCallsFn()
			if !reflect.DeepEqual(gotExecCalls, tt.expectedExecCalls) {
				t.Errorf("Handler.UpdateDSN() gotExecCalls = %v, expectedExecCalls %v", gotExecCalls, tt.expectedExecCalls)
			}
		})
	}
}

func Test_cast(t *testing.T) {
	type args struct {
		s string
	}
	tests := []struct {
		name    string
		args    args
		want    interface{}
		wantErr bool
	}{
		{
			name: "int64",
			args: args{
				s: "int64:123",
			},
			want:    int64(123),
			wantErr: false,
		},
		{
			name: "int32",
			args: args{
				s: "int32:123",
			},
			want:    int32(123),
			wantErr: false,
		},
		{
			name: "int16",
			args: args{
				s: "int16:123",
			},
			want:    int16(123),
			wantErr: false,
		},
		{
			name: "int8",
			args: args{
				s: "int8:123",
			},
			want:    int8(123),
			wantErr: false,
		},
		{
			name: "int",
			args: args{
				s: "int:123",
			},
			want:    int(123),
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := cast(tt.args.s)
			if (err != nil) != tt.wantErr {
				t.Errorf("cast() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("cast() = %v, want %v", got, tt.want)
			}
		})
	}
}
