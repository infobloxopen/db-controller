package providers

import (
	"context"
	crossplaneaws "github.com/crossplane-contrib/provider-aws/apis/rds/v1alpha1"
	v1 "github.com/infobloxopen/db-controller/api/v1"
	"github.com/infobloxopen/db-controller/pkg/config"
	"github.com/infobloxopen/db-controller/pkg/hostparams"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/viper"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"path/filepath"
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

var _ = Describe("AWSProvider createPostgres", func() {
	var (
		provider *AWSProvider
		ctx      context.Context
	)

	BeforeEach(func() {
		ctx = context.TODO()
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

	It("Should create postgres DBInstance with correct parameters", func() {
		spec := DatabaseSpec{
			ResourceName: "env-app-name-db-1d9fb876",
			HostParams: hostparams.HostParams{
				Shape:        "db.t3.medium",
				MinStorageGB: 20,
				DBVersion:    "15.7",
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

		_, err := provider.CreateDatabase(ctx, spec)
		Expect(err).ToNot(HaveOccurred())

		paramGroup := &crossplaneaws.DBInstance{}
		err = k8sClient.Get(ctx, types.NamespacedName{Name: "env-app-name-db-1d9fb876"}, paramGroup)
		Expect(err).ToNot(HaveOccurred())
	})

	It("Should create Aurora cluster and instances with correct parameters", func() {
		spec := DatabaseSpec{
			ResourceName: "env-app-name-db-1d9fb877",
			HostParams: hostparams.HostParams{
				Shape:        "db.t3.medium",
				MinStorageGB: 20,
				DBVersion:    "15.7",
			},
			DbType:       "aurora-postgresql",
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

		_, err := provider.CreateDatabase(ctx, spec)
		Expect(err).ToNot(HaveOccurred())

		paramGroup := &crossplaneaws.DBInstance{}
		err = k8sClient.Get(ctx, types.NamespacedName{Name: "env-app-name-db-1d9fb877"}, paramGroup)
		Expect(err).ToNot(HaveOccurred())
	})
})
