package rdsauth

import (
	"flag"
	"testing"
	"time"
)

// The following gingo struct and associted init() is required to run go test with ginkgo related flags
// Since this test is not using ginkgo, this is a hack to get around the issue of go test complaining about
// unknown flags.
var ginkgo struct {
	dry_run      string
	label_filter string
}

func init() {
	flag.StringVar(&ginkgo.dry_run, "ginkgo.dry-run", "", "Ignore this flag")
	flag.StringVar(&ginkgo.label_filter, "ginkgo.label-filter", "", "Ignore this flag")
}

func Test_parseRegion(t *testing.T) {
	type mockAuth struct {
		authToken string
		updatedAt time.Time
	}

	tests := []struct {
		name     string
		mockauth mockAuth
		endpoint string
		want     string
		wantErr  bool
	}{
		{
			name:     "correct RDS endpoint, region us-east-2",
			mockauth: mockAuth{},
			endpoint: "database-1.c6yvg0shdgsd.us-east-2.rds.amazonaws.com",
			want:     "us-east-2",
			wantErr:  false,
		},
		{
			name:     "incorrect RDS endpoint, doesn't include rds.rds.amazonaws.com",
			mockauth: mockAuth{},
			endpoint: "database-1.c6yvg0shdgsd.us-east-2.rds.sjjjjjj.com",
			want:     "",
			wantErr:  true,
		},
		{
			name:     "incorrect RDS endpoint, only base URL",
			mockauth: mockAuth{},
			endpoint: "rds.amazonaws.com",
			want:     "",
			wantErr:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ma := &MasterAuth{
				authToken: tt.mockauth.authToken,
				updatedAt: tt.mockauth.updatedAt,
			}
			got, err := ma.parseRegion(tt.endpoint)
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
