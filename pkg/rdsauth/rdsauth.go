package rdsauth

import (
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/credentials/stscreds"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/rds/rdsutils"
)

const rdsBaseURL = "rds.amazonaws.com"

func CreateRDSToken(dbEndpoint, dbUser string, awsCreds *credentials.Credentials) (string, error) {
	region, err := parseRegion(dbEndpoint)
	if err != nil {
		return "", err
	}

	authToken, err := rdsutils.BuildAuthToken(dbEndpoint, region, dbUser, awsCreds)
	if err != nil {
		return "", err
	}

	return authToken, nil
}

func STSCreds(roleARN string) (*credentials.Credentials, error) {
	if roleARN == "" {
		return nil, fmt.Errorf("a role ARN name must be supplied")
	}

	sess := session.Must(session.NewSession())

	return stscreds.NewCredentials(sess, roleARN, func(p *stscreds.AssumeRoleProvider) {}), nil
}

func SessionCredentials() *credentials.Credentials {
	sess := session.Must(session.NewSession())
	return sess.Config.Credentials
}

func parseRegion(endpoint string) (string, error) {
	if !strings.Contains(endpoint, rdsBaseURL) {
		return "", fmt.Errorf("endpoint %s is not proper rds endpoint", endpoint)
	}

	splitted := strings.Split(endpoint, ".")

	if len(splitted) < 4 {
		return "", fmt.Errorf("endpoint name doesn't contain region")
	}

	region := splitted[len(splitted)-4]

	return region, nil
}
