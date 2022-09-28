package dbclient

import (
	"context"
	"fmt"
	"net/url"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/rds/auth"
)

func awsAuthBuilder(ctx context.Context, cliCfg Config) (string, error) {

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return "", err
	}

	return awsAuthBuilderWithConfig(ctx, cliCfg, cfg)
}

func awsAuthBuilderWithConfig(ctx context.Context, cliCfg Config, awsCfg aws.Config) (string, error) {

	parsed, err := url.Parse(cliCfg.DSN)
	info := parsed.User

	authToken, err := auth.BuildAuthToken(
		ctx,
		fmt.Sprintf("%s:%s", parsed.Host, parsed.Port()),
		awsCfg.Region,
		info.Username(),
		awsCfg.Credentials,
	)

	parsed.User = url.UserPassword(info.Username(), authToken)

	return parsed.String(), err

}
