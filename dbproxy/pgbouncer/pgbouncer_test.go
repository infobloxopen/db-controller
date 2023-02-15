package pgbouncer

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseDBCredentials(t *testing.T) {
	type args struct {
		path string
	}
	tests := []struct {
		name      string
		args      args
		want      *DBCredential
		wantErr   bool
		errString string
	}{
		{
			name: "empty",
			args: args{
				path: "",
			},
			wantErr: true,
		},
		{
			name: "database=foo",
			args: args{
				path: "database=foo",
			},
			wantErr:   true,
			errString: errHostNotFound.Error(),
		},
		{
			name: " database=foo user=bar password=baz port=1111 dbname=d",
			args: args{
				path: " database=foo user=bar password=baz port=1111 dbname=d",
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseDBCredentials(tt.args.path)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseDBCredentials() error = nil, wantErr = true")
					return
				}
				errString := err.Error()
				if errString != "" && !strings.Contains(err.Error(), errString) {
					t.Errorf("ParseDBCredentials() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Errorf("ParseDBCredentials() error = %v, wantErr false", err)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseDBCredentials() = %v, want %v", got, tt.want)
			}
		})
	}
}
