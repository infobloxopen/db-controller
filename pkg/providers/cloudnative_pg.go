package providers

import (
	"context"
	"fmt"
	"github.com/aws/smithy-go/ptr"
	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/spf13/viper"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"strconv"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	DefaultUsername         = "postgres"
	ReadWriteEndpointSuffix = "-rw."

	TierSmall  = "small"
	TierMedium = "medium"
	TierLarge  = "large"
	TierXLarge = "xlarge"

	StorageTypeDefault = "default"
)

// InstanceTier defines the resource configuration for a database tier
type InstanceTier struct {
	CPURequest    string
	CPULimit      string
	MemoryRequest string
	MemoryLimit   string
	Instances     int
}

type StorageConfig struct {
	StorageClass string
}

var TierConfigMap = map[string]InstanceTier{
	TierSmall: {
		CPURequest:    "100m",
		CPULimit:      "500m",
		MemoryRequest: "256Mi",
		MemoryLimit:   "512Mi",
		Instances:     1,
	},
	TierMedium: {
		CPURequest:    "500m",
		CPULimit:      "1",
		MemoryRequest: "1Gi",
		MemoryLimit:   "2Gi",
		Instances:     1,
	},
	TierLarge: {
		CPURequest:    "1",
		CPULimit:      "2",
		MemoryRequest: "4Gi",
		MemoryLimit:   "8Gi",
		Instances:     2,
	},
	TierXLarge: {
		CPURequest:    "2",
		CPULimit:      "4",
		MemoryRequest: "8Gi",
		MemoryLimit:   "16Gi",
		Instances:     3,
	},
}

var StorageConfigMap = map[string]StorageConfig{
	StorageTypeDefault: {
		StorageClass: "standard",
	},
}

func getInstanceConfig(instanceClass string) InstanceTier {
	if tier, exists := TierConfigMap[instanceClass]; exists {
		return tier
	}
	return TierConfigMap[TierMedium]
}

func getStorageConfig(storageType string) StorageConfig {
	if config, exists := StorageConfigMap[storageType]; exists {
		return config
	}
	return StorageConfigMap[StorageTypeDefault]
}

type CloudNativePGProvider struct {
	client.Client

	config    *viper.Viper
	serviceNS string
}

func newCloudNativePGProvider(k8sClient client.Client, config *viper.Viper, serviceNS string) Provider {
	return &CloudNativePGProvider{
		config:    config,
		Client:    k8sClient,
		serviceNS: serviceNS,
	}
}

func (p *CloudNativePGProvider) CreateDatabase(ctx context.Context, spec DatabaseSpec) (bool, error) {
	logger := log.FromContext(ctx)
	logger.Info("provisioning postgres database", "DatabaseSpec", spec)

	cluster := p.buildClusterFromSpec(spec)

	dbCluster := &cnpgv1.Cluster{}
	clusterKey := client.ObjectKey{Name: spec.ResourceName, Namespace: p.serviceNS}
	if err := verifyOrCreateResource(ctx, p.Client, clusterKey, dbCluster, func() (*cnpgv1.Cluster, error) {
		secretRef := &xpv1.SecretKeySelector{
			SecretReference: xpv1.SecretReference{
				Name:      spec.ResourceName,
				Namespace: p.serviceNS,
			},
			Key: MasterPasswordSecretKey,
		}
		if err := ManageMasterPassword(ctx, secretRef, p.Client); err != nil {
			return nil, fmt.Errorf("failed to create master password %v", err)
		}
		return cluster, nil
	}); err != nil {
		return false, err
	}

	ready, err := p.isReady(ctx, spec)
	if err != nil {
		return false, err
	}

	return ready, nil
}

func (p *CloudNativePGProvider) buildClusterFromSpec(spec DatabaseSpec) *cnpgv1.Cluster {
	tierConfig := getInstanceConfig(spec.InstanceClass)
	storageConfig := getStorageConfig(spec.StorageType)

	cluster := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      spec.ResourceName,
			Namespace: p.serviceNS,
			Labels:    spec.Labels,
		},
		Spec: cnpgv1.ClusterSpec{
			Instances:             tierConfig.Instances,
			ImageName:             fmt.Sprintf("ghcr.io/cloudnative-pg/postgresql:%s", spec.DBVersion),
			PrimaryUpdateStrategy: cnpgv1.PrimaryUpdateStrategyUnsupervised,
			PrimaryUpdateMethod:   cnpgv1.PrimaryUpdateMethodSwitchover,
			StorageConfiguration: cnpgv1.StorageConfiguration{
				StorageClass: &storageConfig.StorageClass,
				Size:         fmt.Sprintf("%dGi", max(spec.MinStorageGB, 1)),
			},
			SuperuserSecret: &cnpgv1.LocalObjectReference{
				Name: spec.ResourceName,
			},
			EnableSuperuserAccess: ptr.Bool(true),
			PostgresConfiguration: cnpgv1.PostgresConfiguration{
				AdditionalLibraries: []string{"pg_stat_statements"},
			},
			Resources: v1.ResourceRequirements{
				Requests: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse(tierConfig.CPURequest),
					v1.ResourceMemory: resource.MustParse(tierConfig.MemoryRequest),
				},
				Limits: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse(tierConfig.CPULimit),
					v1.ResourceMemory: resource.MustParse(tierConfig.MemoryLimit),
				},
			},
			Bootstrap: &cnpgv1.BootstrapConfiguration{
				InitDB: &cnpgv1.BootstrapInitDB{
					Database: spec.DatabaseName,
				},
			},
		},
	}
	return cluster
}

func (p *CloudNativePGProvider) isReady(ctx context.Context, spec DatabaseSpec) (bool, error) {
	logger := log.FromContext(ctx)
	logger.Info("checking postgres cluster readiness", "ResourceName", spec.ResourceName)

	cluster := &cnpgv1.Cluster{}
	clusterKey := client.ObjectKey{Name: spec.ResourceName, Namespace: p.serviceNS}

	if err := p.Client.Get(ctx, clusterKey, cluster); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("Cluster not found", "ResourceName", spec.ResourceName)
			return false, nil
		}
		logger.Error(err, "Failed to get cluster", "ResourceName", spec.ResourceName)
		return false, err
	}

	if cluster.Status.ReadyInstances != cluster.Spec.Instances {
		logger.Info("Cluster is not ready", "ResourceName", spec.ResourceName)
		return false, nil
	}

	err := p.updateMasterSecret(ctx, spec)
	if err != nil {
		return false, err
	}

	logger.Info("Cluster is ready", "ResourceName", spec.ResourceName, "ReadyInstances", cluster.Status.ReadyInstances, "ExpectedInstances", cluster.Spec.Instances)
	return true, nil
}

func (p *CloudNativePGProvider) updateMasterSecret(ctx context.Context, spec DatabaseSpec) error {
	var secret = &corev1.Secret{}
	err := p.Client.Get(ctx, client.ObjectKey{
		Name:      spec.ResourceName,
		Namespace: p.serviceNS,
	}, secret)
	if err != nil {
		return err
	}

	secret.Data["username"] = []byte(DefaultUsername)
	secret.Data["endpoint"] = []byte(spec.ResourceName + ReadWriteEndpointSuffix + p.serviceNS + ".svc.cluster.local")
	secret.Data["port"] = []byte(strconv.FormatInt(spec.Port, 10))

	log.FromContext(ctx).Info("updating conninfo secret", "name", secret.Name, "namespace", secret.Namespace)
	return p.Client.Update(ctx, secret)
}

func (p *CloudNativePGProvider) DeleteDatabaseResources(ctx context.Context, spec DatabaseSpec) (bool, error) {
	logger := log.FromContext(ctx)
	logger.Info("deleting postgres database resources", "ResourceName", spec.ResourceName)

	cluster := &cnpgv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      spec.ResourceName,
			Namespace: p.serviceNS,
		},
	}

	if err := p.Client.Get(ctx, client.ObjectKey{Name: spec.ResourceName, Namespace: p.serviceNS}, cluster); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("Cluster already deleted", "ResourceName", spec.ResourceName)
		} else {
			return false, fmt.Errorf("failed to get cluster for deletion: %w", err)
		}
	} else {
		if err := p.Client.Delete(ctx, cluster); err != nil {
			if !apierrors.IsNotFound(err) {
				logger.Error(err, "Failed to delete cluster", "ResourceName", spec.ResourceName)
				return false, fmt.Errorf("failed to delete cluster: %w", err)
			}
		}
	}
	return true, nil
}

func (p *CloudNativePGProvider) GetDatabase(ctx context.Context, name string) (*DatabaseSpec, error) {
	panic("implement me")
}
