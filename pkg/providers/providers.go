package providers

import (
	"context"
	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/spf13/viper"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ProviderAWS         = "aws"
	ProviderGCP         = "gcp"
	ProviderCloudNative = "cloudnative-pg"
)

// DatabaseSpec defines the required parameters to provision a database using any provider.
type DatabaseSpec struct {
	ResourceName string
	DatabaseName string
	DbType       string
	Port         int64
	MinStorageGB int
	MaxStorageGB int64
	DBVersion    string

	MasterUsername                  string
	InstanceClass                   string
	StorageType                     string
	SkipFinalSnapshotBeforeDeletion bool
	PubliclyAccessible              bool
	EnableIAMDatabaseAuthentication bool
	DeletionPolicy                  xpv1.DeletionPolicy
	IsDefaultVersion                bool
	EnablePerfInsight               bool
	EnableCloudwatchLogsExport      []*string
	BackupRetentionDays             int64
	CACertificateIdentifier         *string

	Tags                       []ProviderTag
	Labels                     map[string]string
	PreferredMaintenanceWindow *string
	BackupPolicy               string

	SnapshotID  *string
	TagInactive bool
}

// Provider is an interface that abstracts provider-specific logic.
type Provider interface {
	// CreateDatabase ensures a database instance is provisioned, updated, and ready.
	// If the instance does not exist, it provisions a new one.
	// Returns true if the database is fully ready, along with any encountered error.
	CreateDatabase(ctx context.Context, spec DatabaseSpec) (bool, error)
	// DeleteDatabaseResources deprovisions an existing database instance.
	DeleteDatabaseResources(ctx context.Context, spec DatabaseSpec) (bool, error)
	// GetDatabase retrieves the current status of a database instance.
	GetDatabase(ctx context.Context, name string) (*DatabaseSpec, error)
}

type Providers map[string]Provider

func RegisterProviders(config *viper.Viper, k8sClient client.Client, serviceNS string) Providers {
	providers := make(Providers)

	// Always add the CloudNativePG provider if it is enabled
	if config.GetBool("enableCloudnativePG") {
		providers[ProviderCloudNative] = newCloudNativePGProvider(k8sClient, config, serviceNS)
	}

	// Add selected cloud provider
	if cloud := config.GetString("cloud"); cloud == ProviderAWS {
		providers[ProviderAWS] = newAWSProvider(k8sClient, config, serviceNS)
	} else if cloud == ProviderGCP {
		providers[ProviderGCP] = newGCPProvider(k8sClient, config)
	}

	if len(providers) == 0 {
		panic("Invalid number of providers configured. Must have at least one.")
	}

	return providers
}

func (p Providers) GetPrimaryProvider(dbType string) Provider {
	if dbType == "cloudnative-postgres" {
		return p[ProviderCloudNative]
	}
	if provider, exists := p[ProviderAWS]; exists {
		return provider
	}
	if provider, exists := p[ProviderGCP]; exists {
		return provider
	}
	return nil
}
