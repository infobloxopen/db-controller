package controller

import (
	"context"
	"path/filepath"

	"github.com/crossplane-contrib/provider-aws/apis/rds/v1alpha1"
	persistencev1 "github.com/infobloxopen/db-controller/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/viper"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// Helper function to create string pointers
func strPtr(s string) *string {
	return &s
}

var _ = Describe("DBInstance Labels Management with envtest", func() {
	var (
		testEnv    *envtest.Environment
		k8sClient  client.Client
		testLogger = zap.New(zap.UseDevMode(true))
		ctx        context.Context
		cancel     context.CancelFunc
	)

	BeforeEach(func() {
		// Set up a new context and environment for each test
		ctx, cancel = context.WithCancel(context.Background())
		testEnv = &envtest.Environment{
			CRDDirectoryPaths: []string{
				filepath.Join("..", "..", "config", "crd", "bases"),
				filepath.Join("..", "..", "test", "crd"),
			},
			ErrorIfCRDPathMissing: true,
		}

		// Start the test environment
		cfg, err := testEnv.Start()
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg).NotTo(BeNil())

		// Initialize the Kubernetes client
		k8sClient, err = client.New(cfg, client.Options{})
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient).NotTo(BeNil())

		// Register custom resource schemas
		err = v1alpha1.SchemeBuilder.AddToScheme(scheme.Scheme)
		Expect(err).NotTo(HaveOccurred())

		err = persistencev1.AddToScheme(scheme.Scheme)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		cancel()
		Expect(testEnv.Stop()).To(Succeed())
	})

	It("Job should replicate labels from DatabaseClaim to DBInstance", func() {
		dbClaim := &persistencev1.DatabaseClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "redirect-dbapi",
				Namespace: "default",
				Labels: map[string]string{
					"app.kubernetes.io/component": "database",
					"app.kubernetes.io/instance":  "redirect",
					"app.kubernetes.io/name":      "redirect",
				},
			},
		}
		Expect(k8sClient.Create(ctx, dbClaim)).To(Succeed())

		dbInstance := &v1alpha1.DBInstance{
			ObjectMeta: metav1.ObjectMeta{
				Name: "box-3-redirect-dbapi-2f2d3cd1",
				Labels: map[string]string{
					"existing-label": "unchanged",
				},
			},
			Spec: v1alpha1.DBInstanceSpec{
				ForProvider: v1alpha1.DBInstanceParameters{
					DBInstanceClass: strPtr("db.t3.micro"),
					Engine:          strPtr("postgres"),
				},
			},
		}
		Expect(k8sClient.Create(ctx, dbInstance)).To(Succeed())

		// Execute the SyncDBInstances function.
		viperInstance := viper.New()
		viperInstance.Set("env", "box-3")
		err := SyncDBInstances(ctx, viperInstance, k8sClient, testLogger)
		Expect(err).NotTo(HaveOccurred())

		// Verify that labels were propagated to the DBInstance
		updatedDBInstance := &v1alpha1.DBInstance{}
		Eventually(func() map[string]string {
			_ = k8sClient.Get(ctx, client.ObjectKey{Name: "box-3-redirect-dbapi-2f2d3cd1"}, updatedDBInstance)
			return updatedDBInstance.Labels
		}, "10s", "1s").Should(HaveKeyWithValue("app.kubernetes.io/component", "database"))
		Eventually(func() map[string]string {
			_ = k8sClient.Get(ctx, client.ObjectKey{Name: "box-3-redirect-dbapi-2f2d3cd1"}, updatedDBInstance)
			return updatedDBInstance.Labels
		}, "10s", "1s").Should(HaveKeyWithValue("app.kubernetes.io/instance", "redirect"))
		Eventually(func() map[string]string {
			_ = k8sClient.Get(ctx, client.ObjectKey{Name: "box-3-redirect-dbapi-2f2d3cd1"}, updatedDBInstance)
			return updatedDBInstance.Labels
		}, "10s", "1s").Should(HaveKeyWithValue("app.kubernetes.io/name", "redirect"))
		Eventually(func() map[string]string {
			_ = k8sClient.Get(ctx, client.ObjectKey{Name: "box-3-redirect-dbapi-2f2d3cd1"}, updatedDBInstance)
			return updatedDBInstance.Labels
		}, "10s", "1s").Should(HaveKeyWithValue("existing.label", "unchanged"))
	})

})
