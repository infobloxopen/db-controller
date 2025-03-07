package providers

import (
	"context"
	crossplaneaws "github.com/crossplane-contrib/provider-aws/apis/rds/v1alpha1"
	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	v1 "github.com/infobloxopen/db-controller/api/v1"
	basefun "github.com/infobloxopen/db-controller/pkg/basefunctions"
	"github.com/infobloxopen/db-controller/pkg/config"
	"github.com/infobloxopen/db-controller/pkg/hostparams"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/viper"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"path/filepath"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"testing"

	k8sRuntime "k8s.io/apimachinery/pkg/runtime"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var k8sClient client.Client
var controllerConfig *viper.Viper

func TestAWSProvider(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "AWS Provider Test Suite")
}

var _ = BeforeSuite(func() {
	By("bootstrapping test environment")
	sch := k8sRuntime.NewScheme()
	err := scheme.AddToScheme(sch)
	Expect(err).NotTo(HaveOccurred())
	err = crossplaneaws.AddToScheme(sch)
	Expect(err).NotTo(HaveOccurred())
	k8sClient = fake.NewClientBuilder().WithScheme(sch).Build()

	By("setting up the database controller")
	configPath, err := filepath.Abs(filepath.Join("..", "..", "cmd", "config", "config.yaml"))
	Expect(err).NotTo(HaveOccurred())
	controllerConfig = config.NewConfig(configPath)
})

var _ = AfterSuite(func() {})

var _ = Describe("AWSProvider create Postgres database", func() {
	var (
		provider *AWSProvider
		ctx      context.Context
		spec     DatabaseSpec
	)

	BeforeEach(func() {
		ctx = context.TODO()
		spec = DatabaseSpec{
			ResourceName: "env-app-name-db-1d9fb876",
			HostParams: hostparams.HostParams{
				Shape:                           "db.t3.medium",
				MinStorageGB:                    10,
				MaxStorageGB:                    20,
				DBVersion:                       "15.7",
				SkipFinalSnapshotBeforeDeletion: true,
				MasterUsername:                  "root",
				EnableIAMDatabaseAuthentication: true,
				StorageType:                     "storage-type",
				DeletionPolicy:                  xpv1.DeletionOrphan,
				PubliclyAccessible:              true,
				InstanceClass:                   "db.t3.medium",
			},
			DbType:       "postgres",
			SharedDBHost: false,
			MasterConnInfo: v1.DatabaseClaimConnectionInfo{
				Username: "testadmin",
				Password: "test-password-123",
				Port:     "5432",
			},
			TempSecret:                 "temp-secret-abc123",
			EnableReplicationRole:      true,
			EnableSuperUser:            false,
			EnablePerfInsight:          true,
			EnableCloudwatchLogsExport: []*string{ptr.To("postgresql"), ptr.To("upgrade")},
			BackupRetentionDays:        7,
			CACertificateIdentifier:    ptr.To("rds-ca-2019"),
			Tags: []ProviderTag{
				{Key: "environment", Value: "test"},
				{Key: "managed-by", Value: "controller-test"},
			},
			Labels: map[string]string{
				"app":         "test-app",
				"environment": "test",
				"team":        "database",
			},
			PreferredMaintenanceWindow: ptr.To("sun:02:00-sun:03:00"),
			BackupPolicy:               "daily",
			SnapshotID:                 nil,
		}
		provider = &AWSProvider{
			k8sClient: k8sClient,
			config:    controllerConfig,
			serviceNS: "db-controller",
		}
	})

	AfterEach(func() {
		Expect(k8sClient.DeleteAllOf(ctx, &crossplaneaws.DBInstance{})).To(Succeed())
		Expect(k8sClient.DeleteAllOf(ctx, &crossplaneaws.DBParameterGroup{})).To(Succeed())
		Expect(k8sClient.DeleteAllOf(ctx, &crossplaneaws.DBCluster{})).To(Succeed())
	})

	Describe("create postgres database", func() {
		When("create function is called with correct parameters", func() {
			It("should properly create crossplane resources", func() {
				_, err := provider.CreateDatabase(ctx, spec)
				Expect(err).ToNot(HaveOccurred())

				// Validate DBParameterGroup
				paramGroup := &crossplaneaws.DBParameterGroup{}
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: "env-app-name-db-1d9fb876-15"}, paramGroup)
				}).Should(Succeed())

				Expect(paramGroup.Spec.ForProvider.Parameters).To(ContainElements(
					crossplaneaws.CustomParameter{
						ParameterName:  ptr.To("idle_in_transaction_session_timeout"),
						ParameterValue: ptr.To("300000"),
						ApplyMethod:    ptr.To("immediate"),
					},
					crossplaneaws.CustomParameter{
						ParameterName:  ptr.To("shared_preload_libraries"),
						ParameterValue: ptr.To("pg_stat_statements,pg_cron"),
						ApplyMethod:    ptr.To("pending-reboot"),
					},
					crossplaneaws.CustomParameter{
						ParameterName:  ptr.To("cron.database_name"),
						ParameterValue: ptr.To(spec.MasterConnInfo.DatabaseName),
						ApplyMethod:    ptr.To("pending-reboot"),
					},
				))

				dbInstance := &crossplaneaws.DBInstance{}
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: "env-app-name-db-1d9fb876"}, dbInstance)
				}).Should(Succeed())

				Expect(dbInstance.Spec.ForProvider.Engine).To(Equal(ptr.To(spec.HostParams.Type)))
				Expect(dbInstance.Spec.ForProvider.EngineVersion).To(Equal(GetEngineVersion(spec.HostParams, provider.config)))
				Expect(dbInstance.Spec.ForProvider.DBInstanceClass).To(Equal(ptr.To(spec.HostParams.InstanceClass)))
				Expect(dbInstance.Spec.ForProvider.AllocatedStorage).To(Equal(ptr.To(int64(spec.HostParams.MinStorageGB))))
				Expect(dbInstance.Spec.ForProvider.DBParameterGroupNameRef.Name).To(Equal("env-app-name-db-1d9fb876-15"))
				Expect(dbInstance.Spec.ForProvider.CACertificateIdentifier).To(Equal(spec.CACertificateIdentifier))
				Expect(dbInstance.Spec.ForProvider.MultiAZ).To(Equal(ptr.To(basefun.GetMultiAZEnabled(provider.config))))
				Expect(dbInstance.Spec.ForProvider.MasterUsername).To(Equal(ptr.To(spec.HostParams.MasterUsername)))
				Expect(dbInstance.Spec.ForProvider.PubliclyAccessible).To(Equal(ptr.To(spec.HostParams.PubliclyAccessible)))
				Expect(dbInstance.Spec.ForProvider.EnableIAMDatabaseAuthentication).To(Equal(ptr.To(spec.HostParams.EnableIAMDatabaseAuthentication)))
				Expect(dbInstance.Spec.ForProvider.EnablePerformanceInsights).To(Equal(ptr.To(spec.EnablePerfInsight)))
				Expect(dbInstance.Spec.ForProvider.EnableCloudwatchLogsExports).To(Equal(spec.EnableCloudwatchLogsExport))
				Expect(dbInstance.Spec.ForProvider.BackupRetentionPeriod).To(Equal(ptr.To(spec.BackupRetentionDays)))
				Expect(dbInstance.Spec.ForProvider.StorageEncrypted).To(Equal(ptr.To(true)))
				Expect(dbInstance.Spec.ForProvider.StorageType).To(Equal(ptr.To(spec.HostParams.StorageType)))
				Expect(dbInstance.Spec.ForProvider.Port).To(Equal(ptr.To(spec.HostParams.Port)))
				Expect(dbInstance.Spec.ForProvider.PreferredMaintenanceWindow).To(Equal(spec.PreferredMaintenanceWindow))
				Expect(dbInstance.Spec.ForProvider.DBParameterGroupNameRef.Name).To(Equal("env-app-name-db-1d9fb876-15"))

				// master password
				Expect(dbInstance.Spec.ForProvider.MasterUserPasswordSecretRef.SecretReference.Name).To(Equal(spec.ResourceName + MasterPasswordSuffix))
				Expect(dbInstance.Spec.ForProvider.MasterUserPasswordSecretRef.SecretReference.Namespace).To(Equal(provider.serviceNS))
				Expect(dbInstance.Spec.ResourceSpec.WriteConnectionSecretToReference.Name).To(Equal(spec.ResourceName))
				Expect(dbInstance.Spec.ResourceSpec.WriteConnectionSecretToReference.Namespace).To(Equal(provider.serviceNS))

				// Validate VPC & Security Groups
				Expect(dbInstance.Spec.ForProvider.VPCSecurityGroupIDRefs).To(ContainElement(xpv1.Reference{
					Name: basefun.GetVpcSecurityGroupIDRefs(provider.config),
				}))
				Expect(dbInstance.Spec.ForProvider.DBSubnetGroupNameRef.Name).To(Equal(basefun.GetDbSubnetGroupNameRef(provider.config)))

				// Validate Provider Config & Deletion Policy
				Expect(dbInstance.Spec.ResourceSpec.ProviderConfigReference.Name).To(Equal(basefun.GetProviderConfig(provider.config)))
				Expect(dbInstance.Spec.ResourceSpec.DeletionPolicy).To(Equal(spec.HostParams.DeletionPolicy))

			})

			It("should creates master password secret for the new database instance", func() {
				_, err := provider.CreateDatabase(ctx, spec)
				Expect(err).ToNot(HaveOccurred())

				// Define the expected secret key
				secretKey := types.NamespacedName{
					Name:      spec.ResourceName + MasterPasswordSuffix,
					Namespace: provider.serviceNS,
				}

				// Validate that the secret is created
				masterSecret := &corev1.Secret{}
				Eventually(func() error {
					return k8sClient.Get(ctx, secretKey, masterSecret)
				}).Should(Succeed())

				// Validate the secret contains the expected key
				Expect(masterSecret.Data).To(HaveKey(MasterPasswordSecretKey))
				Expect(masterSecret.Data[MasterPasswordSecretKey]).ToNot(BeEmpty())
			})

			It("should return true when database is already provisioned", func() {
				isReady, err := provider.CreateDatabase(ctx, spec)
				Expect(err).ToNot(HaveOccurred())
				Expect(isReady).To(BeFalse()) // Initially, the database is not provisioned

				dbInstance := &crossplaneaws.DBInstance{}
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: spec.ResourceName}, dbInstance)
				}).Should(Succeed())

				updatedInstance := dbInstance.DeepCopy()
				updatedInstance.Status.Conditions = []xpv1.Condition{
					{
						Type:               xpv1.TypeReady,
						Status:             corev1.ConditionTrue,
						LastTransitionTime: metav1.Now(),
					},
				}

				// Manually trigger an Update (not Status().Update())
				Expect(k8sClient.Update(ctx, updatedInstance)).To(Succeed())

				// Call CreateDatabase again and check if it returns true
				isReady, err = provider.CreateDatabase(ctx, spec)
				Expect(err).ToNot(HaveOccurred())
				Expect(isReady).To(BeTrue()) // Now, the database should be marked as ready
			})

			It("should it propagates the spec provider tags to database instance, add operational active tag, and add backup tag", func() {
				_, err := provider.CreateDatabase(ctx, spec)
				Expect(err).ToNot(HaveOccurred())

				dbInstance := &crossplaneaws.DBInstance{}
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: spec.ResourceName}, dbInstance)
				}).Should(Succeed())

				expectedTags := spec.Tags
				expectedTags = append(expectedTags, OperationalTAGActive)
				expectedTags = append(expectedTags, ProviderTag{Key: BackupPolicyKey, Value: spec.BackupPolicy})
				// Validate Tags
				Expect(dbInstance.Spec.ForProvider.Tags).To(ConsistOf(ConvertFromProviderTags(expectedTags, func(tag ProviderTag) *crossplaneaws.Tag {
					return &crossplaneaws.Tag{Key: &tag.Key, Value: &tag.Value}
				})))
			})

			It("should configures RestoreDBInstanceBackupConfiguration when snapshot id is passed", func() {
				newSpec := spec
				newSpec.SnapshotID = ptr.To("snapshot-id")

				_, err := provider.CreateDatabase(ctx, newSpec)
				Expect(err).ToNot(HaveOccurred())

				dbInstance := &crossplaneaws.DBInstance{}
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: "env-app-name-db-1d9fb876"}, dbInstance)
				}).Should(Succeed())

				Expect(dbInstance.Spec.ForProvider.RestoreFrom).ToNot(BeNil())
				Expect(dbInstance.Spec.ForProvider.RestoreFrom.Snapshot.SnapshotIdentifier).To(Equal(newSpec.SnapshotID))
				Expect(dbInstance.Spec.ForProvider.RestoreFrom.Source).To(Equal(ptr.To(SnapshotSource)))
			})

			It("should propagate passed parameter provider labels to database instance", func() {
				_, err := provider.CreateDatabase(ctx, spec)
				Expect(err).ToNot(HaveOccurred())

				dbInstance := &crossplaneaws.DBInstance{}
				Eventually(func() error {
					return k8sClient.Get(ctx, types.NamespacedName{Name: "env-app-name-db-1d9fb876"}, dbInstance)
				}).Should(Succeed())

				Expect(dbInstance.Labels).To(Equal(spec.Labels))
			})
		})
	})

	Describe("update postgres database", func() {
		When("when crossplane cr preexists the creation call we need to update with provided parameters", func() {
		})
	})

	Describe("delete database interface usage", func() {
		When("delete func is called with correct spec and no operational tagging", func() {
			It("should delete postgres the database right away without adding operational tagging", func() {
				By("creating a new database")
				_, err := provider.CreateDatabase(ctx, spec)
				Expect(err).ToNot(HaveOccurred())

				deleted, err := provider.DeleteDatabase(ctx, spec)
				Expect(err).ToNot(HaveOccurred())
				Expect(deleted).To(BeTrue())

				paramGroup := &crossplaneaws.DBParameterGroup{}
				err = k8sClient.Get(ctx, types.NamespacedName{Name: "env-app-name-db-1d9fb876-15"}, paramGroup)
				Expect(err).To(HaveOccurred())
				Expect(errors.IsNotFound(err)).To(BeTrue())

				dbInstance := &crossplaneaws.DBInstance{}
				err = k8sClient.Get(ctx, types.NamespacedName{Name: "env-app-name-db-1d9fb876"}, dbInstance)
				Expect(err).To(HaveOccurred())
				Expect(errors.IsNotFound(err)).To(BeTrue())
			})
		})

		When("delete func is called with correct spec and instructed to add inactive operational tagging", func() {
			It("should delete postgres database with operational tagging only when tag is propagated to aws", func() {
				By("creating a new database")
				_, err := provider.CreateDatabase(ctx, spec)
				Expect(err).ToNot(HaveOccurred())

				deleteSpec := spec
				deleteSpec.TagInactive = true
				deleted, err := provider.DeleteDatabase(ctx, deleteSpec)
				Expect(err).ToNot(HaveOccurred())
				Expect(deleted).To(BeFalse())

				paramGroup := &crossplaneaws.DBParameterGroup{}
				err = k8sClient.Get(ctx, types.NamespacedName{Name: "env-app-name-db-1d9fb876-15"}, paramGroup)
				Expect(err).ToNot(HaveOccurred())

				dbInstance := &crossplaneaws.DBInstance{}
				err = k8sClient.Get(ctx, types.NamespacedName{Name: "env-app-name-db-1d9fb876"}, dbInstance)
				Expect(err).ToNot(HaveOccurred())

				Expect(dbInstance.Spec.ForProvider.Tags).To(ContainElement(&crossplaneaws.Tag{
					Key:   ptr.To("operational-status"),
					Value: ptr.To("inactive"),
				}))

				By("simulating the operational tagging propagated to aws")
				patchDBInstance := client.MergeFrom(dbInstance.DeepCopy())
				dbInstance.Status.AtProvider.TagList = append(
					dbInstance.Status.AtProvider.TagList,
					&crossplaneaws.Tag{Key: &OperationalTAGInactive.Key, Value: &OperationalTAGInactive.Value})
				err = k8sClient.Patch(ctx, dbInstance, patchDBInstance)
				Expect(err).ToNot(HaveOccurred())

				deleted, err = provider.DeleteDatabase(ctx, deleteSpec)
				Expect(err).ToNot(HaveOccurred())
				Expect(deleted).To(BeTrue())
			})
		})
	})
})

func createFakeClientWithObjects(objects ...k8sRuntime.Object) client.Client {
	sch := k8sRuntime.NewScheme()
	_ = scheme.AddToScheme(sch)
	_ = crossplaneaws.AddToScheme(sch)

	return fake.NewClientBuilder().
		WithScheme(sch).
		WithRuntimeObjects(objects...).
		Build()
}

func TestIsDBInstanceReady(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name               string
		instanceName       string
		instanceConditions []xpv1.Condition
		expectedReady      bool
		expectedError      bool
	}{
		{
			name:          "Instance not found",
			instanceName:  "non-existent-instance",
			expectedReady: false,
			expectedError: false,
		},
		{
			name:         "Instance not ready (Creating)",
			instanceName: "test-instance",
			instanceConditions: []xpv1.Condition{
				xpv1.Creating(),
			},
			expectedReady: false,
			expectedError: false,
		},
		{
			name:         "Instance not ready (Failed)",
			instanceName: "test-instance",
			instanceConditions: []xpv1.Condition{
				{
					Type:   xpv1.TypeReady,
					Status: corev1.ConditionFalse,
					Reason: xpv1.ReasonReconcileError,
				},
			},
			expectedReady: false,
			expectedError: true,
		},
		{
			name:         "Instance ready",
			instanceName: "test-instance",
			instanceConditions: []xpv1.Condition{
				xpv1.Available(),
			},
			expectedReady: true,
			expectedError: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var fakeClient client.Client

			if test.instanceConditions != nil {
				instance := &crossplaneaws.DBInstance{
					ObjectMeta: metav1.ObjectMeta{
						Name: test.instanceName,
					},
				}
				instance.SetConditions(test.instanceConditions...)
				fakeClient = createFakeClientWithObjects(instance)
			} else {
				// Create an empty fake client if instance is not found
				fakeClient = createFakeClientWithObjects()
			}

			provider := &AWSProvider{
				k8sClient: fakeClient,
			}

			ready, err := provider.isDBInstanceReady(ctx, test.instanceName)

			if test.expectedError {
				if err == nil {
					t.Errorf("[%s] expected error but got nil", test.name)
				}
			} else {
				if err != nil {
					t.Errorf("[%s] unexpected error: %v", test.name, err)
				}
				if ready != test.expectedReady {
					t.Errorf("[%s] expected ready: %v, got: %v", test.name, test.expectedReady, ready)
				}
			}
		})
	}
}

func TestIsDBClusterReady(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name              string
		clusterName       string
		clusterConditions []xpv1.Condition
		expectedReady     bool
		expectedError     bool
	}{
		{
			name:          "Cluster not found",
			clusterName:   "non-existent-cluster",
			expectedReady: false,
			expectedError: false,
		},
		{
			name:        "Cluster not ready (Creating)",
			clusterName: "test-cluster",
			clusterConditions: []xpv1.Condition{
				xpv1.Creating(),
			},
			expectedReady: false,
			expectedError: false,
		},
		{
			name:        "Cluster not ready (Failed)",
			clusterName: "test-cluster",
			clusterConditions: []xpv1.Condition{
				{
					Type:   xpv1.TypeReady,
					Status: corev1.ConditionFalse,
					Reason: xpv1.ReasonReconcileError,
				},
			},
			expectedReady: false,
			expectedError: true,
		},
		{
			name:        "Cluster ready",
			clusterName: "test-cluster",
			clusterConditions: []xpv1.Condition{
				xpv1.Available(),
			},
			expectedReady: true,
			expectedError: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var fakeClient client.Client

			if test.clusterConditions != nil {
				cluster := &crossplaneaws.DBCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name: test.clusterName,
					},
				}
				cluster.SetConditions(test.clusterConditions...)
				fakeClient = createFakeClientWithObjects(cluster)
			} else {
				// Create an empty fake client if cluster is not found
				fakeClient = createFakeClientWithObjects()
			}

			provider := &AWSProvider{
				k8sClient: fakeClient,
			}

			ready, err := provider.isDBClusterReady(ctx, test.clusterName)

			if test.expectedError {
				if err == nil {
					t.Errorf("[%s] expected error but got nil", test.name)
				}
			} else {
				if err != nil {
					t.Errorf("[%s] unexpected error: %v", test.name, err)
				}
				if ready != test.expectedReady {
					t.Errorf("[%s] expected ready: %v, got: %v", test.name, test.expectedReady, ready)
				}
			}
		})
	}
}

func TestConfigureDBTags(t *testing.T) {
	tests := []struct {
		name         string
		inputSpec    *DatabaseSpec
		expectedTags []ProviderTag
		backupPolicy string
	}{
		{
			name: "No backup policy provided, use default",
			inputSpec: &DatabaseSpec{
				Tags: []ProviderTag{
					{Key: "env", Value: "production"},
				},
			},
			expectedTags: []ProviderTag{
				{Key: "env", Value: "production"},
				OperationalTAGActive,
				{Key: BackupPolicyKey, Value: "default-policy"},
			},
			backupPolicy: "default-policy",
		},
		{
			name: "Backup policy provided",
			inputSpec: &DatabaseSpec{
				BackupPolicy: "custom-policy",
				Tags: []ProviderTag{
					{Key: "team", Value: "devops"},
				},
			},
			expectedTags: []ProviderTag{
				{Key: "team", Value: "devops"},
				OperationalTAGActive,
				{Key: BackupPolicyKey, Value: "custom-policy"},
			},
			backupPolicy: "custom-policy",
		},
		{
			name: "Empty tags, only required tags applied",
			inputSpec: &DatabaseSpec{
				BackupPolicy: "retention-30-days",
			},
			expectedTags: []ProviderTag{
				OperationalTAGActive,
				{Key: BackupPolicyKey, Value: "retention-30-days"},
			},
			backupPolicy: "retention-30-days",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mockConfig := viper.New()
			mockConfig.Set("defaultBackupPolicyValue", test.backupPolicy)
			provider := &AWSProvider{config: mockConfig}

			provider.configureDBTags(test.inputSpec)

			if !CompareTags(test.inputSpec.Tags, test.expectedTags) {
				t.Errorf("[%s] expected tags: %+v, got: %+v",
					test.name, test.expectedTags, test.inputSpec.Tags)
			}
		})
	}
}

func TestIsInactiveAtProvider(t *testing.T) {
	tests := []struct {
		name     string
		tags     []*crossplaneaws.Tag
		expected bool
	}{
		{
			name: "Tag present",
			tags: []*crossplaneaws.Tag{
				{Key: ptr.To("operational-status"), Value: ptr.To("inactive")},
			},
			expected: true,
		},
		{
			name: "Tag key matches but value differs",
			tags: []*crossplaneaws.Tag{
				{Key: ptr.To("operational-status"), Value: ptr.To("active")},
			},
			expected: false,
		},
		{
			name: "Tag value matches but key differs",
			tags: []*crossplaneaws.Tag{
				{Key: ptr.To("status"), Value: ptr.To("inactive")},
			},
			expected: false,
		},
		{
			name:     "Empty tag list",
			tags:     []*crossplaneaws.Tag{},
			expected: false,
		},
		{
			name:     "Nil tag list",
			tags:     nil,
			expected: false,
		},
		{
			name: "Multiple tags, with inactive tag present",
			tags: []*crossplaneaws.Tag{
				{Key: ptr.To("random-key"), Value: ptr.To("random-value")},
				{Key: ptr.To("operational-status"), Value: ptr.To("inactive")},
				{Key: ptr.To("another-key"), Value: ptr.To("another-value")},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isInactiveAtProvider(tt.tags)
			if result != tt.expected {
				t.Errorf("[%s] expected isInactiveAtProvider %v, got %v", tt.name, tt.expected, result)
			}
		})
	}
}

func TestChangeToInactive(t *testing.T) {
	tests := []struct {
		name     string
		tags     []*crossplaneaws.Tag
		expected []*crossplaneaws.Tag
	}{
		{
			name: "Tag already inactive",
			tags: []*crossplaneaws.Tag{
				{Key: ptr.To("operational-status"), Value: ptr.To("inactive")},
			},
			expected: []*crossplaneaws.Tag{
				{Key: ptr.To("operational-status"), Value: ptr.To("inactive")},
			},
		},
		{
			name: "Change active to inactive",
			tags: []*crossplaneaws.Tag{
				{Key: ptr.To("operational-status"), Value: ptr.To("active")},
			},
			expected: []*crossplaneaws.Tag{
				{Key: ptr.To("operational-status"), Value: ptr.To("inactive")},
			},
		},
		{
			name: "Add inactive tag when no operational-status key exists",
			tags: []*crossplaneaws.Tag{
				{Key: ptr.To("some-key"), Value: ptr.To("some-value")},
			},
			expected: []*crossplaneaws.Tag{
				{Key: ptr.To("some-key"), Value: ptr.To("some-value")},
				{Key: ptr.To("operational-status"), Value: ptr.To("inactive")},
			},
		},
		{
			name: "Empty tag list, add inactive tag",
			tags: []*crossplaneaws.Tag{},
			expected: []*crossplaneaws.Tag{
				{Key: ptr.To("operational-status"), Value: ptr.To("inactive")},
			},
		},
		{
			name: "Nil tag list, add inactive tag",
			tags: nil,
			expected: []*crossplaneaws.Tag{
				{Key: ptr.To("operational-status"), Value: ptr.To("inactive")},
			},
		},
		{
			name: "Multiple tags, active tag present and changed",
			tags: []*crossplaneaws.Tag{
				{Key: ptr.To("random-key"), Value: ptr.To("random-value")},
				{Key: ptr.To("operational-status"), Value: ptr.To("active")},
				{Key: ptr.To("another-key"), Value: ptr.To("another-value")},
			},
			expected: []*crossplaneaws.Tag{
				{Key: ptr.To("random-key"), Value: ptr.To("random-value")},
				{Key: ptr.To("operational-status"), Value: ptr.To("inactive")}, // Active changed to inactive
				{Key: ptr.To("another-key"), Value: ptr.To("another-value")},
			},
		},
		{
			name: "Multiple tags, no operational-status tag, should add inactive",
			tags: []*crossplaneaws.Tag{
				{Key: ptr.To("random-key"), Value: ptr.To("random-value")},
				{Key: ptr.To("another-key"), Value: ptr.To("another-value")},
			},
			expected: []*crossplaneaws.Tag{
				{Key: ptr.To("random-key"), Value: ptr.To("random-value")},
				{Key: ptr.To("another-key"), Value: ptr.To("another-value")},
				{Key: ptr.To("operational-status"), Value: ptr.To("inactive")}, // Inactive tag added
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := changeToInactive(tt.tags)

			for i, expectedTag := range tt.expected {
				if !reflect.DeepEqual(result[i], expectedTag) {
					t.Errorf("Unexpected tag at index %d: expected %v, actual %v", i, expectedTag, result[i])
				}
			}
		})
	}
}
