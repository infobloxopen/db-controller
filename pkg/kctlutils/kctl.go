package kctlutils

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	v1 "github.com/infobloxopen/db-controller/api/v1"
)

type Client struct {
	cli       client.Reader
	serviceNS string
	// cfg       *viper.Viper
}

func New(c client.Reader, serviceNS string) *Client {
	return &Client{
		cli:       c,
		serviceNS: serviceNS,
	}
}

// MasterCreds reads the master credentials that crossplane
// has stored in the secret
// Returns a DSN
func (c *Client) GetMasterDSN(ctx context.Context, secretName, databaseName, sslMode string) (string, error) {
	info, err := c.GetMasterCredsDeprecated(ctx, secretName, databaseName, sslMode)
	if err != nil {
		return "", err
	}
	return info.Uri(), nil
}

// GetMasterCredsDeprecated reads the master credentials
// FIXME: switch to the new GetMasterDSN
func (c *Client) GetMasterCredsDeprecated(ctx context.Context, secretName, databaseName string, sslMode string) (v1.DatabaseClaimConnectionInfo, error) {

	connInfo := v1.DatabaseClaimConnectionInfo{}
	if c.serviceNS == "" {
		return connInfo, fmt.Errorf("service namespace is required")
	}

	rs := &corev1.Secret{}
	serviceNS := c.serviceNS

	err := c.cli.Get(ctx, client.ObjectKey{
		Namespace: serviceNS,
		Name:      secretName,
	}, rs)
	//TODO handle not found vs other errors here
	if err != nil {
		return connInfo, err
	}

	connInfo.DatabaseName = databaseName
	connInfo.Host = string(rs.Data["endpoint"])
	connInfo.Port = string(rs.Data["port"])
	connInfo.Username = string(rs.Data["username"])
	connInfo.Password = string(rs.Data["password"])
	connInfo.SSLMode = sslMode

	log.FromContext(ctx).Info("GetMasterCredsDeprecated", "secret", secretName, "info", connInfo)

	if connInfo.Host == "" ||
		connInfo.Port == "" ||
		connInfo.Username == "" ||
		connInfo.Password == "" {
		return connInfo, fmt.Errorf("generated secret is incomplete")
	}

	return connInfo, nil
}

// GetConnDSN reads the connection secret created by db-controller
func (c *Client) GetConnDSN(ctx context.Context, dbClaim *v1.DatabaseClaim) (string, error) {
	connInfo, err := c.GetConnSecret(ctx, dbClaim)
	if err != nil {
		return "", err
	}
	return connInfo.Uri(), nil
}

// GetConnSecret reads the connection secret created by db-controller
func (c *Client) GetConnSecret(ctx context.Context, dbClaim *v1.DatabaseClaim) (*v1.DatabaseClaimConnectionInfo, error) {

	connInfo := v1.DatabaseClaimConnectionInfo{}

	rs := corev1.Secret{}

	err := c.cli.Get(ctx, client.ObjectKey{
		Namespace: dbClaim.Namespace,
		Name:      dbClaim.Spec.SecretName,
	}, &rs)
	//TODO handle not found vs other errors here
	if err != nil {
		return &connInfo, err
	}

	connInfo.Host = string(rs.Data["hostname"])
	connInfo.Port = string(rs.Data["port"])
	connInfo.Username = string(rs.Data["username"])
	connInfo.Password = string(rs.Data["password"])
	connInfo.SSLMode = string(rs.Data["sslmode"])
	connInfo.DatabaseName = string(rs.Data["database"])

	if connInfo.DatabaseName == "" ||
		connInfo.Host == "" ||
		connInfo.Port == "" ||
		connInfo.Username == "" ||
		connInfo.Password == "" {
		return &connInfo, fmt.Errorf("generated secret is incomplete")
	}

	return &connInfo, nil
}
