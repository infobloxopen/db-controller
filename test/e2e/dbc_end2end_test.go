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

package e2e

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
	"github.com/infobloxopen/db-controller/pkg/config"
	"github.com/infobloxopen/db-controller/pkg/hostparams"
	"github.com/infobloxopen/db-controller/test/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/utils/ptr"

	crossplanerds "github.com/crossplane-contrib/provider-aws/apis/rds/v1alpha1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	timeout_e2e  = time.Minute * 20
	interval_e2e = time.Second * 30
)

var (
	newdbcMasterSecretName string
	rds1                   string
	db2                    string
	db3                    string
	ctx                    = context.Background()
	// DBVersion_new          = "15.7"
)

var _ = Describe("AWS", Ordered, func() {

	var (
		// equal to env ie. box-3
		dbIdentifierPrefix string
		db1                string
		dbinstance1        string
		dbroleclaim1       string
		dbroleclaim2       string
	)

	BeforeAll(func() {

		loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
		kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, &clientcmd.ConfigOverrides{})

		rawConfig, err := kubeConfig.RawConfig()
		Expect(err).ToNot(HaveOccurred(), "unable to read kubeconfig")

		env := rawConfig.CurrentContext
		// Check if current context is box-3
		Expect(env).To(Equal("box-3"), "This test can only run in box-3")

		dbIdentifierPrefix = env

		dbroleclaim1 = namespace + "-dbrc-1"
		dbroleclaim2 = namespace + "-dbrc-2"
		db1 = namespace + "-db-1"
		db2 = namespace + "-db-2"
		db3 = namespace + "-db-3"
		rds1 = env + "-" + db1 + "-1d9fb876"
		newdbcMasterSecretName = rds1 + "-master"

		// createNamespace()
	})

	logf.Log.Info("Starting test", "timeout", timeout_e2e, "interval", interval_e2e)

	//creates db_1
	Context("Creating a RDS", func() {

		It("Creating a DBClaim without a dbVersion", func() {

			Expect(db1).NotTo(BeEmpty())
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
					Class:                 &class,
					AppID:                 "sample-app",
					DatabaseName:          "sample_db",
					SecretName:            "newdb-secret",
					Username:              "sample_user",
					Type:                  "postgres",
					DSNName:               "dsn",
					EnableReplicationRole: ptr.To(false),
					UseExistingSource:     ptr.To(false),
				},
			}

			// FIXME: Logic to determine this needs a complete
			// rewrite. Determine the name of the created
			// crossplane dbinstance and make sure it is deleted
			// when the integration test is done.
			// taken from pkg/databaseclaims/getDynamicHostName(*v1.DatabaseClaim)
			{
				wd, err := utils.GetProjectDir()
				Expect(err).ToNot(HaveOccurred())

				viperconfig := config.NewConfig(logger, filepath.Join(wd, "cmd", "config", "config.yaml"))
				hostParams, err := hostparams.New(viperconfig, "", dbClaim)
				Expect(err).ToNot(HaveOccurred())
				dbinstance1 = fmt.Sprintf("%s-%s-%s", dbIdentifierPrefix, db1, hostParams.Hash())
			}
			By("Checking if dbinstance exists")
			Expect(dbinstance1).NotTo(BeEmpty())

			var dbinst crossplanerds.DBInstance
			err := k8sClient.Get(ctx, types.NamespacedName{Name: dbinstance1}, &dbinst)
			Expect(err).To(HaveOccurred())
			Expect(errors.IsNotFound(err)).To(BeTrue())

			Expect(k8sClient.Create(ctx, dbClaim)).Should(Succeed())

			createdDbClaim := &persistancev1.DatabaseClaim{}

			By("status error includes engine version not specified error")
			Eventually(func() (string, error) {

				err := k8sClient.Get(ctx, key, createdDbClaim)
				Expect(err).ToNot(HaveOccurred())
				Expect(createdDbClaim.Spec.DBVersion).To(BeEmpty())

				return createdDbClaim.Status.Error, nil
			}, 50*time.Second, 100*time.Millisecond).Should(Equal(hostparams.ErrEngineVersionNotSpecified.Error()))
		})

		It("Updating a databaseclaim to have an invalid dbVersion", func() {
			By("erroring out when AWS does not support dbVersion")

			key := types.NamespacedName{
				Name:      db1,
				Namespace: namespace,
			}
			invalidVersion := "15.3"
			prevDbClaim := &persistancev1.DatabaseClaim{}
			By("Getting the prev dbclaim")
			Expect(k8sClient.Get(ctx, key, prevDbClaim)).Should(Succeed())
			By(fmt.Sprintf("Updating with version dbVersion: %s", invalidVersion))
			prevDbClaim.Spec.DBVersion = invalidVersion
			Expect(k8sClient.Update(ctx, prevDbClaim)).Should(Succeed())
			updatedDbClaim := &persistancev1.DatabaseClaim{}
			By("Check that .spec.dbVersion is set")
			Expect(k8sClient.Get(ctx, key, updatedDbClaim)).Should(Succeed())
			Expect(updatedDbClaim.Spec.DBVersion).To(Equal(invalidVersion))

			var dbinst crossplanerds.DBInstance
			By(fmt.Sprintf("checking crossplane.dbinstance is created: %s", dbinstance1))
			Eventually(func() error {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: dbinstance1}, &dbinst)
				return err
			}, 60*time.Second, 100*time.Millisecond).Should(BeNil())

			By("checking dbclaim status.error message is not empty")
			Eventually(func() (string, error) {
				err := k8sClient.Get(ctx, key, updatedDbClaim)
				if err != nil {
					return "", err
				}
				return updatedDbClaim.Status.Error, nil
			}, time.Minute*2, time.Second*15).Should(Equal("requested database version(15.3) is not available"))

		})
	})

	//update db_1
	Context("Creating a Postgres RDS using a dbclaim ", func() {
		It("should create a RDS in AWS", func() {
			By("creating a new DB Claim")

			key := types.NamespacedName{
				Name:      db1,
				Namespace: namespace,
			}
			prevDbClaim := &persistancev1.DatabaseClaim{}
			By("Getting the prev dbclaim")
			Expect(k8sClient.Get(ctx, key, prevDbClaim)).Should(Succeed())
			By("Updating dbVersion")
			prevDbClaim.Spec.DBVersion = "15.5"
			Expect(k8sClient.Update(ctx, prevDbClaim)).Should(Succeed())
			updatedDbClaim := &persistancev1.DatabaseClaim{}
			By("checking dbclaim status is ready")
			Eventually(func() (persistancev1.DbState, error) {
				err := k8sClient.Get(ctx, key, updatedDbClaim)
				if err != nil {
					return "", err
				}
				return updatedDbClaim.Status.ActiveDB.DbState, nil
			}, timeout_e2e, interval_e2e).Should(Equal(persistancev1.Ready))
			By("checking if the secret is created")
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: "newdb-secret", Namespace: namespace}, &corev1.Secret{})
			}, timeout_e2e, interval_e2e).Should(BeNil())

		})
	})

	// //update db_1 - create new schema
	Context("Create new schemas, roles and user ecgto-dbrc-1_user_a", func() {
		It("should create new user, schemas and roles", func() {
			By("creating a new DBRoleClaim")
			secretName := "dbroleclaim-secret"

			key := types.NamespacedName{
				Name:      dbroleclaim1,
				Namespace: namespace,
			}
			dbRoleClaim := &persistancev1.DbRoleClaim{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "persistance.atlas.infoblox.com/v1",
					Kind:       "DatabaseClaim",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
				Spec: persistancev1.DbRoleClaimSpec{
					Class:      &class,
					SecretName: secretName,
					SourceDatabaseClaim: &persistancev1.SourceDatabaseClaim{
						Namespace: namespace,
						Name:      db1,
					},
					SchemaRoleMap: map[string]persistancev1.RoleType{
						"schemaapp111": persistancev1.ReadOnly,
						"schemaapp222": persistancev1.Regular,
						"schemaapp333": persistancev1.Admin,
					},
				},
			}

			Expect(k8sClient.Create(ctx, dbRoleClaim)).Should(Succeed())
			By("checking if the secret is created: " + namespace + "." + secretName)
			secret := &corev1.Secret{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: secretName, Namespace: namespace}, secret)
			}, timeout_e2e, interval_e2e).Should(BeNil())

			Expect(string(secret.Data["database"])).Should(Equal("sample_db"))
			Expect(string(secret.Data["dsn"])).ShouldNot(BeNil())
			Expect(string(secret.Data["hostname"])).Should(ContainSubstring("box-3-ecgto-db-1-b8ee12ca"))
			Expect(string(secret.Data["password"])).ShouldNot(BeNil())
			Expect(string(secret.Data["port"])).Should(Equal("5432"))
			Expect(string(secret.Data["sslmode"])).Should(Equal("require"))
			Expect(string(secret.Data["uri_dsn"])).ShouldNot(BeNil())
			Expect(string(secret.Data["username"])).Should(Equal("ecgto-dbrc-1_user_a"))

		})
	})

	Context("Create new schemas, roles and user ecgto-dbrc-2_user_a", func() {
		It("should create new user, schemas and roles", func() {
			By("creating a new DBRoleClaim")
			secretName := "dbroleclaim-other-secret"

			key := types.NamespacedName{
				Name:      dbroleclaim2,
				Namespace: namespace,
			}
			dbRoleClaim := &persistancev1.DbRoleClaim{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "persistance.atlas.infoblox.com/v1",
					Kind:       "DatabaseClaim",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      key.Name,
					Namespace: key.Namespace,
				},
				Spec: persistancev1.DbRoleClaimSpec{
					Class:      &class,
					SecretName: secretName,
					SourceDatabaseClaim: &persistancev1.SourceDatabaseClaim{
						Namespace: namespace,
						Name:      db1,
					},
					SchemaRoleMap: map[string]persistancev1.RoleType{
						"schemaapp444": persistancev1.ReadOnly,
						"schemaapp555": persistancev1.Regular,
						"schemaapp666": persistancev1.Admin,
					},
				},
			}

			Expect(k8sClient.Create(ctx, dbRoleClaim)).Should(Succeed())
			By("checking if the secret is created: " + namespace + "." + secretName)
			secret := &corev1.Secret{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: secretName, Namespace: namespace}, secret)
			}, timeout_e2e, interval_e2e).Should(BeNil())

			Expect(string(secret.Data["database"])).Should(Equal("sample_db"))
			Expect(string(secret.Data["dsn"])).ShouldNot(BeNil())
			Expect(string(secret.Data["hostname"])).Should(ContainSubstring("box-3-ecgto-db-1-b8ee12ca"))
			Expect(string(secret.Data["password"])).ShouldNot(BeNil())
			Expect(string(secret.Data["port"])).Should(Equal("5432"))
			Expect(string(secret.Data["sslmode"])).Should(Equal("require"))
			Expect(string(secret.Data["uri_dsn"])).ShouldNot(BeNil())
			Expect(string(secret.Data["username"])).Should(Equal("ecgto-dbrc-2_user_a"))

		})
	})

	//deletes db_1
	Context("Delete RDS", func() {
		FIt("should delete dbinstances.crds", func() {
			By("deleting the dbc, dbrc and associated dbinstances.crd")

			// ===================================================== DBRoleClaim1
			keyDbRoleClaim1 := types.NamespacedName{
				Name:      dbroleclaim1,
				Namespace: namespace,
			}

			prevDbRoleClaim := &persistancev1.DbRoleClaim{}
			By("Getting dbroleclaim1")
			Expect(k8sClient.Get(ctx, keyDbRoleClaim1, prevDbRoleClaim)).Should(Succeed())

			By("Deleting dbroleclaim1")
			Eventually(func() error {
				err := k8sClient.Delete(ctx, prevDbRoleClaim)
				if err != nil {
					return err
				} else {
					return fmt.Errorf("error deleting dbroleclaim1")
				}
			}, timeout_e2e, time.Second*5).Should(Succeed())

			By("checking dbroleclaim does not exist")
			Eventually(func() error {
				err := k8sClient.Get(ctx, keyDbRoleClaim1, prevDbRoleClaim)
				if err != nil {
					if errors.IsNotFound(err) {
						return nil
					}
					return err
				} else {
					return fmt.Errorf("dbroleclaim1 still exists")
				}
			}, timeout_e2e, time.Second*5).Should(Succeed())

			// ===================================================== DBRoleClaim2
			keyDbRoleClaim2 := types.NamespacedName{
				Name:      dbroleclaim2,
				Namespace: namespace,
			}
			By("Getting dbroleclaim2")
			Expect(k8sClient.Get(ctx, keyDbRoleClaim2, prevDbRoleClaim)).Should(Succeed())

			By("Deleting dbroleclaim2")
			Eventually(func() error {
				err := k8sClient.Delete(ctx, prevDbRoleClaim)
				if err != nil {
					return err
				} else {
					return fmt.Errorf("error deleting dbroleclaim2")
				}
			}, timeout_e2e, time.Second*5).Should(Succeed())

			By("checking dbroleclaim2 does not exist")
			Eventually(func() error {
				err := k8sClient.Get(ctx, keyDbRoleClaim2, prevDbRoleClaim)
				if err != nil {
					if errors.IsNotFound(err) {
						return nil
					}
					return err
				} else {
					return fmt.Errorf("dbroleclaim2 still exists")
				}
			}, timeout_e2e, time.Second*5).Should(Succeed())

			// ===================================================== DBClaim
			keyDbClaim := types.NamespacedName{
				Name:      db1,
				Namespace: namespace,
			}

			prevDbClaim := &persistancev1.DatabaseClaim{}
			By("Getting dbc")
			Expect(k8sClient.Get(ctx, keyDbClaim, prevDbClaim)).Should(Succeed())
			By("Deleting dbc")
			Expect(k8sClient.Delete(ctx, prevDbClaim)).Should(Succeed())

			By("checking dbclaim does not exist")
			Eventually(func() error {
				err := k8sClient.Get(ctx, keyDbClaim, prevDbClaim)
				if err != nil {
					if errors.IsNotFound(err) {
						return nil
					}
					return err
				} else {
					return fmt.Errorf("dbclaim still exists")
				}
			}, timeout_e2e, time.Second*5).Should(Succeed())

		})
	})

	//creates secret
	//creates db_2 based on db_1
	Context("Use Existing RDS", func() {
		return
		It("should use Existing RDS", func() {
			By("setting up master secret to access existing RDS")
			setupMasterSecretForExistingRDS()
			By("successfully processing an useExisting dbClaim")
			UseExistingPostgresRDSTest()
		})
	})

	//deletes secret
	//updates db_2
	Context("Migrate Use Existing RDS to a local RDS", func() {
		return
		It("should create a new RDS and migrate Existing database", func() {
			By("deleting master secret to access existing RDS")
			//delete master secret if it exists
			k8sClient.Delete(ctx, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      newdbcMasterSecretName,
					Namespace: namespace,
				},
			})
			By("Removing the useExistingFlag in dbclaim and forcing the use of cached secret")
			MigrateUseExistingToNewRDS()
		})
	})

	//updates db_2 to aurora
	Context("Migrate postgres RDS to Aurora RDS", func() {
		return
		It("should create a new RDS and migrate postgres sample_db to new RDS", func() {
			MigratePostgresToAuroraRDS()
		})
	})

	var _ = AfterAll(func() {

		// keyRoleClaim := types.NamespacedName{
		// 	Name:      dbroleclaim1,
		// 	Namespace: namespace,
		// }

		// roleClaim := &persistancev1.DbRoleClaim{}
		// if err := k8sClient.Get(ctx, keyRoleClaim, roleClaim); err == nil {
		// 	By("Deleting dbRoleClaim1")
		// 	Expect(k8sClient.Delete(ctx, roleClaim)).Should(Succeed())
		// }

		// keyRoleClaim.Name = dbroleclaim2
		// roleClaim = &persistancev1.DbRoleClaim{}
		// if err := k8sClient.Get(ctx, keyRoleClaim, roleClaim); err == nil {
		// 	By("Deleting dbRoleClaim2")
		// 	Expect(k8sClient.Delete(ctx, roleClaim)).Should(Succeed())
		// }

		// key := types.NamespacedName{
		// 	Name:      db2,
		// 	Namespace: namespace,
		// }

		// claim := &persistancev1.DatabaseClaim{}
		// if err := k8sClient.Get(ctx, key, claim); err == nil {
		// 	By("Deleting db1")
		// 	Expect(k8sClient.Delete(ctx, claim)).Should(Succeed())
		// }

		// key.Name = db1
		// claim = &persistancev1.DatabaseClaim{}
		// if err := k8sClient.Get(ctx, key, claim); err == nil {
		// 	By("Deleting db2")
		// 	Expect(k8sClient.Delete(ctx, claim)).Should(Succeed())
		// }

		// var dbinst crossplanerds.DBInstance

		// if err := k8sClient.Get(ctx, types.NamespacedName{Name: dbinstance1}, &dbinst); err == nil {
		// 	By(fmt.Sprintf("Deleting crossplane.DBInstance: %s", dbinstance1))
		// 	Expect(k8sClient.Delete(ctx, &dbinst)).Should(Succeed())
		// }

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
	Expect(k8sClient.Get(ctx, key, db2Claim)).Should(Succeed())
	By("Updating type from postgres to aurora-postgresql in the claim")
	db2Claim.Spec.Type = "aurora-postgresql"
	// db2Claim.Spec.DeletionPolicy = "delete"
	Expect(k8sClient.Update(ctx, db2Claim)).Should(Succeed())
	// time.Sleep(time.Minute * 5)
	createdDbClaim := &persistancev1.DatabaseClaim{}
	By("checking dbclaim status is ready")
	By("checking dbclaim status type is aurora-postgresql")
	Eventually(func() (testState, error) {
		err := k8sClient.Get(ctx, key, createdDbClaim)
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
		err := k8sClient.Get(ctx, types.NamespacedName{Name: "sample-secret", Namespace: namespace}, secret)
		if err != nil {
			return "", err
		}
		return string(secret.Data["hostname"]), nil
	}, time.Minute*20, interval_e2e).Should(ContainSubstring("box-3-" + db2 + "-b8487b9c"))
}

func MigrateUseExistingToNewRDS() {
	key := types.NamespacedName{
		Name:      db2,
		Namespace: namespace,
	}
	existingDbClaim := &persistancev1.DatabaseClaim{}
	By("Getting the existing dbclaim")
	Expect(k8sClient.Get(ctx, key, existingDbClaim)).Should(Succeed())
	By("Removing the useExistingFlag in dbclaim")
	existingDbClaim.Spec.UseExistingSource = ptr.To(false)
	existingDbClaim.Spec.SourceDataFrom = nil
	Expect(k8sClient.Update(ctx, existingDbClaim)).Should(Succeed())
	createdDbClaim := &persistancev1.DatabaseClaim{}
	By("checking dbclaim status is ready")
	Eventually(func() (persistancev1.DbState, error) {
		err := k8sClient.Get(ctx, key, createdDbClaim)
		if err != nil {
			logger.Error(err, "error getting dbclaim")
			return "", err
		}
		return createdDbClaim.Status.ActiveDB.DbState, nil
	}, timeout_e2e, interval_e2e).Should(Equal(persistancev1.Ready))
	//check if eventually the secret sample-secret is created
	By("checking if the secret is created and host name contains 1ec9b27c")
	//box-3-end2end-test-2-dbclaim-1ec9b27c
	newSecret := &corev1.Secret{}
	Eventually(func() bool {
		k8sClient.Get(ctx, types.NamespacedName{Name: "sample-secret", Namespace: namespace}, newSecret)
		if string(newSecret.Data["database"]) == "sample_db_new" {
			return true
		} else {
			return false
		}
	}, timeout_e2e, interval_e2e).Should(BeTrue())
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
			Class:                 &class,
			AppID:                 "sample-app",
			DatabaseName:          "sample_db_new",
			SecretName:            "sample-secret",
			Username:              "sample_user",
			Type:                  "postgres",
			DSNName:               "dsn",
			EnableReplicationRole: ptr.To(false),
			UseExistingSource:     ptr.To(true),
			DBVersion:             "15.5",
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
	Expect(k8sClient.Create(ctx, dbClaim)).Should(Succeed())
	// time.Sleep(time.Minute * 5)
	createdDbClaim := &persistancev1.DatabaseClaim{}
	By("checking dbclaim status is use-existing-db")
	Eventually(func() (persistancev1.DbState, error) {
		err := k8sClient.Get(ctx, key, createdDbClaim)
		if err != nil {
			return "", err
		}
		return createdDbClaim.Status.ActiveDB.DbState, nil
	}, time.Minute*10, time.Second*15).Should(Equal(persistancev1.UsingExistingDB))
	//check if eventually the secret sample-secret is created
	By("checking if the secret is created")
	Eventually(func() error {
		return k8sClient.Get(ctx, types.NamespacedName{Name: "sample-secret", Namespace: namespace}, &corev1.Secret{})
	}, time.Minute*2, time.Second*15).Should(BeNil())
}

func setupMasterSecretForExistingRDS() {
	//copy secret from prev dbclaim and use it as master secret for existing rds usecase
	key := types.NamespacedName{
		Name:      newdbcMasterSecretName,
		Namespace: namespace,
	}
	newDBMasterSecret := &corev1.Secret{}
	By("Reading the prev secret")
	Expect(k8sClient.Get(ctx, key, newDBMasterSecret)).Should(Succeed())
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

	Expect(k8sClient.Create(ctx, existingDBMasterSecret)).Should(Succeed())
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
			Class:                 &class,
			AppID:                 "sample-app",
			DatabaseName:          "sample_db",
			SecretName:            "newdb-secret",
			Username:              "sample_user",
			Type:                  "postgres",
			DSNName:               "dsn",
			EnableReplicationRole: ptr.To(false),
			UseExistingSource:     ptr.To(false),
			DBVersion:             "15.5",
			DeletionPolicy:        "delete",
		},
	}
	k8sClient.Create(ctx, dbClaim)
	time.Sleep(time.Minute * 1)
	k8sClient.Delete(ctx, dbClaim)
}
