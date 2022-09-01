package pgctl

import (
	"reflect"
	"testing"
)

var (
	SourceDBAdminDsn = "postgresql://bjeevan:example@localhost:5433/pub?sslmode=disable"
	SourceDBUserDsn  = "postgresql://bjeevan:example@localhost:5433/pub?sslmode=disable"
	TargetDBAdminDsn = "postgresql://bjeevan:example@localhost:5434/sub?sslmode=disable"
	TargetDBUserDsn  = "postgresql://bjeevan:example@localhost:5434/sub?sslmode=disable"
)

func TestEndToEnd(t *testing.T) {

	// docker setup
	//s := &initial_state{config: tt.args}
	//for {
	//	s, err = s.Execute()
	//
	//}

}

func TestInitalState(t *testing.T) {

	type testcase struct {
		args          Config
		name          string
		expectedErr   bool
		expectedState StateEnum
	}
	tests := []testcase{
		{name: "empty", expectedErr: true, args: Config{}},
		{name: "target Admin empty", expectedErr: true,
			args: Config{
				SourceDBAdminDsn: SourceDBAdminDsn,
				SourceDBUserDsn:  SourceDBUserDsn,
				TargetDBUserDsn:  TargetDBUserDsn,
			},
		},
		{name: "target User empty", expectedErr: true,
			args: Config{
				SourceDBAdminDsn: SourceDBAdminDsn,
				SourceDBUserDsn:  SourceDBUserDsn,
				TargetDBAdminDsn: TargetDBAdminDsn,
			},
		},
		{name: "Source Admin empty", expectedErr: true,
			args: Config{
				SourceDBUserDsn:  SourceDBUserDsn,
				TargetDBAdminDsn: TargetDBAdminDsn,
				TargetDBUserDsn:  TargetDBUserDsn,
			},
		},
		{name: "Source User empty", expectedErr: true,
			args: Config{
				SourceDBAdminDsn: SourceDBAdminDsn,
				TargetDBAdminDsn: TargetDBAdminDsn,
				TargetDBUserDsn:  TargetDBUserDsn,
			},
		},
		{name: "ok", expectedErr: false, expectedState: S_ValidateConnection,
			args: Config{
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

func Test_validate_connection_state_Execute(t *testing.T) {
	type fields struct {
		config Config
	}
	tests := []struct {
		name    string
		fields  fields
		want    StateEnum
		wantErr bool
	}{
		{name: "ok", wantErr: false, want: S_CreatePublication,
			fields: fields{Config{
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
			if !reflect.DeepEqual(got.Id(), tt.want) {
				t.Errorf("validate_connection_state.Execute() = %v, want %v", got.Id(), tt.want)
			}
		})
	}
}
