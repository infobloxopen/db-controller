package rdsauth

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/credentials/stscreds"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/rds/rdsutils"
)

const (
	rdsBaseURL = "rds.amazonaws.com"
	// Authentication tokens have a lifespan of 15 minutes
	// https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/UsingWithRDS.IAMDBAuth.html
	tokenExpirationTime = 900 * time.Second
)

func NewMasterAuth() *MasterAuth {
	return &MasterAuth{}
}

type MasterAuth struct {
	sync.RWMutex

	authToken string
	updatedAt time.Time
}

// Get gets new master token/password
func (ma *MasterAuth) Get() string {
	ma.RLock()
	defer ma.RUnlock()

	return ma.authToken
}

func (ma *MasterAuth) RetrieveToken(endpoint, user string) (string, error) {
	awsCreds := ma.SessionCredentials()

	token, err := ma.CreateRDSToken(endpoint, user, awsCreds)
	if err != nil {
		return "", err
	}

	return token, nil
}

// IsExpired checks if RDS auth toke expired
func (ma *MasterAuth) IsExpired() bool {
	// consider that token is expired if more than 15 - 1 minutes have passed since the token update
	return time.Since(ma.updatedAt) > tokenExpirationTime-time.Minute
}

// Set sets new master token/password
func (ma *MasterAuth) Set(newToken string) {
	ma.Lock()
	defer ma.Unlock()

	ma.authToken = newToken
	ma.updatedAt = time.Now()
}

func (ma *MasterAuth) CreateRDSToken(dbEndpoint, dbUser string, awsCreds *credentials.Credentials) (string, error) {
	region, err := ma.parseRegion(dbEndpoint)
	if err != nil {
		return "", err
	}

	authToken, err := rdsutils.BuildAuthToken(dbEndpoint, region, dbUser, awsCreds)
	if err != nil {
		return "", err
	}

	return authToken, nil
}

func (ma *MasterAuth) STSCreds(roleARN string) (*credentials.Credentials, error) {
	if roleARN == "" {
		return nil, fmt.Errorf("a role ARN name must be supplied")
	}

	sess := session.Must(session.NewSession())

	return stscreds.NewCredentials(sess, roleARN, func(p *stscreds.AssumeRoleProvider) {}), nil
}

func (ma *MasterAuth) SessionCredentials() *credentials.Credentials {
	sess := session.Must(session.NewSession())
	return sess.Config.Credentials
}

func (ma *MasterAuth) parseRegion(endpoint string) (string, error) {
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
