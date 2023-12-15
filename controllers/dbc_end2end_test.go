package controllers

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	timeout  = time.Minute * 10
	interval = time.Second * 30
)

var (
	falseVal               = false
	namespace              string
	newdbcName             string
	newdbcMasterSecretName string
	db1                    string
	db2                    string
	ctx                    = context.Background()
	// dBHostname             string
)

var _ = Describe("db-controller end to end testing", Label("integration"), Ordered, func() {
	var _ = BeforeAll(func() {
		loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
		kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, &clientcmd.ConfigOverrides{})

		rawConfig, err := kubeConfig.RawConfig()
		if err != nil {
			// if we can't read the kubeconfig, we can't run the test
			Skip(fmt.Sprintf("This test can only run in %s. Current context is %s", "box-3", "unable to read kubeconfig"))
		}

		if !strings.Contains(rawConfig.CurrentContext, "box-3") {
			Skip(fmt.Sprintf("This test can only run in %s. Current context is %s", "box-3", rawConfig.CurrentContext))
		}
		//	integration tests

		By("bootstrapping test environment")
		e2e_testEnv = &envtest.Environment{
			UseExistingCluster: &trueVal,
		}

		e2e_cfg, err = e2e_testEnv.Start()
		Expect(err).ToNot(HaveOccurred())
		Expect(e2e_cfg).ToNot(BeNil())

		err = persistancev1.AddToScheme(scheme.Scheme)
		Expect(err).NotTo(HaveOccurred())

		// +kubebuilder:scaffold:scheme

		e2e_k8sClient, err = client.New(e2e_cfg, client.Options{Scheme: scheme.Scheme})
		Expect(err).ToNot(HaveOccurred())
		Expect(e2e_k8sClient).ToNot(BeNil())

		data, err := os.ReadFile("../.id")
		Expect(err).ToNot(HaveOccurred())
		namespace = strings.TrimSpace(string(data)) + "-e2e"
		newdbcName = namespace + "-db-1"
		db1 = "box-3-" + newdbcName + "-1ec9b27c"
		newdbcMasterSecretName = db1 + "-master"
		db2 = namespace + "-db-2"
		createNamespace()
	})

	logf.Log.Info("Starting test", "timeout", timeout, "interval", interval)

	Context("Creating a Postgres RDS using a dbclaim ", func() {
		It("should create a RDS in AWS", func() {
			By("creating a new DB Claim")
			createPostgresRDSTest()
		})
	})
	Context("Delete RDS", func() {
		It("should delete dbinstances.crds", func() {
			By("deleting the dbc and associated dbinstances.crd")
			deletePostgresRDSTest()
		})
	})
	Context("Use Existing RDS", func() {
		It("should use Existing RDS", func() {
			By("setting up master secret to access existing RDS")
			setupMasterSecretForExistingRDS()
			By("successfully processing an useExisting dbClaim")
			UseExistingPostgresRDSTest()
		})
	})
	Context("Migrate Use Existing RDS to a local RDS", func() {
		It("should create a new RDS and migrate Existing database", func() {
			By("Removing the useExistingFlag in dbclaim")
			MigrateUseExistingToNewRDS()
		})
	})
	Context("Migrate postgres RDS to Aurora RDS", func() {
		It("should create a new RDS and migrate postgres sample_db to new RDS", func() {
			MigratePostgresToAuroraRDS()
		})
	})

	var _ = AfterAll(func() {
		//delete master secret if it exists
		By("deleting the master secret")
		e2e_k8sClient.Delete(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      newdbcMasterSecretName,
				Namespace: "db-controller",
			},
		})

		//delete the namespace
		By("deleting the namespace")
		e2e_k8sClient.Delete(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		})

		By("tearing down the test environment")
		if e2e_testEnv != nil {
			err := e2e_testEnv.Stop()
			Expect(err).ToNot(HaveOccurred())
		}
	})
})

func MigratePostgresToAuroraRDS() {
	key := types.NamespacedName{
		Name:      "end2end-test-2-dbclaim",
		Namespace: namespace,
	}
	type testState struct {
		DbState        persistancev1.DbState
		MigrationState string
		Type           persistancev1.DatabaseType
	}
	expectedState := testState{
		DbState:        persistancev1.Ready,
		MigrationState: "completed",
		Type:           "aurora-postgresql",
	}
	existingDbClaim := &persistancev1.DatabaseClaim{}
	By("Getting the existing dbclaim")
	Expect(e2e_k8sClient.Get(ctx, key, existingDbClaim)).Should(Succeed())
	By("Updating type from postgres to aurora-postgresql in the claim")
	existingDbClaim.Spec.Type = "aurora-postgresql"
	Expect(e2e_k8sClient.Update(ctx, existingDbClaim)).Should(Succeed())
	time.Sleep(time.Minute * 5)
	createdDbClaim := &persistancev1.DatabaseClaim{}
	By("checking dbclaim status is ready")
	By("checking dbclaim status type is aurora-postgresql")
	Eventually(func() (testState, error) {
		err := e2e_k8sClient.Get(ctx, key, createdDbClaim)
		if err != nil {
			return testState{}, err
		}
		currentState := testState{
			DbState:        createdDbClaim.Status.ActiveDB.DbState,
			MigrationState: createdDbClaim.Status.MigrationState,
			Type:           createdDbClaim.Status.ActiveDB.Type,
		}
		return currentState, nil
	}, time.Minute*15, interval).Should(Equal(expectedState))
	//check if eventually the secret sample-secret is created
	By("checking if the secret is created")
	//box-3-end2end-test-2-dbclaim-bb1e7196
	Eventually(func() (string, error) {
		secret := &corev1.Secret{}
		err := e2e_k8sClient.Get(ctx, types.NamespacedName{Name: "sample-secret", Namespace: namespace}, secret)
		if err != nil {
			return "", err
		}
		return string(secret.Data["hostname"]), nil
	}, time.Minute*15, interval).Should(ContainSubstring("box-3-end2end-test-2-dbclaim-bb1e7196"))
}

func MigrateUseExistingToNewRDS() {
	key := types.NamespacedName{
		Name:      db2,
		Namespace: namespace,
	}
	existingDbClaim := &persistancev1.DatabaseClaim{}
	By("Getting the existing dbclaim")
	Expect(e2e_k8sClient.Get(ctx, key, existingDbClaim)).Should(Succeed())
	By("Removing the useExistingFlag in dbclaim")
	existingDbClaim.Spec.UseExistingSource = &falseVal
	existingDbClaim.Spec.SourceDataFrom = nil
	Expect(e2e_k8sClient.Update(ctx, existingDbClaim)).Should(Succeed())
	time.Sleep(time.Minute * 5)
	createdDbClaim := &persistancev1.DatabaseClaim{}
	By("checking dbclaim status is ready")
	Eventually(func() (persistancev1.DbState, error) {
		err := e2e_k8sClient.Get(ctx, key, createdDbClaim)
		if err != nil {
			return "", err
		}
		return createdDbClaim.Status.ActiveDB.DbState, nil
	}, time.Minute*15, interval).Should(Equal(persistancev1.Ready))
	//check if eventually the secret sample-secret is created
	By("checking if the secret is created and host name contains 1ec9b27c")
	//box-3-end2end-test-2-dbclaim-1ec9b27c
	Eventually(func() error {
		return e2e_k8sClient.Get(ctx, types.NamespacedName{Name: "sample-secret", Namespace: namespace}, &corev1.Secret{})
	}, time.Minute*15, interval).Should(BeNil())

}

func UseExistingPostgresRDSTest() {
	key := types.NamespacedName{
		Name:      db2,
		Namespace: namespace,
	}
	dbClaim := &persistancev1.DatabaseClaim{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "persistance.atlas.infoblox.com/v1",
			Kind:       "DatabaseClaim",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      key.Name,
			Namespace: key.Namespace,
		},
		Spec: persistancev1.DatabaseClaimSpec{
			AppID:                 "sample-app",
			DatabaseName:          "sample_db",
			SecretName:            "sample-secret",
			Username:              "sample_user",
			Type:                  "postgres",
			DSNName:               "dsn",
			EnableReplicationRole: &falseVal,
			UseExistingSource:     &trueVal,
			DBVersion:             "15.3",
			SourceDataFrom: &persistancev1.SourceDataFrom{
				Type: persistancev1.SourceDataType("database"),
				Database: &persistancev1.Database{
					DSN: "postgres://root@" + db1 + ".cpwy0kesdxhx.us-east-1.rds.amazonaws.com:5432/sample_db?sslmode=require",
					SecretRef: &persistancev1.SecretRef{
						Name:      "existing-db-master-secret",
						Namespace: namespace,
					},
				},
			},
		},
	}
	Expect(e2e_k8sClient.Create(ctx, dbClaim)).Should(Succeed())
	// time.Sleep(time.Minute * 5)
	createdDbClaim := &persistancev1.DatabaseClaim{}
	By("checking dbclaim status is use-existing-db")
	Eventually(func() (persistancev1.DbState, error) {
		err := e2e_k8sClient.Get(ctx, key, createdDbClaim)
		if err != nil {
			return "", err
		}
		return createdDbClaim.Status.ActiveDB.DbState, nil
	}, time.Minute*2, time.Second*15).Should(Equal(persistancev1.UsingExistingDB))
	//check if eventually the secret sample-secret is created
	By("checking if the secret is created")
	Eventually(func() error {
		return e2e_k8sClient.Get(ctx, types.NamespacedName{Name: "sample-secret", Namespace: namespace}, &corev1.Secret{})
	}, time.Minute*2, time.Second*15).Should(BeNil())
}

func setupMasterSecretForExistingRDS() {
	//copy secret from prev dbclaim and use it as master secret for existing rds usecase
	key := types.NamespacedName{
		Name:      newdbcMasterSecretName,
		Namespace: "db-controller",
	}
	newDBMasterSecret := &corev1.Secret{}
	By("Reading the prev secret")
	Expect(e2e_k8sClient.Get(ctx, key, newDBMasterSecret)).Should(Succeed())
	Expect(newDBMasterSecret.Data["password"]).NotTo(BeNil())
	By("Creating the master secret")
	existingDBMasterSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "existing-db-master-secret",
			Namespace: namespace,
		},
		Data: map[string][]byte{
			"password": []byte(newDBMasterSecret.Data["password"]),
		},
	}

	Expect(e2e_k8sClient.Create(ctx, existingDBMasterSecret)).Should(Succeed())
}

func deletePostgresRDSTest() {
	key := types.NamespacedName{
		Name:      newdbcName,
		Namespace: namespace,
	}
	prevDbClaim := &persistancev1.DatabaseClaim{}
	By("Getting the prev dbclaim")
	Expect(e2e_k8sClient.Get(ctx, key, prevDbClaim)).Should(Succeed())
	By("Deleting the dbclaim")
	Expect(e2e_k8sClient.Delete(ctx, prevDbClaim)).Should(Succeed())
	// time.Sleep(time.Minute * 5)
	By("checking dbclaim does not exists")
	Eventually(func() error {
		err := e2e_k8sClient.Get(ctx, key, prevDbClaim)
		if err != nil {
			if errors.IsNotFound(err) {
				return nil
			}
			return err
		} else {
			return fmt.Errorf("dbclaim still exists")
		}
	}, timeout, time.Second*5).Should(Succeed())
}

func createPostgresRDSTest() {
	key := types.NamespacedName{
		Name:      newdbcName,
		Namespace: namespace,
	}
	dbClaim := &persistancev1.DatabaseClaim{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "persistance.atlas.infoblox.com/v1",
			Kind:       "DatabaseClaim",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      key.Name,
			Namespace: key.Namespace,
		},
		Spec: persistancev1.DatabaseClaimSpec{
			AppID:                 "sample-app",
			DatabaseName:          "sample_db",
			SecretName:            "newdb-secret",
			Username:              "sample_user",
			Type:                  "postgres",
			DSNName:               "dsn",
			EnableReplicationRole: &falseVal,
			UseExistingSource:     &falseVal,
			DBVersion:             "15.3",
		},
	}
	Expect(e2e_k8sClient.Create(ctx, dbClaim)).Should(Succeed())
	// time.Sleep(time.Minute * 5)
	createdDbClaim := &persistancev1.DatabaseClaim{}
	By("checking dbclaim status is ready")
	Eventually(func() (persistancev1.DbState, error) {
		err := e2e_k8sClient.Get(ctx, key, createdDbClaim)
		if err != nil {
			return "", err
		}
		return createdDbClaim.Status.ActiveDB.DbState, nil
	}, timeout, interval).Should(Equal(persistancev1.Ready))
	By("checking if the secret is created")
	Eventually(func() error {
		return e2e_k8sClient.Get(ctx, types.NamespacedName{Name: "newdb-secret", Namespace: namespace}, &corev1.Secret{})
	}, timeout, interval).Should(BeNil())
}

func createNamespace() {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}
	e2e_k8sClient.Create(ctx, ns)
}
