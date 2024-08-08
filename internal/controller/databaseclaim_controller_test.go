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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
)

var _ = Describe("DatabaseClaim Controller", func() {

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

			By("ensuring the resource does not exist")
			Expect(k8sClient.Get(ctx, typeNamespacedName, claim)).To(HaveOccurred())

			By("creating the custom resource for the Kind DatabaseClaim")
			parsedDSN, err := url.Parse(testDSN)
			Expect(err).NotTo(HaveOccurred())
			password, ok := parsedDSN.User.Password()
			Expect(ok).To(BeTrue())

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
					AppID:                 "sample-app",
					DatabaseName:          "sample_app",
					SecretName:            secretName,
					Username:              parsedDSN.User.Username(),
					EnableSuperUser:       ptr.To(false),
					EnableReplicationRole: ptr.To(false),
					UseExistingSource:     ptr.To(false),
					Type:                  "postgres",
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			secret := &corev1.Secret{}
			err = k8sClient.Get(ctx, typeNamespacedSecretName, secret)
			Expect(err).To(HaveOccurred())
			Expect(client.IgnoreNotFound(err)).To(Succeed())

			secret = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: "default",
				},
				StringData: map[string]string{
					"password": password,
				},
				Type: "Opaque",
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			By("Cleanup the specific resource instance DatabaseClaim")
			resource := &persistancev1.DatabaseClaim{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).ToNot(HaveOccurred())

			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			// Reconcile again to finalize the finalizer
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			By("Ensure the resource is deleted")
			err = k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).To(HaveOccurred())
			Expect(client.IgnoreNotFound(err)).To(Succeed())

			secret := &corev1.Secret{}
			err = k8sClient.Get(ctx, typeNamespacedSecretName, secret)
			Expect(err).NotTo(HaveOccurred())
			By("Cleanup the database secret")
			Expect(k8sClient.Delete(ctx, secret)).To(Succeed())

		})

		It("Should fail to reconcile DB Claim missing dbVersion", func() {
			By("Reconciling the created resource")

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).To(MatchError(".spec.dbVersion is a mandatory field and cannot be empty"))
			Expect(claim.Status.Error).To(Equal(""))
		})

		It("Should succeed with no error status to reconcile CR with DBVersion", func() {
			By("Updating CR with a DB Version")

			resource := &persistancev1.DatabaseClaim{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).NotTo(HaveOccurred())
			resource.Spec.DBVersion = "13.3"
			Expect(k8sClient.Update(ctx, resource)).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).NotTo(HaveOccurred())
			Expect(resource.Spec.DBVersion).To(Equal("13.3"))

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(resource.Status.Error).To(Equal(""))

		})
	})
})
