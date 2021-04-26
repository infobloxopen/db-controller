package rdsauth

import "testing"

func Test_parseRegion(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		want     string
		wantErr  bool
	}{
		{
			name:     "correct RDS endpoint, region us-east-2",
			endpoint: "database-1.c6yvg0shdgsd.us-east-2.rds.amazonaws.com",
			want:     "us-east-2",
			wantErr:  false,
		},
		{
			name:     "incorrect RDS endpoint, doesn't include rds.rds.amazonaws.com",
			endpoint: "database-1.c6yvg0shdgsd.us-east-2.rds.sjjjjjj.com",
			want:     "",
			wantErr:  true,
		},
		{
			name:     "incorrect RDS endpoint, only base URL",
			endpoint: "rds.amazonaws.com",
			want:     "",
			wantErr:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseRegion(tt.endpoint)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseRegion() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("parseRegion() got = %v, want %v", got, tt.want)
			}
		})
	}
}
