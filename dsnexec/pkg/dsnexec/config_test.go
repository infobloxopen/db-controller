package dsnexec

import (
	"strings"
	"testing"
)

func TestConfig_Validate(t *testing.T) {
	type fields struct {
		Sources     map[string]DBConnInfo
		Destination DBConnInfo
		Commands    []Command
	}
	tests := []struct {
		name      string
		fields    fields
		wantErr   bool
		errString string
	}{
		{
			name: "valid config",
			fields: fields{
				Sources: map[string]DBConnInfo{
					"test": {
						Driver: "postgres",
						DSN:    "postgres://user:pass@host:port/dbname?sslmode=disable",
					},
				},
				Destination: DBConnInfo{
					Driver: "postgres",
					DSN:    "postgres://user:pass@host:port/dbname?sslmode=disable",
				},
				Commands: []Command{
					{
						Command: "SELECT 1",
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Config{
				Sources:     tt.fields.Sources,
				Destination: tt.fields.Destination,
				Commands:    tt.fields.Commands,
			}
			if err := c.Validate(); (err != nil) != tt.wantErr {
				t.Errorf("Config.Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			} else if err != nil && tt.errString != "" {
				if !strings.Contains(err.Error(), tt.errString) {
					t.Errorf("Config.Validate() error = %v, wantErr %v", err, tt.errString)
					return
				}
			}
		})
	}
}
