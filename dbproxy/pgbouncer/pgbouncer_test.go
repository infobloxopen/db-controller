package pgbouncer

import "testing"

func TestParseURI(t *testing.T) {
	tests := []struct {
		name    string
		dsn     string
		wantErr bool
	}{
		{
			name:    "invalid scheme",
			dsn:     "invalid://localhost:5432",
			wantErr: true,
		},
		{
			name:    "invalid port",
			dsn:     "postgres://localhost:invalid",
			wantErr: true,
		},
		{
			name:    "valid",
			dsn:     "postgres://localhost:5432",
			wantErr: false,
		},
		{
			name:    "valid with database",
			dsn:     "postgres://localhost:5432/dbname",
			wantErr: false,
		},
		{
			name:    "valid with sslmode",
			dsn:     "postgres://localhost:5432/dbname?sslmode=disable",
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseURI(tt.dsn)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseURI() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}
