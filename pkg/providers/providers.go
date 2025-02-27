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
	ResourceName               string
	HostParams                 hostparams.HostParams
	DbType                     v1.DatabaseType
	SharedDBHost               bool
	MasterConnInfo             v1.DatabaseClaimConnectionInfo
	TempSecret                 string
	EnableReplicationRole      bool
	EnableSuperUser            bool
	EnablePerfInsight          bool
	EnableCloudwatchLogsExport []*string
	BackupRetentionDays        int64
	CACertificateIdentifier    *string

	Labels                     map[string]string
	PreferredMaintenanceWindow *string
	BackupPolicy               string

	SnapshotID *string
}

// Provider is an interface that abstracts provider-specific logic.
type Provider interface {
	// CreateDatabase ensures a database instance is provisioned, updated, and ready.
	// If the instance does not exist, it provisions a new one.
	// Returns true if the database is fully ready, along with any encountered error.
	CreateDatabase(ctx context.Context, spec DatabaseSpec) (bool, error)
	// DeleteDatabase deprovisions an existing database instance.
	DeleteDatabase(ctx context.Context, spec DatabaseSpec) error
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
		return newGCPProvider(k8sClient, config, serviceNS)
	case "cloudnative-pg":
		return newCloudNativePGProvider(k8sClient, config, serviceNS)
	default:
		panic(fmt.Sprintf("Unsupported provider %s", cloud))
	}
}
