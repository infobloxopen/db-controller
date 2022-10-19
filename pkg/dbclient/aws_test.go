package dbclient

import (
	"context"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
)

func TestAWS_inject(t *testing.T) {

	tests := []struct {
		cfg    Config
		awsCfg aws.Config
		e      string
		err    error
	}{
		{
			cfg: Config{
				DSN: "postgres://user:pass@host:5555",
			},
			awsCfg: aws.Config{
				Region: "us-blox-1",
			},
			e: "us-blox-1",
		},
		{
			cfg: Config{
				DSN: "postgres://user:pass@host:5555",
			},
			awsCfg: aws.Config{
				Region: "us-blox-1",
			},
			e: "user",
		},
		{
			cfg: Config{
				DSN: "postgres://user:pass@host:5555",
			},
			awsCfg: aws.Config{
				Region: "us-blox-1",
			},
			e: "host:5555",
		},
	}

	creds := &staticCredentials{AccessKey: "AKID", SecretKey: "SECRET", Session: "SESSION"}

	for _, ts := range tests {
		awsCfg := ts.awsCfg
		awsCfg.Credentials = creds
		ad, err := awsAuthBuilderWithConfig(context.TODO(), ts.cfg, awsCfg)
		if err != ts.err {
			t.Errorf("\n   got: %s\nwanted: %s", err, ts.err)
			continue
		}

		if !strings.Contains(ad, ts.e) {
			t.Errorf("\n   got: %s\nwanted: %s", ad, ts.e)
		}
	}
}

type staticCredentials struct {
	AccessKey, SecretKey, Session string
}

func (s *staticCredentials) Retrieve(ctx context.Context) (aws.Credentials, error) {
	return aws.Credentials{
		AccessKeyID:     s.AccessKey,
		SecretAccessKey: s.SecretKey,
		SessionToken:    s.Session,
	}, nil
}
