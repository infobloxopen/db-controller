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

// Helper function to create string pointers.
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
		ctx, cancel = context.WithCancel(context.Background())
		testEnv = &envtest.Environment{
			CRDDirectoryPaths: []string{
				filepath.Join("..", "..", "config", "crd", "bases"),
				filepath.Join("..", "..", "test", "crd"),
			},
			ErrorIfCRDPathMissing: true,
		}

		cfg, err := testEnv.Start()
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg).NotTo(BeNil())

		k8sClient, err = client.New(cfg, client.Options{})
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient).NotTo(BeNil())

		err = v1alpha1.SchemeBuilder.AddToScheme(scheme.Scheme)
		Expect(err).NotTo(HaveOccurred())

		err = persistencev1.AddToScheme(scheme.Scheme)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		cancel()
		Expect(testEnv.Stop()).To(Succeed())
	})

	It("Should create or update dbclaim-name and dbclaim-namespace labels in DBInstance", func() {
		// The idea is that if the DBInstance is missing labels,
		// the function creates the labels "test-dbinstance"/"default".
		// To avoid an error, we need to have a DatabaseClaim named "test-dbinstance".

		dbClaim := &persistencev1.DatabaseClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-dbinstance",
				Namespace: "default",
			},
			Spec: persistencev1.DatabaseClaimSpec{
				DatabaseName: "sample-app",
				SecretName:   "test-secret",
			},
		}
		Expect(k8sClient.Create(ctx, dbClaim)).To(Succeed())

		dbInstance := &v1alpha1.DBInstance{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-dbinstance",
			},
			Spec: v1alpha1.DBInstanceSpec{
				ForProvider: v1alpha1.DBInstanceParameters{
					DBInstanceClass: strPtr("db.t3.micro"),
					Engine:          strPtr("postgres"),
				},
			},
		}
		Expect(k8sClient.Create(ctx, dbInstance)).To(Succeed())

		viperInstance := viper.New()
		viperInstance.Set("env", "test-env")

		// Executes the synchronization function
		err := SyncDBInstances(ctx, viperInstance, k8sClient, testLogger)
		Expect(err).NotTo(HaveOccurred())

		// Checks if the labels were added
		updatedDBInstance := &v1alpha1.DBInstance{}
		Eventually(func() map[string]string {
			_ = k8sClient.Get(ctx, client.ObjectKey{Name: "test-dbinstance"}, updatedDBInstance)
			return updatedDBInstance.Labels
		}, "10s", "1s").Should(HaveKeyWithValue("app.kubernetes.io/dbclaim-name", "test-dbinstance"))
		Eventually(func() map[string]string {
			_ = k8sClient.Get(ctx, client.ObjectKey{Name: "test-dbinstance"}, updatedDBInstance)
			return updatedDBInstance.Labels
		}, "10s", "1s").Should(HaveKeyWithValue("app.kubernetes.io/dbclaim-namespace", "default"))
	})

	It("Should not modify DBInstance if dbclaim-name and dbclaim-namespace already exist", func() {
		// Here, we predefine the labels "existing-dbclaim"/"existing-namespace"
		// and want to avoid a "not found" error. Therefore, we create a DatabaseClaim
		// with this name/namespace.

		existingDBClaim := &persistencev1.DatabaseClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "existing-dbclaim",
				Namespace: "existing-namespace",
			},
			Spec: persistencev1.DatabaseClaimSpec{
				DatabaseName: "some-db",
				SecretName:   "some-secret",
			},
		}
		Expect(k8sClient.Create(ctx, existingDBClaim)).To(Succeed())

		dbInstance := &v1alpha1.DBInstance{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-dbinstance-existing",
				Labels: map[string]string{
					"app.kubernetes.io/dbclaim-name":      "existing-dbclaim",
					"app.kubernetes.io/dbclaim-namespace": "existing-namespace",
					"existing.label":                      "unchanged",
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

		viperInstance := viper.New()
		viperInstance.Set("env", "test-env")

		// Executes the synchronization function.
		err := SyncDBInstances(ctx, viperInstance, k8sClient, testLogger)
		Expect(err).NotTo(HaveOccurred())

		// Verifies if the labels were not overwritten.
		updatedDBInstance := &v1alpha1.DBInstance{}
		Eventually(func() map[string]string {
			_ = k8sClient.Get(ctx, client.ObjectKey{Name: "test-dbinstance-existing"}, updatedDBInstance)
			return updatedDBInstance.Labels
		}, "10s", "1s").Should(HaveKeyWithValue("app.kubernetes.io/dbclaim-name", "existing-dbclaim"))
		Eventually(func() map[string]string {
			_ = k8sClient.Get(ctx, client.ObjectKey{Name: "test-dbinstance-existing"}, updatedDBInstance)
			return updatedDBInstance.Labels
		}, "10s", "1s").Should(HaveKeyWithValue("app.kubernetes.io/dbclaim-namespace", "existing-namespace"))

		// Ensures that "existing.label" was preserved.
		Eventually(func() map[string]string {
			_ = k8sClient.Get(ctx, client.ObjectKey{Name: "test-dbinstance-existing"}, updatedDBInstance)
			return updatedDBInstance.Labels
		}, "10s", "1s").Should(HaveKeyWithValue("existing.label", "unchanged"))
	})

	It("Should return an error if DatabaseClaim is missing", func() {
		// Here, we do not manually define labels,
		// so SyncDBInstances will create labels
		// "dbclaim-name = test-dbinstance-missing-claim"
		// "dbclaim-namespace = default"
		// and try to fetch a DatabaseClaim with this name/namespace,
		// which does not exist.

		dbInstance := &v1alpha1.DBInstance{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-dbinstance-missing-claim",
			},
			Spec: v1alpha1.DBInstanceSpec{
				ForProvider: v1alpha1.DBInstanceParameters{
					DBInstanceClass: strPtr("db.t3.micro"),
					Engine:          strPtr("postgres"),
				},
			},
		}
		Expect(k8sClient.Create(ctx, dbInstance)).To(Succeed())

		viperInstance := viper.New()
		viperInstance.Set("env", "test-env")

		// Expects an error, since the DatabaseClaim does not exist.
		err := SyncDBInstances(ctx, viperInstance, k8sClient, testLogger)
		Expect(err).To(HaveOccurred())

		// Instead of checking "labels missing", verifies the actual error message.
		Expect(err.Error()).To(ContainSubstring("DatabaseClaim not found for DBInstance"))
	})

})
