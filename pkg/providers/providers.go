package providers

import (
	"context"
	"fmt"
	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/spf13/viper"

	"sigs.k8s.io/controller-runtime/pkg/client"
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

// NewProvider returns a concrete provider implementation based on the given provider name.
func NewProvider(config *viper.Viper, k8sClient client.Client, serviceNS string) Provider {
	cloud := config.GetString("cloud")
	switch cloud {
	case "aws":
		return newAWSProvider(k8sClient, config, serviceNS)
	case "gcp":
		return newGCPProvider(k8sClient, config)
	case "cloudnative-pg":
		return newCloudNativePGProvider(k8sClient, config, serviceNS)
	default:
		panic(fmt.Sprintf("Unsupported provider %s", cloud))
	}
}
