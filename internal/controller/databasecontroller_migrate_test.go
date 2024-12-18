/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
	v1 "github.com/infobloxopen/db-controller/api/v1"
	"github.com/infobloxopen/db-controller/internal/dockerdb"
	"github.com/infobloxopen/db-controller/pkg/hostparams"
	"github.com/infobloxopen/db-controller/pkg/pgctl"
)

var _ = Describe("claim migrate", func() {

	// Define utility constants for object names and testing timeouts/durations and intervals.

	var (
		ctxLogger context.Context
		cancel    func()
		success   = ctrl.Result{Requeue: false, RequeueAfter: 60}
	)

	BeforeEach(func() {
		ctxLogger, cancel = context.WithCancel(context.Background())
		ctxLogger = log.IntoContext(ctxLogger, NewGinkgoLogger())

		Expect(ctxLogger).ToNot(BeNil())

	})

	AfterEach(func() {
		cancel()
	})

	Context("Setup DB Claim", func() {

		const resourceName = "migrate-dbclaim"
		// secret to store the dsn for claims
		const claimSecretName = "migrate-dbclaim-creds"
		// master creds to source db
		const sourceSecretName = "postgres-source"
		// master creds to target db
		// const targetSecretName = "postgres-target"

		// user for new migrated database
		var migratedowner = "migrate"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}
		typeNamespacedClaimSecretName := types.NamespacedName{
			Name:      claimSecretName,
			Namespace: "default",
		}
		typeNamespacedSourceSecretName := types.NamespacedName{
			Name:      sourceSecretName,
			Namespace: "default",
		}
		// typeNamespacedTargetSecretName := types.NamespacedName{
		// 	Name:      targetSecretName,
		// 	Namespace: "default",
		// }

		claim := &persistancev1.DatabaseClaim{}

		kctx := context.Background()

		BeforeEach(func() {

			By("ensuring the resource does not exist")
			Expect(k8sClient.Get(kctx, typeNamespacedName, claim)).To(HaveOccurred())

			By("creating the custom resource for the Kind DatabaseClaim")
			_, err := url.Parse(testDSN)

			Expect(err).NotTo(HaveOccurred())
			Expect(client.IgnoreNotFound(err)).To(Succeed())

			resource := &persistancev1.DatabaseClaim{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "persistance.atlas.infoblox.com/v1",
					Kind:       "DatabaseClaim",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: persistancev1.DatabaseClaimSpec{
					Class:                 ptr.To(""),
					DatabaseName:          "postgres",
					SecretName:            claimSecretName,
					EnableSuperUser:       ptr.To(false),
					EnableReplicationRole: ptr.To(false),
					UseExistingSource:     ptr.To(true),
					Type:                  "postgres",
					SourceDataFrom: &persistancev1.SourceDataFrom{
						Type: "database",
						Database: &persistancev1.Database{
							SecretRef: &persistancev1.SecretRef{
								Name:      sourceSecretName,
								Namespace: "default",
							},
						},
					},
					//Username: parsedDSN.User.Username(),
					Username: migratedowner,
				},
			}
			Expect(k8sClient.Create(kctx, resource)).To(Succeed())

			By("Creating the source master creds")
			srcSecret := &corev1.Secret{}
			err = k8sClient.Get(ctxLogger, typeNamespacedSourceSecretName, srcSecret)
			Expect(err).To(HaveOccurred())
			Expect(client.IgnoreNotFound(err)).To(Succeed())

			srcSecret = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      sourceSecretName,
					Namespace: "default",
				},
				StringData: map[string]string{
					v1.DSNURIKey: testDSN,
				},
				Type: "Opaque",
			}
			Expect(k8sClient.Create(ctxLogger, srcSecret)).To(Succeed())

		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			By("Cleanup the specific resource instance DatabaseClaim")
			resource := &persistancev1.DatabaseClaim{}
			err := k8sClient.Get(ctxLogger, typeNamespacedName, resource)
			Expect(err).ToNot(HaveOccurred())

			Expect(k8sClient.Delete(ctxLogger, resource)).To(Succeed())
			// Reconcile again to finalize the finalizer
			_, err = controllerReconciler.Reconcile(ctxLogger, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			By("Ensure the resource is deleted")
			err = k8sClient.Get(ctxLogger, typeNamespacedName, resource)
			Expect(err).To(HaveOccurred())
			Expect(client.IgnoreNotFound(err)).To(Succeed())

			By("Cleanup source master creds")
			srcSecret := &corev1.Secret{}
			err = k8sClient.Get(ctxLogger, typeNamespacedSourceSecretName, srcSecret)
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Delete(ctxLogger, srcSecret)).To(Succeed())

		})

		It("Should succeed with UseExistingSource", func() {
			By("Reconciling the created resource")

			_, err := controllerReconciler.Reconcile(ctxLogger, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(claim.Status.Error).To(Equal(""))
			By("Ensuring the active db connection info is set")
			Eventually(func() *persistancev1.DatabaseClaimConnectionInfo {
				Expect(k8sClient.Get(ctxLogger, typeNamespacedName, claim)).NotTo(HaveOccurred())
				return claim.Status.ActiveDB.ConnectionInfo
			}).ShouldNot(BeNil())

			By("Ensuring NewDb is reset to nil")
			Expect(claim.Status.NewDB.ConnectionInfo).To(BeNil())

			By("Ensuring the status activedb connection info is set correctly")
			u, err := url.Parse(testDSN)
			Expect(err).NotTo(HaveOccurred())
			aConn := claim.Status.ActiveDB.ConnectionInfo
			Expect(aConn.Host).To(Equal(u.Hostname()))
			Expect(aConn.Port).To(Equal(u.Port()))
			Expect(aConn.Username).To(Equal(migratedowner + "_a"))
			Expect(aConn.DatabaseName).To(Equal(strings.TrimPrefix(u.Path, "/")))
			Expect(aConn.SSLMode).To(Equal(u.Query().Get("sslmode")))

			activeURI, err := url.Parse(aConn.Uri())
			Expect(err).NotTo(HaveOccurred())

			By("Checking the DSN in the secret")
			redacted := activeURI.Redacted()
			var creds corev1.Secret
			Expect(k8sClient.Get(ctxLogger, typeNamespacedClaimSecretName, &creds)).NotTo(HaveOccurred())
			Expect(creds.Data[persistancev1.DSNURIKey]).ToNot(BeNil())
			dsn, err := url.Parse(string(creds.Data[persistancev1.DSNURIKey]))
			Expect(err).NotTo(HaveOccurred())
			Expect(dsn.Redacted()).To(Equal(redacted))
		})

		It("Migrate", func() {

			Expect(controllerReconciler.Reconcile(ctxLogger, reconcile.Request{NamespacedName: typeNamespacedName})).To(Equal(success))
			var dbc persistancev1.DatabaseClaim
			Expect(k8sClient.Get(ctxLogger, typeNamespacedName, &dbc)).NotTo(HaveOccurred())
			Expect(claim.Status.Error).To(Equal(""))
			By("Ensuring the active db connection info is set")
			Eventually(func() *persistancev1.DatabaseClaimConnectionInfo {
				Expect(k8sClient.Get(ctxLogger, typeNamespacedName, &dbc)).NotTo(HaveOccurred())
				return claim.Status.ActiveDB.ConnectionInfo
			}).ShouldNot(BeNil())

			hostParams, err := hostparams.New(controllerReconciler.Config.Viper, &dbc)
			Expect(err).ToNot(HaveOccurred())

			fakeCPSecretName := fmt.Sprintf("%s-%s-%s", env, resourceName, hostParams.Hash())

			By(fmt.Sprintf("Mocking a RDS pod to look like crossplane set it up: %s", fakeCPSecretName))
			fakeCli, fakeDSN, fakeCancel := dockerdb.MockRDS(GinkgoT(), ctxLogger, k8sClient, fakeCPSecretName, "migrate", dbc.Spec.DatabaseName)
			DeferCleanup(fakeCancel)

			fakeCPSecret := corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fakeCPSecretName,
					Namespace: "default",
				},
			}
			nname := types.NamespacedName{
				Name:      fakeCPSecretName,
				Namespace: "default",
			}
			Eventually(k8sClient.Get(ctxLogger, nname, &fakeCPSecret)).Should(Succeed())
			logger.Info("debugsecret", "rdssecret", fakeCPSecret)

			By("Disabling UseExistingSource")
			Expect(k8sClient.Get(ctxLogger, typeNamespacedName, &dbc)).NotTo(HaveOccurred())
			dbc.Spec.UseExistingSource = ptr.To(false)
			Expect(k8sClient.Update(ctxLogger, &dbc)).NotTo(HaveOccurred())
			res, err := controllerReconciler.Reconcile(ctxLogger, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).To(BeNil())
			Expect(dbc.Status.Error).To(Equal(""))
			Expect(res.Requeue).To(BeTrue())
			Expect(k8sClient.Get(ctxLogger, typeNamespacedName, &dbc)).NotTo(HaveOccurred())
			By("Check source DSN looks roughly correct")
			activeDB := dbc.Status.ActiveDB
			Expect(activeDB.ConnectionInfo).NotTo(BeNil())
			compareDSN := strings.Replace(testDSN, "//postgres:postgres", fmt.Sprintf("//%s_b:", migratedowner), 1)
			Expect(activeDB.ConnectionInfo.Uri()).To(Equal(compareDSN))

			By("Check target DSN looks roughly correct")
			newDB := dbc.Status.NewDB
			compareDSN = strings.Replace(fakeDSN, "//migrate:postgres", fmt.Sprintf("//%s_a:", migratedowner), 1)
			Expect(newDB.ConnectionInfo).NotTo(BeNil())
			Expect(newDB.ConnectionInfo.Uri()).To(Equal(compareDSN))
			Expect(dbc.Status.MigrationState).To(Equal(pgctl.S_MigrationInProgress.String()))

			var tempCreds corev1.Secret
			// temp-migrate-dbclaim-creds
			Expect(k8sClient.Get(ctxLogger, types.NamespacedName{Name: "temp-" + claimSecretName, Namespace: "default"}, &tempCreds)).NotTo(HaveOccurred())
			for k, v := range tempCreds.Data {
				logger.Info("tempcreds", k, string(v))
			}

			By("CR reconciles but must be requeued to perform migration, reconcile manually for test")
			res, err = controllerReconciler.Reconcile(ctxLogger, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).To(BeNil())
			Expect(res.Requeue).To(BeFalse())
			Expect(k8sClient.Get(ctxLogger, typeNamespacedName, &dbc)).NotTo(HaveOccurred())
			Expect(dbc.Status.Error).To(Equal(""))

			By("Waiting to disable source, reconcile manually again")
			res, err = controllerReconciler.Reconcile(ctxLogger, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).To(BeNil())
			Expect(res.RequeueAfter).To(Equal(time.Duration(60 * time.Second)))
			By("Verify migration is complete on this reconcile")
			res, err = controllerReconciler.Reconcile(ctxLogger, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).To(BeNil())
			Expect(res.Requeue).To(BeFalse())
			Expect(k8sClient.Get(ctxLogger, typeNamespacedName, &dbc)).NotTo(HaveOccurred())
			Expect(dbc.Status.Error).To(Equal(""))
			Expect(dbc.Status.MigrationState).To(Equal(pgctl.S_Completed.String()))
			activeDB = dbc.Status.ActiveDB
			Expect(activeDB.ConnectionInfo).NotTo(BeNil())
			Expect(activeDB.ConnectionInfo.Uri()).To(Equal(compareDSN))

			By(fmt.Sprintf("Verify owner of migrated DB is %s", migratedowner))
			row := fakeCli.QueryRowContext(ctxLogger, "select tableowner from pg_tables where tablename = 'users' AND schemaname = 'public';")
			var owner string
			Expect(row.Scan(&owner)).NotTo(HaveOccurred())
			Expect(owner).To(Equal(migratedowner))

		})

	})
})
