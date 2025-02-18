package providers

import (
	"context"
	"fmt"
	v1 "github.com/infobloxopen/db-controller/api/v1"
	"github.com/infobloxopen/db-controller/pkg/hostparams"
	"github.com/spf13/viper"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DatabaseSpec defines the required parameters to provision a database using any provider.
type DatabaseSpec struct {
	ProviderResourceName       string
	DbType                     v1.DatabaseType
	SharedDBHost               bool
	MasterConnInfo             v1.DatabaseClaimConnectionInfo
	TempSecret                 string
	HostParams                 hostparams.HostParams
	EnableReplicationRole      bool
	EnableSuperUser            bool
	EnablePerfInsight          bool
	EnableCloudwatchLogsExport []*string
	BackupRetentionDays        int64
	CACertificateIdentifier    string

	Tags                       v1.Tag
	Labels                     map[string]string
	PreferredMaintenanceWindow *string
	BackupPolicy               string
}

// Provider is an interface that abstracts provider-specific logic.
type Provider interface {
	// CreateDatabase provisions a new database instance.
	CreateDatabase(ctx context.Context, spec DatabaseSpec) error
	// DeleteDatabase deprovisions an existing database instance.
	DeleteDatabase(ctx context.Context, name string) error
	// GetDatabase retrieves the current status of a database instance.
	GetDatabase(ctx context.Context, name string) (*DatabaseSpec, error)
}

// NewProvider returns a concrete provider implementation based on the given provider name.
func NewProvider(config *viper.Viper, k8sClient client.Client) (Provider, error) {
	cloud := config.GetString("cloud")
	switch cloud {
	case "aws":
		return NewAWSProvider(k8sClient, config), nil
	case "gcp":
		return NewGCPProvider(k8sClient, config), nil
	case "cloudnative-pg":
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported provider for cloud: %s", cloud)
	}
}
