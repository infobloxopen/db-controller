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
	"net/url"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
	"github.com/infobloxopen/db-controller/pkg/config"
	"github.com/infobloxopen/db-controller/pkg/databaseclaim"
)

var _ = Describe("DatabaseClaim Controller", func() {
	Context("When reconciling a resource", func() {
		/*
			const resourceName = "test-resource"

			ctx := context.Background()

			typeNamespacedName := types.NamespacedName{
				Name:      resourceName,
				Namespace: "default", // TODO(user):Modify as needed
			}
			databaseclaim := &persistancev1.DatabaseClaim{}

			BeforeEach(func() {
				By("creating the custom resource for the Kind DatabaseClaim")
				err := k8sClient.Get(ctx, typeNamespacedName, databaseclaim)
				if err != nil && errors.IsNotFound(err) {
					resource := &persistancev1.DatabaseClaim{
						ObjectMeta: metav1.ObjectMeta{
							Name:      resourceName,
							Namespace: "default",
						},
						// TODO(user): Specify other spec details if needed.
					}
					Expect(k8sClient.Create(ctx, resource)).To(Succeed())
				}
			})

			AfterEach(func() {
				// TODO(user): Cleanup logic after each test, like removing the resource instance.
				resource := &persistancev1.DatabaseClaim{}
				err := k8sClient.Get(ctx, typeNamespacedName, resource)
				Expect(err).NotTo(HaveOccurred())

				By("Cleanup the specific resource instance DatabaseClaim")
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			})
			It("should successfully reconcile the resource", func() {
				By("Reconciling the created resource")
				controllerReconciler := &DatabaseClaimReconciler{
					Client: k8sClient,
					Scheme: k8sClient.Scheme(),
				}

				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: typeNamespacedName,
				})
				_ = err
				// FIXME: make this a valid test
				// Expect(err).NotTo(HaveOccurred())
				// TODO(user): Add more specific assertions depending on your controller's reconciliation logic.
				// Example: If you expect a certain status condition after reconciliation, verify it here.
			})
		*/
	})

})

var _ = Describe("db-controller", func() {

	// Define utility constants for object names and testing timeouts/durations and intervals.

	Context("When updating DB Claim Status", func() {

		const resourceName = "test-dbclaim"
		const secretName = "postgres-postgresql"

		ctx := context.Background()
		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default", // TODO(user):Modify as needed
		}
		typeNamespacedSecretName := types.NamespacedName{
			Name:      secretName,
			Namespace: "default", // TODO(user):Modify as needed
		}
		claim := &persistancev1.DatabaseClaim{}

		BeforeEach(func() {
			parsedDSN, err := url.Parse(testDSN)
			Expect(err).NotTo(HaveOccurred())
			password, ok := parsedDSN.User.Password()
			Expect(ok).To(BeTrue())

			By("creating the custom resource for the Kind DatabaseClaim")
			err = k8sClient.Get(ctx, typeNamespacedName, claim)
			if err != nil && errors.IsNotFound(err) {
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
						AppID:                 "sample-app",
						DatabaseName:          "sample_app",
						InstanceLabel:         "sample-connection",
						SecretName:            secretName,
						Username:              parsedDSN.User.Username(),
						EnableSuperUser:       ptr.To(false),
						EnableReplicationRole: ptr.To(false),
						UseExistingSource:     ptr.To(false),
						Type:                  "postgres",

						Port: parsedDSN.Port(),
						Host: parsedDSN.Hostname(),
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}

			secret := &corev1.Secret{}
			err = k8sClient.Get(ctx, typeNamespacedSecretName, secret)
			if err != nil && errors.IsNotFound(err) {
				resource := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      secretName,
						Namespace: "default",
					},
					StringData: map[string]string{
						"password": password,
					},
					Type: "Opaque",
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			}

		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &persistancev1.DatabaseClaim{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance DatabaseClaim")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

			secret := &corev1.Secret{}
			err = k8sClient.Get(ctx, typeNamespacedSecretName, secret)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the database secret")

			Expect(k8sClient.Delete(ctx, secret)).To(Succeed())

		})

		It("Should update DB Claim status", func() {

			By("Reconciling the created resource")

			configPath, err := filepath.Abs(filepath.Join("..", "..", "cmd", "config", "config.yaml"))
			Expect(err).NotTo(HaveOccurred())

			controllerReconciler := &DatabaseClaimReconciler{
				Config: &databaseclaim.DatabaseClaimConfig{
					Viper:     config.NewConfig(logger, configPath),
					Namespace: "default",
				},
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			controllerReconciler.Setup()

			// FIXME: make these actual properties on the reconciler struct
			controllerReconciler.Config.Viper.Set("defaultMasterusername", "postgres")
			controllerReconciler.Config.Viper.Set("defaultSslMode", "require")

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() bool {
				resource := &persistancev1.DatabaseClaim{}
				err := k8sClient.Get(ctx, typeNamespacedName, resource)
				if err != nil {
					return false
				}
				return resource.Status.Error == ""

			}, 10*time.Second, 100*time.Millisecond).Should(BeTrue())

		})
	})
})

func TestDB(t *testing.T) {
	db, _, cleanup := RunDB()
	defer cleanup()
	defer db.Close()
	_, err := db.Exec("CREATE TABLE test (id SERIAL PRIMARY KEY, name TEXT)")
	if err != nil {
		panic(err)
	}
}
