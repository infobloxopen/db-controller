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
			name: "host=foo.com user=bar password=baz port=1111 database=d",
			args: args{
				path: "host=foo user=bar password=baz port=1111 database=d",
			},
			wantErr: false,
			want: &DBCredential{
				Host:     "foo",
				DBName:   "d",
				User:     "bar",
				Password: "baz",
				Port:     1111,
			},
		},
		{
			name: "host=foo.com user=bar password=baz port=1111 dbname=d",
			args: args{
				path: "host=foo user=bar password=baz port=1111 dbname=d",
			},
			wantErr: false,
			want: &DBCredential{
				Host:     "foo",
				DBName:   "d",
				User:     "bar",
				Password: "baz",
				Port:     1111,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseDBCredentials(tt.args.path)
			if tt.wantErr {
				if err == nil {
					t.Errorf("parseDBCredentials() error = nil, wantErr = true")
					return
				}
				errString := err.Error()
				if errString != "" && !strings.Contains(err.Error(), errString) {
					t.Errorf("parseDBCredentials() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Errorf("parseDBCredentials() error = %v, wantErr false", err)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseDBCredentials() = %v, want %v", got, tt.want)
			}
		})
	}
}
