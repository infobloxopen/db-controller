package providers

import (
	"context"
	"fmt"
	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	persistanceinfobloxcomv1alpha1 "github.com/infobloxopen/db-controller/api/persistance.infoblox.com/v1alpha1"
	basefun "github.com/infobloxopen/db-controller/pkg/basefunctions"
	"github.com/spf13/viper"
	crossplanegcp "github.com/upbound/provider-gcp/apis/alloydb/v1beta2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"strings"
)

const (
	// https://cloud.google.com/alloydb/docs/reference/rest/v1beta/DatabaseVersion
	alloyVersion14          = "POSTGRES_14"
	alloyVersion15          = "POSTGRES_15"
	networkRecordNameSuffix = "-psc-network"
)

type gcpProvider struct {
	k8sClient client.Client
	config    *viper.Viper
	serviceNS string
}

func newGCPProvider(k8sClient client.Client, config *viper.Viper, serviceNS string) Provider {
	return &gcpProvider{
		k8sClient: k8sClient,
		config:    config,
		serviceNS: serviceNS,
	}
}

func (p *gcpProvider) CreateDatabase(ctx context.Context, spec DatabaseSpec) (bool, error) {
	clusterKey := client.ObjectKey{Name: spec.ResourceName}
	cluster := &crossplanegcp.Cluster{}
	err := ensureResource(ctx, p.k8sClient, clusterKey, cluster, func() (*crossplanegcp.Cluster, error) {
		cluster = p.dbCluster(spec)
		if err := ManageMasterPassword(ctx, &cluster.Spec.ForProvider.InitialUser.PasswordSecretRef, p.k8sClient); err != nil {
			log.FromContext(ctx).Error(err, "Failed to manage master password", "resource", spec.ResourceName)
		}
		return cluster, nil
	})
	if err != nil {
		return false, err
	}

	instanceKey := client.ObjectKey{Name: spec.ResourceName}
	instance := &crossplanegcp.Instance{}
	err = ensureResource(ctx, p.k8sClient, instanceKey, instance, func() (*crossplanegcp.Instance, error) {
		return p.dbInstance(spec), nil
	})
	if err != nil {
		return false, err
	}

	if !cluster.ObjectMeta.DeletionTimestamp.IsZero() {
		return false, fmt.Errorf("can not create Cloud DB cluster %s it is being deleted", spec.ResourceName)
	}

	err = p.updateDBClusterGCP(ctx, spec, cluster)
	if err != nil {
		return false, err
	}

	instanceReady, err := isReady(instance.Status.Conditions)
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to check instance readiness", "resource", spec.ResourceName)
		return false, fmt.Errorf("failed to check instance readiness: %w", err)
	}
	if !instanceReady {
		log.FromContext(ctx).Info("Waiting for DB Instance to be ready", "name", spec.ResourceName)
		return false, nil
	}

	networkRecordKey := client.ObjectKey{Name: spec.ResourceName + networkRecordNameSuffix}
	networkRecord := &persistanceinfobloxcomv1alpha1.XNetworkRecord{}
	err = ensureResource(ctx, p.k8sClient, networkRecordKey, networkRecord, func() (*persistanceinfobloxcomv1alpha1.XNetworkRecord, error) {
		return p.dbNetworkRecord(spec, instance), nil
	})
	if err != nil {
		return false, err
	}

	err = p.createSecretWithConnInfo(ctx, spec, instance)
	if err != nil {
		return false, err
	}

	return true, nil
}

func (p *gcpProvider) DeleteDatabase(ctx context.Context, spec DatabaseSpec) (bool, error) {
	if basefun.GetDefaultReclaimPolicy(p.config) != "delete" {
		return false, nil
	}

	deletionPolicy := client.PropagationPolicy(metav1.DeletePropagationBackground)
	// Delete DBCluster if it exists
	dbCluster := &crossplanegcp.Cluster{}
	clusterKey := client.ObjectKey{Name: spec.ResourceName}
	if err := p.k8sClient.Get(ctx, clusterKey, dbCluster); err == nil {
		if err := p.k8sClient.Delete(ctx, dbCluster, deletionPolicy); err != nil {
			return false, err
		}
	}

	// Delete DBInstance if it exists
	instanceKey := client.ObjectKey{Name: spec.ResourceName}
	dbInstance := &crossplanegcp.Instance{}
	if err := p.k8sClient.Get(ctx, instanceKey, dbInstance); err == nil {
		if err := p.k8sClient.Delete(ctx, dbInstance, deletionPolicy); err != nil {
			return false, err
		}
	}

	return true, nil
}

func (p *gcpProvider) GetDatabase(ctx context.Context, name string) (*DatabaseSpec, error) {
	panic("implement me")
}

func (p *gcpProvider) dbCluster(params DatabaseSpec) *crossplanegcp.Cluster {
	var dbVersion string
	if strings.HasPrefix(params.DBVersion, "14") {
		dbVersion = alloyVersion14
	} else {
		dbVersion = alloyVersion15
	}

	return &crossplanegcp.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:   params.ResourceName,
			Labels: params.Labels,
		},
		Spec: crossplanegcp.ClusterSpec{
			ForProvider: crossplanegcp.ClusterParameters{
				AutomatedBackupPolicy: &crossplanegcp.AutomatedBackupPolicyParameters{
					Enabled: ptr.To(true),
				},
				DatabaseVersion: &dbVersion,
				DeletionPolicy:  ptr.To(string(params.DeletionPolicy)),
				DisplayName:     ptr.To(params.ResourceName),
				InitialUser: &crossplanegcp.InitialUserParameters{
					PasswordSecretRef: xpv1.SecretKeySelector{
						SecretReference: xpv1.SecretReference{
							Name:      params.ResourceName + MasterPasswordSuffix,
							Namespace: p.serviceNS,
						},
						Key: MasterPasswordSecretKey,
					},
					User: &params.MasterUsername,
				},

				Location: ptr.To(basefun.GetRegion(p.config)),
				PscConfig: &crossplanegcp.PscConfigParameters{
					PscEnabled: ptr.To(true),
				},
			},
			ResourceSpec: xpv1.ResourceSpec{
				WriteConnectionSecretToReference: &xpv1.SecretReference{
					Name:      params.ResourceName,
					Namespace: p.serviceNS,
				},
				ProviderConfigReference: &xpv1.Reference{
					Name: basefun.GetProviderConfig(p.config),
				},
				DeletionPolicy: params.DeletionPolicy,
			},
		},
	}
}

func (p *gcpProvider) dbInstance(params DatabaseSpec) *crossplanegcp.Instance {
	multiAZ := basefun.GetMultiAZEnabled(p.config)
	return &crossplanegcp.Instance{
		ObjectMeta: metav1.ObjectMeta{
			Name:   params.ResourceName,
			Labels: params.Labels,
		},
		Spec: crossplanegcp.InstanceSpec{
			ForProvider: crossplanegcp.InstanceParameters{
				AvailabilityType: ptr.To((map[bool]string{true: "ZONAL", false: "REGIONAL"})[multiAZ]),
				ClientConnectionConfig: &crossplanegcp.ClientConnectionConfigParameters{
					SSLConfig: &crossplanegcp.SSLConfigParameters{
						SSLMode: ptr.To("ENCRYPTED_ONLY"),
					},
				},
				ClusterRef: &xpv1.Reference{
					Name: params.ResourceName,
				},

				DisplayName:  ptr.To(params.ResourceName),
				GceZone:      ptr.To(basefun.GetRegion(p.config)),
				InstanceType: ptr.To("PRIMARY"),
				NetworkConfig: &crossplanegcp.InstanceNetworkConfigParameters{
					EnablePublicIP: ptr.To(false),
				},
				PscInstanceConfig: &crossplanegcp.PscInstanceConfigParameters{
					AllowedConsumerProjects: []*string{ptr.To(basefun.GetProject(p.config))},
				},
				DatabaseFlags: map[string]*string{"alloydb.iam_authentication": ptr.To("on")},
			},
			ResourceSpec: xpv1.ResourceSpec{
				ProviderConfigReference: &xpv1.Reference{
					Name: basefun.GetProviderConfig(p.config),
				},
				DeletionPolicy: params.DeletionPolicy,
			},
		},
	}
}

func (p *gcpProvider) dbNetworkRecord(spec DatabaseSpec, dbInstance *crossplanegcp.Instance) *persistanceinfobloxcomv1alpha1.XNetworkRecord {
	return &persistanceinfobloxcomv1alpha1.XNetworkRecord{
		ObjectMeta: metav1.ObjectMeta{
			Name:      spec.ResourceName + "-psc-network",
			Namespace: p.serviceNS,
		},
		Spec: persistanceinfobloxcomv1alpha1.XNetworkRecordSpec{
			Parameters: persistanceinfobloxcomv1alpha1.XNetworkRecordParameters{
				PSCDNSName:            *dbInstance.Status.AtProvider.PscInstanceConfig.PscDNSName,
				ServiceAttachmentLink: *dbInstance.Status.AtProvider.PscInstanceConfig.ServiceAttachmentLink,
				Region:                basefun.GetRegion(p.config),
				Subnetwork:            basefun.GetSubNetwork(p.config),
				Network:               basefun.GetNetwork(p.config),
			},
		},
	}
}

func (p *gcpProvider) updateDBClusterGCP(ctx context.Context, params DatabaseSpec, dbCluster *crossplanegcp.Cluster) error {
	logr := log.FromContext(ctx)
	// Create a patch snapshot from current DBCluster
	patchDBCluster := client.MergeFrom(dbCluster.DeepCopy())
	// Update DBCluster
	if params.BackupRetentionDays != 0 {
		dbCluster.Spec.ForProvider.AutomatedBackupPolicy = &crossplanegcp.AutomatedBackupPolicyParameters{
			Enabled: ptr.To(true),
			QuantityBasedRetention: &crossplanegcp.QuantityBasedRetentionParameters{
				Count: basefun.GetNumBackupsToRetain(p.config),
			},
		}
	}
	dbCluster.Spec.DeletionPolicy = params.DeletionPolicy

	logr.Info("updating crossplane DBCluster resource", "DBCluster", dbCluster.Name)
	err := p.k8sClient.Patch(ctx, dbCluster, patchDBCluster)
	if err != nil {
		return err
	}

	return nil
}

func (p *gcpProvider) createSecretWithConnInfo(ctx context.Context, params DatabaseSpec, instance *crossplanegcp.Instance) error {
	var secret = &corev1.Secret{}
	err := p.k8sClient.Get(ctx, client.ObjectKey{
		Name:      params.ResourceName,
		Namespace: p.serviceNS,
	}, secret)
	if err != nil {
		return err
	}

	pass := string(secret.Data["attribute.initial_user.0.password"])

	secret.Data["username"] = []byte(params.MasterUsername)
	secret.Data["password"] = []byte(pass)
	secret.Data["endpoint"] = []byte(*instance.Status.AtProvider.PscInstanceConfig.PscDNSName)
	secret.Data["port"] = []byte("5432")

	return p.k8sClient.Update(ctx, secret)
}
