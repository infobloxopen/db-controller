package providers

import (
	"context"
	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/spf13/viper"
	crossplanegcp "github.com/upbound/provider-gcp/apis/alloydb/v1beta2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
)

func TestAlloyDBCluster(t *testing.T) {
	tests := []struct {
		name           string
		params         DatabaseSpec
		expectedResult *crossplanegcp.Cluster
	}{
		{
			name: "Creates AlloyDB cluster with Postgres 14",
			params: DatabaseSpec{
				ResourceName:   "test-alloydb-cluster",
				Labels:         map[string]string{"env": "test", "app": "myapp"},
				DBVersion:      "14.1",
				MasterUsername: "admin",
				DeletionPolicy: xpv1.DeletionDelete,
			},
			expectedResult: &crossplanegcp.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-alloydb-cluster",
					Labels: map[string]string{"env": "test", "app": "myapp"},
				},
				Spec: crossplanegcp.ClusterSpec{
					ForProvider: crossplanegcp.ClusterParameters{
						AutomatedBackupPolicy: &crossplanegcp.AutomatedBackupPolicyParameters{
							Enabled: ptr.To(true),
						},
						DatabaseVersion: ptr.To("POSTGRES_14"),
						DeletionPolicy:  ptr.To("Delete"),
						DisplayName:     ptr.To("test-alloydb-cluster"),
						InitialUser: &crossplanegcp.InitialUserParameters{
							PasswordSecretRef: xpv1.SecretKeySelector{
								SecretReference: xpv1.SecretReference{
									Name:      "test-alloydb-cluster-master-password",
									Namespace: "default",
								},
								Key: "password",
							},
							User: ptr.To("admin"),
						},
						Location: ptr.To("us-central1"),
						PscConfig: &crossplanegcp.PscConfigParameters{
							PscEnabled: ptr.To(true),
						},
					},
					ResourceSpec: xpv1.ResourceSpec{
						WriteConnectionSecretToReference: &xpv1.SecretReference{
							Name:      "test-alloydb-cluster",
							Namespace: "default",
						},
						ProviderConfigReference: &xpv1.Reference{
							Name: "gcp-provider",
						},
						DeletionPolicy: xpv1.DeletionDelete,
					},
				},
			},
		},
		{
			name: "Creates AlloyDB cluster with Postgres 15",
			params: DatabaseSpec{
				ResourceName:   "prod-alloydb-cluster",
				Labels:         map[string]string{"env": "prod", "tier": "db"},
				DBVersion:      "15.0",
				MasterUsername: "postgres",
				DeletionPolicy: xpv1.DeletionOrphan,
			},
			expectedResult: &crossplanegcp.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "prod-alloydb-cluster",
					Labels: map[string]string{"env": "prod", "tier": "db"},
				},
				Spec: crossplanegcp.ClusterSpec{
					ForProvider: crossplanegcp.ClusterParameters{
						AutomatedBackupPolicy: &crossplanegcp.AutomatedBackupPolicyParameters{
							Enabled: ptr.To(true),
						},
						DatabaseVersion: ptr.To("POSTGRES_15"),
						DeletionPolicy:  ptr.To("Orphan"),
						DisplayName:     ptr.To("prod-alloydb-cluster"),
						InitialUser: &crossplanegcp.InitialUserParameters{
							PasswordSecretRef: xpv1.SecretKeySelector{
								SecretReference: xpv1.SecretReference{
									Name:      "prod-alloydb-cluster-master-password",
									Namespace: "default",
								},
								Key: MasterPasswordSecretKey,
							},
							User: ptr.To("postgres"),
						},
						Location: ptr.To("us-central1"),
						PscConfig: &crossplanegcp.PscConfigParameters{
							PscEnabled: ptr.To(true),
						},
					},
					ResourceSpec: xpv1.ResourceSpec{
						WriteConnectionSecretToReference: &xpv1.SecretReference{
							Name:      "prod-alloydb-cluster",
							Namespace: "default",
						},
						ProviderConfigReference: &xpv1.Reference{
							Name: "gcp-provider",
						},
						DeletionPolicy: xpv1.DeletionOrphan,
					},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			RegisterTestingT(t)
			mockConfig := viper.New()
			mockConfig.Set("providerConfig", "gcp-provider")
			mockConfig.Set("region", "us-central1")
			provider := &gcpProvider{config: mockConfig}

			result := provider.dbCluster(tc.params)

			// Assert expectations using GoMega
			Expect(result.ObjectMeta.Name).To(Equal(tc.expectedResult.ObjectMeta.Name))
			Expect(result.ObjectMeta.Labels).To(Equal(tc.expectedResult.ObjectMeta.Labels))

			// Check ForProvider fields
			Expect(result.Spec.ForProvider.DatabaseVersion).To(Equal(tc.expectedResult.Spec.ForProvider.DatabaseVersion))

			// Verify database version selection logic
			expectedDbVersion := alloyVersion15
			if strings.HasPrefix(tc.params.DBVersion, "14") {
				expectedDbVersion = alloyVersion14
			}
			Expect(*result.Spec.ForProvider.DatabaseVersion).To(Equal(expectedDbVersion))

			Expect(result.Spec.ForProvider.DeletionPolicy).To(Equal(tc.expectedResult.Spec.ForProvider.DeletionPolicy))
			Expect(result.Spec.ForProvider.DisplayName).To(Equal(tc.expectedResult.Spec.ForProvider.DisplayName))

			// Check AutomatedBackupPolicy
			Expect(result.Spec.ForProvider.AutomatedBackupPolicy.Enabled).To(Equal(tc.expectedResult.Spec.ForProvider.AutomatedBackupPolicy.Enabled))

			// Check InitialUser
			Expect(result.Spec.ForProvider.InitialUser.User).To(Equal(tc.expectedResult.Spec.ForProvider.InitialUser.User))
			Expect(result.Spec.ForProvider.InitialUser.PasswordSecretRef.SecretReference.Name).To(Equal(tc.params.ResourceName + MasterPasswordSuffix))
			Expect(result.Spec.ForProvider.InitialUser.PasswordSecretRef.SecretReference.Namespace).To(Equal(provider.serviceNS))
			Expect(result.Spec.ForProvider.InitialUser.PasswordSecretRef.Key).To(Equal(MasterPasswordSecretKey))

			// Check Location
			Expect(result.Spec.ForProvider.Location).To(Equal(tc.expectedResult.Spec.ForProvider.Location))

			// Check PscConfig
			Expect(result.Spec.ForProvider.PscConfig.PscEnabled).To(Equal(tc.expectedResult.Spec.ForProvider.PscConfig.PscEnabled))

			// Check ResourceSpec
			Expect(result.Spec.DeletionPolicy).To(Equal(tc.expectedResult.Spec.DeletionPolicy))
			Expect(result.Spec.ProviderConfigReference.Name).To(Equal(tc.expectedResult.Spec.ProviderConfigReference.Name))
			Expect(result.Spec.WriteConnectionSecretToReference.Name).To(Equal(tc.params.ResourceName))
			Expect(result.Spec.WriteConnectionSecretToReference.Namespace).To(Equal(provider.serviceNS))
		})
	}
}

func TestAlloyDBInstance(t *testing.T) {
	tests := []struct {
		name           string
		params         DatabaseSpec
		expectedResult *crossplanegcp.Instance
	}{
		{
			name: "Creates AlloyDB instance with REGIONAL availability type",
			params: DatabaseSpec{
				ResourceName:   "test-alloydb-instance",
				Labels:         map[string]string{"env": "test", "app": "myapp"},
				DeletionPolicy: xpv1.DeletionDelete,
			},
			expectedResult: &crossplanegcp.Instance{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-alloydb-instance",
					Labels: map[string]string{"env": "test", "app": "myapp"},
				},
				Spec: crossplanegcp.InstanceSpec{
					ForProvider: crossplanegcp.InstanceParameters{
						AvailabilityType: ptr.To("REGIONAL"),
						ClientConnectionConfig: &crossplanegcp.ClientConnectionConfigParameters{
							SSLConfig: &crossplanegcp.SSLConfigParameters{
								SSLMode: ptr.To("ENCRYPTED_ONLY"),
							},
						},
						ClusterRef: &xpv1.Reference{
							Name: "test-alloydb-instance",
						},
						DisplayName:  ptr.To("test-alloydb-instance"),
						GceZone:      ptr.To("us-central1"),
						InstanceType: ptr.To("PRIMARY"),
						NetworkConfig: &crossplanegcp.InstanceNetworkConfigParameters{
							EnablePublicIP: ptr.To(false),
						},
						PscInstanceConfig: &crossplanegcp.PscInstanceConfigParameters{
							AllowedConsumerProjects: []*string{ptr.To("test-project")},
						},
						DatabaseFlags: map[string]*string{"alloydb.iam_authentication": ptr.To("on")},
					},
					ResourceSpec: xpv1.ResourceSpec{
						ProviderConfigReference: &xpv1.Reference{
							Name: "gcp-provider",
						},
						DeletionPolicy: xpv1.DeletionDelete,
					},
				},
			},
		},
		{
			name: "Creates AlloyDB instance with ZONAL availability type",
			params: DatabaseSpec{
				ResourceName:   "prod-alloydb-instance",
				Labels:         map[string]string{"env": "prod", "tier": "db"},
				DeletionPolicy: xpv1.DeletionOrphan,
			},
			expectedResult: &crossplanegcp.Instance{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "prod-alloydb-instance",
					Labels: map[string]string{"env": "prod", "tier": "db"},
				},
				Spec: crossplanegcp.InstanceSpec{
					ForProvider: crossplanegcp.InstanceParameters{
						AvailabilityType: ptr.To("ZONAL"),
						ClientConnectionConfig: &crossplanegcp.ClientConnectionConfigParameters{
							SSLConfig: &crossplanegcp.SSLConfigParameters{
								SSLMode: ptr.To("ENCRYPTED_ONLY"),
							},
						},
						ClusterRef: &xpv1.Reference{
							Name: "prod-alloydb-instance",
						},
						DisplayName:  ptr.To("prod-alloydb-instance"),
						GceZone:      ptr.To("us-central1"),
						InstanceType: ptr.To("PRIMARY"),
						NetworkConfig: &crossplanegcp.InstanceNetworkConfigParameters{
							EnablePublicIP: ptr.To(false),
						},
						PscInstanceConfig: &crossplanegcp.PscInstanceConfigParameters{
							AllowedConsumerProjects: []*string{ptr.To("test-project")},
						},
						DatabaseFlags: map[string]*string{"alloydb.iam_authentication": ptr.To("on")},
					},
					ResourceSpec: xpv1.ResourceSpec{
						ProviderConfigReference: &xpv1.Reference{
							Name: "gcp-provider",
						},
						DeletionPolicy: xpv1.DeletionOrphan,
					},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			RegisterTestingT(t)
			mockConfig := viper.New()
			mockConfig.Set("providerConfig", "gcp-provider")
			mockConfig.Set("region", "us-central1")
			mockConfig.Set("project", "test-project")
			if *tc.expectedResult.Spec.ForProvider.AvailabilityType == "ZONAL" {
				mockConfig.Set("dbMultiAZEnabled", true)
			}

			provider := &gcpProvider{config: mockConfig}

			result := provider.dbInstance(tc.params)

			Expect(result.ObjectMeta.Name).To(Equal(tc.expectedResult.ObjectMeta.Name))
			Expect(result.ObjectMeta.Labels).To(Equal(tc.expectedResult.ObjectMeta.Labels))

			// Check ForProvider fields
			Expect(result.Spec.ForProvider.AvailabilityType).To(Equal(tc.expectedResult.Spec.ForProvider.AvailabilityType))
			Expect(result.Spec.ForProvider.ClientConnectionConfig.SSLConfig.SSLMode).To(Equal(tc.expectedResult.Spec.ForProvider.ClientConnectionConfig.SSLConfig.SSLMode))
			Expect(result.Spec.ForProvider.ClusterRef.Name).To(Equal(tc.expectedResult.Spec.ForProvider.ClusterRef.Name))
			Expect(result.Spec.ForProvider.DisplayName).To(Equal(tc.expectedResult.Spec.ForProvider.DisplayName))
			Expect(result.Spec.ForProvider.GceZone).To(Equal(tc.expectedResult.Spec.ForProvider.GceZone))
			Expect(result.Spec.ForProvider.InstanceType).To(Equal(tc.expectedResult.Spec.ForProvider.InstanceType))
			Expect(result.Spec.ForProvider.NetworkConfig.EnablePublicIP).To(Equal(tc.expectedResult.Spec.ForProvider.NetworkConfig.EnablePublicIP))

			// Check PSC instance config
			Expect(len(result.Spec.ForProvider.PscInstanceConfig.AllowedConsumerProjects)).To(Equal(1))
			Expect(*result.Spec.ForProvider.PscInstanceConfig.AllowedConsumerProjects[0]).To(Equal(*tc.expectedResult.Spec.ForProvider.PscInstanceConfig.AllowedConsumerProjects[0]))

			// Check database flags
			Expect(result.Spec.ForProvider.DatabaseFlags).To(HaveLen(1))
			Expect(*result.Spec.ForProvider.DatabaseFlags["alloydb.iam_authentication"]).To(Equal("on"))

			// Check ResourceSpec
			Expect(result.Spec.DeletionPolicy).To(Equal(tc.expectedResult.Spec.DeletionPolicy))
			Expect(result.Spec.ProviderConfigReference.Name).To(Equal(tc.expectedResult.Spec.ProviderConfigReference.Name))
		})
	}
}

func TestCreateSecretWithConnInfo(t *testing.T) {
	RegisterTestingT(t)
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = crossplanegcp.AddToScheme(scheme)

	var (
		namespace    = "test-namespace"
		resourceName = "test-db"
		password     = "test-password"
		username     = "admin"
		dnsName      = "test-dns-name"
	)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			"attribute.initial_user.0.password": []byte(password),
		},
	}

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(secret).
		Build()

	provider := &gcpProvider{
		k8sClient: cl,
		serviceNS: namespace,
	}

	dns := dnsName
	instance := &crossplanegcp.Instance{
		Status: crossplanegcp.InstanceStatus{
			AtProvider: crossplanegcp.InstanceObservation{
				PscInstanceConfig: &crossplanegcp.PscInstanceConfigObservation{
					PscDNSName: &dns,
				},
			},
		},
	}

	err := provider.createSecretWithConnInfo(context.Background(),
		DatabaseSpec{
			ResourceName:   resourceName,
			MasterUsername: username,
		},
		instance)

	Expect(err).To(BeNil())
	updatedSecret := &corev1.Secret{}
	err = cl.Get(context.Background(),
		client.ObjectKey{Name: resourceName, Namespace: namespace},
		updatedSecret)

	Expect(updatedSecret.Data["username"]).To(Equal([]byte(username)))
	Expect(updatedSecret.Data["password"]).To(Equal(secret.Data["attribute.initial_user.0.password"]))
	Expect(updatedSecret.Data["endpoint"]).To(Equal([]byte(*instance.Status.AtProvider.PscInstanceConfig.PscDNSName)))
	Expect(updatedSecret.Data["port"]).To(Equal([]byte("5432")))
}

func TestDbNetworkRecord(t *testing.T) {
	RegisterTestingT(t)
	var (
		namespace    = "test-namespace"
		resourceName = "test-db"
		dnsName      = "test-dns-name.example.com"
		saLink       = "projects/test-project/serviceAttachments/test-attachment"
		region       = "us-central1"
		network      = "projects/test-subnet/global/networks/projects/test-project/regions/us-central1/subnetworks/"
	)

	mockConfig := viper.New()
	mockConfig.Set("project", "test-subnet")
	mockConfig.Set("subnetwork", "test-subnet")
	mockConfig.Set("region", "us-central1")
	mockConfig.Set("network", "projects/test-project/regions/us-central1/subnetworks/")

	provider := &gcpProvider{
		serviceNS: namespace,
		config:    mockConfig,
	}
	spec := DatabaseSpec{
		ResourceName: resourceName,
	}
	dns := dnsName
	link := saLink
	instance := &crossplanegcp.Instance{
		Status: crossplanegcp.InstanceStatus{
			AtProvider: crossplanegcp.InstanceObservation{
				PscInstanceConfig: &crossplanegcp.PscInstanceConfigObservation{
					PscDNSName:            &dns,
					ServiceAttachmentLink: &link,
				},
			},
		},
	}

	result := provider.dbNetworkRecord(spec, instance)
	Expect(result).NotTo(BeNil())
	Expect(result.Name).To(Equal(resourceName + "-psc-network"))
	Expect(result.Namespace).To(Equal(namespace))

	params := result.Spec.Parameters
	Expect(params.PSCDNSName).To(Equal(dnsName))
	Expect(params.ServiceAttachmentLink).To(Equal(saLink))
	Expect(params.Region).To(Equal(region))
	Expect(params.Network).To(Equal(network))
}
