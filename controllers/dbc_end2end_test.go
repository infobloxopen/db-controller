/* This file contains end to end tests for db-controller. It tests the following scenarios
* 1. Create a postgres RDS using a dbclaim
* 2. Delete the dbclaim and associated dbinstances.crd
* 3. Use existing RDS
* 4. Migrate Use Existing RDS to a local RDS
* 5. Migrate postgres RDS to Aurora RDS
* The tests are run in box-3 cluster. The tests are skipped if the cluster is not box-3
* It runs in the namespace specified in .id + e2e file in the root directory (eg: bjeevan-e2e)
* The tests create RDS resources in AWS. The resources are cleaned up after the tests are complete.
* At this time these tests can be run manually only using:
* make integration-test
*
 */

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
	timeout_e2e  = time.Minute * 20
	interval_e2e = time.Second * 30
)

var (
	falseVal               = false
	namespace              string
	db1                    string
	newdbcMasterSecretName string
	rds1                   string
	db2                    string
	db3                    string
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
		db1 = namespace + "-db-1"
		db2 = namespace + "-db-2"
		db3 = namespace + "-db-3"
		rds1 = "box-3-" + db1 + "-1ec9b27c"
		newdbcMasterSecretName = rds1 + "-master"
		createNamespace()
	})

	logf.Log.Info("Starting test", "timeout", timeout_e2e, "interval", interval_e2e)

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
		//delete
		key := types.NamespacedName{
			Name:      db2,
			Namespace: namespace,
		}
		db2Claim := &persistancev1.DatabaseClaim{}
		By("Getting the existing dbclaim")
		e2e_k8sClient.Get(ctx, key, db2Claim)
		e2e_k8sClient.Delete(ctx, db2Claim)

		cleanupdb(db1)
		cleanupdb(db2)
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
		Name:      db2,
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
	db2Claim := &persistancev1.DatabaseClaim{}
	By("Getting the existing dbclaim")
	Expect(e2e_k8sClient.Get(ctx, key, db2Claim)).Should(Succeed())
	By("Updating type from postgres to aurora-postgresql in the claim")
	db2Claim.Spec.Type = "aurora-postgresql"
	// db2Claim.Spec.DeletionPolicy = "delete"
	Expect(e2e_k8sClient.Update(ctx, db2Claim)).Should(Succeed())
	// time.Sleep(time.Minute * 5)
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
	}, time.Minute*20, interval_e2e).Should(Equal(expectedState))
	//check if eventually the secret sample-secret is created
	By("checking if the secret is created")
	//box-3-bjeevan-e2e-db-2-bb1e7196
	Eventually(func() (string, error) {
		secret := &corev1.Secret{}
		err := e2e_k8sClient.Get(ctx, types.NamespacedName{Name: "sample-secret", Namespace: namespace}, secret)
		if err != nil {
			return "", err
		}
		return string(secret.Data["hostname"]), nil
	}, time.Minute*20, interval_e2e).Should(ContainSubstring("box-3-" + db2 + "-bb1e7196"))
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
	createdDbClaim := &persistancev1.DatabaseClaim{}
	By("checking dbclaim status is ready")
	Eventually(func() (persistancev1.DbState, error) {
		err := e2e_k8sClient.Get(ctx, key, createdDbClaim)
		if err != nil {
			return "", err
		}
		return createdDbClaim.Status.ActiveDB.DbState, nil
	}, timeout_e2e, interval_e2e).Should(Equal(persistancev1.Ready))
	//check if eventually the secret sample-secret is created
	By("checking if the secret is created and host name contains 1ec9b27c")
	//box-3-end2end-test-2-dbclaim-1ec9b27c
	Eventually(func() error {
		return e2e_k8sClient.Get(ctx, types.NamespacedName{Name: "sample-secret", Namespace: namespace}, &corev1.Secret{})
	}, timeout_e2e, interval_e2e).Should(BeNil())

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
			DeletionPolicy:        "delete",
			SourceDataFrom: &persistancev1.SourceDataFrom{
				Type: persistancev1.SourceDataType("database"),
				Database: &persistancev1.Database{
					DSN: "postgres://root@" + rds1 + ".cpwy0kesdxhx.us-east-1.rds.amazonaws.com:5432/sample_db?sslmode=require",
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
		Name:      db1,
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
	}, timeout_e2e, time.Second*5).Should(Succeed())
}

func createPostgresRDSTest() {
	key := types.NamespacedName{
		Name:      db1,
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
	}, timeout_e2e, interval_e2e).Should(Equal(persistancev1.Ready))
	By("checking if the secret is created")
	Eventually(func() error {
		return e2e_k8sClient.Get(ctx, types.NamespacedName{Name: "newdb-secret", Namespace: namespace}, &corev1.Secret{})
	}, timeout_e2e, interval_e2e).Should(BeNil())
}

func cleanupdb(db string) {
	key := types.NamespacedName{
		Name:      db,
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
			DeletionPolicy:        "delete",
		},
	}
	e2e_k8sClient.Create(ctx, dbClaim)
	time.Sleep(time.Minute * 1)
	e2e_k8sClient.Delete(ctx, dbClaim)
}

func createNamespace() {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}
	e2e_k8sClient.Create(ctx, ns)
}
