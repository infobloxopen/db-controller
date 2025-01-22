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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	crossplaneaws "github.com/crossplane-contrib/provider-aws/apis/rds/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
	v1 "github.com/infobloxopen/db-controller/api/v1"
	"github.com/infobloxopen/db-controller/internal/dockerdb"
	"github.com/infobloxopen/db-controller/pkg/hostparams"
)

var _ = Describe("DatabaseClaim Controller", func() {

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

			By(fmt.Sprintf("Creating dbc: %s", resourceName))
			parsedDSN, err := url.Parse(testDSN)
			Expect(err).NotTo(HaveOccurred())
			password, ok := parsedDSN.User.Password()
			Expect(ok).To(BeTrue())

			secret := &corev1.Secret{}
			err = k8sClient.Get(ctx, typeNamespacedSecretName, secret)
			Expect(err).To(HaveOccurred())
			Expect(client.IgnoreNotFound(err)).To(Succeed())

			By(fmt.Sprintf("creating master credentials: %s", secretName))
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

			By("creating master databaseclaims")
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
					// TODO: remove customization of DSNName
					DSNName:               "fixme.txt",
					Class:                 ptr.To(""),
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

			By("Mocking master credentials")
			hostParams, err := hostparams.New(controllerReconciler.Config.Viper, resource)
			Expect(err).ToNot(HaveOccurred())
			// postgres-db.t4g.medium-15
			credSecretName := fmt.Sprintf("%s-%s-%s", env, resourceName, hostParams.Hash())
			Expect(credSecretName).To(Equal("testenv-test-dbclaim-416e183c"))
			cleanup := dockerdb.MockRDSCredentials(GinkgoT(), ctx, k8sClient, testDSN, credSecretName)
			DeferCleanup(cleanup)

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

		It("Should succeed to reconcile DB Claim missing dbVersion", func() {
			By("Verify that the DB Claim has not active DB yet and spec.DbVersion is empty")
			resource := &v1.DatabaseClaim{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())
			Expect(resource.Status.ActiveDB.DbCreatedAt).To(BeNil())
			Expect(resource.Spec.DBVersion).To(BeEmpty())

			By("Reconciling the created resource")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			By("Verify that the DB Claim has active DB with default Version")
			reconciledClaim := &v1.DatabaseClaim{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, reconciledClaim)).To(Succeed())
			Expect(reconciledClaim.Status.Error).To(Equal(""))
			// default major version (spec version, no active DB)

			activeDb := reconciledClaim.Status.ActiveDB
			Expect(activeDb.DBVersion).To(Equal("15"))
			Expect(activeDb.DbState).To(Equal(v1.Ready))
			Expect(activeDb.SourceDataFrom).To(BeNil())

			By("Validating timestamps in ActiveDB")
			Expect(activeDb.DbCreatedAt).NotTo(BeNil())
			Expect(activeDb.ConnectionInfoUpdatedAt).NotTo(BeNil())
			Expect(activeDb.UserUpdatedAt).NotTo(BeNil())

			By("Validating ConnectionInfo")
			connectionInfo := activeDb.ConnectionInfo
			Expect(connectionInfo).NotTo(BeNil())
		})

		It("Should have DSN and URIDSN keys populated", func() {
			By("Reconciling the created resource")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, typeNamespacedName, claim)
			Expect(err).NotTo(HaveOccurred())
			Expect(claim.Status.Error).To(Equal(""))

			By("Checking the user credentials secret")
			secret := &corev1.Secret{}
			err = k8sClient.Get(ctx, typeNamespacedSecretName, secret)
			Expect(err).NotTo(HaveOccurred())

			for _, key := range []string{v1.DSNKey, v1.DSNURIKey, "fixme.txt", "uri_fixme.txt"} {
				Expect(secret.Data[key]).NotTo(BeNil())
			}
			oldKey := secret.Data[v1.DSNKey]
			Expect(secret.Data[v1.DSNKey]).To(Equal(secret.Data["fixme.txt"]))
			Expect(secret.Data[v1.DSNURIKey]).To(Equal(secret.Data["uri_fixme.txt"]))
			// Slow down the test so creds are rotated, 60ns rotation time
			By("Rotate passwords and verify credentials are updated")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, typeNamespacedSecretName, secret)
			Expect(err).NotTo(HaveOccurred())

			Expect(secret.Data[v1.DSNKey]).NotTo(Equal(oldKey))
			Expect(secret.Data[v1.DSNKey]).To(Equal(secret.Data["fixme.txt"]))
			Expect(secret.Data[v1.DSNURIKey]).To(Equal(secret.Data["uri_fixme.txt"]))
		})

		It("Should have DSN and URIDSN keys populated", func() {
			By("Reconciling the created resource")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			var claim persistancev1.DatabaseClaim
			err = k8sClient.Get(ctx, typeNamespacedName, &claim)
			Expect(err).NotTo(HaveOccurred())
			Expect(claim.Status.Error).To(Equal(""))

			By("Checking the user credentials secret")

			secret := &corev1.Secret{}
			err = k8sClient.Get(ctx, typeNamespacedSecretName, secret)
			Expect(err).NotTo(HaveOccurred())

			for _, key := range []string{v1.DSNKey, v1.DSNURIKey, "fixme.txt", "uri_fixme.txt"} {
				Expect(secret.Data[key]).NotTo(BeNil())
			}
			oldKey := secret.Data[v1.DSNKey]
			Expect(secret.Data[v1.DSNKey]).To(Equal(secret.Data["fixme.txt"]))
			Expect(secret.Data[v1.DSNURIKey]).To(Equal(secret.Data["uri_fixme.txt"]))
			// Slow down the test so creds are rotated, 60ns rotation time
			By("Rotate passwords and verify credentials are updated")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, typeNamespacedSecretName, secret)
			Expect(err).NotTo(HaveOccurred())

			Expect(secret.Data[v1.DSNKey]).NotTo(Equal(oldKey))
			Expect(secret.Data[v1.DSNKey]).To(Equal(secret.Data["fixme.txt"]))
			Expect(secret.Data[v1.DSNURIKey]).To(Equal(secret.Data["uri_fixme.txt"]))

		})

		It("Should succeed with no error status to reconcile CR with DBVersion", func() {
			By("Updating CR with a DB Version")

			resource := &persistancev1.DatabaseClaim{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).NotTo(HaveOccurred())
			resource.Spec.DBVersion = "13.3"
			Expect(k8sClient.Update(ctx, resource)).NotTo(HaveOccurred())

			hostParams, err := hostparams.New(controllerReconciler.Config.Viper, resource)
			Expect(err).ToNot(HaveOccurred())

			credSecretName := fmt.Sprintf("%s-%s-%s", env, resourceName, hostParams.Hash())
			// testenv-test-dbclaim-416e183c
			cleanup := dockerdb.MockRDSCredentials(GinkgoT(), ctx, k8sClient, testDSN, credSecretName)
			DeferCleanup(cleanup)
			Expect(credSecretName).To(Equal("testenv-test-dbclaim-15387708"))

			Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).NotTo(HaveOccurred())
			Expect(resource.Spec.DBVersion).To(Equal("13.3"))

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(resource.Status.Error).To(Equal(""))

			instanceName := fmt.Sprintf("%s-%s-%s", env, resourceName, hostParams.Hash())

			By(fmt.Sprintf("Check dbinstance is created: %s", instanceName))
			var instance crossplaneaws.DBInstance
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: instanceName}, &instance)
			}).Should(Succeed())
		})

		It("Should succeed with no error status to reconcile CR with DBVersion", func() {
			By("Updating CR with a DB Version")

			resource := &persistancev1.DatabaseClaim{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).NotTo(HaveOccurred())
			Expect(k8sClient.Update(ctx, resource)).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).NotTo(HaveOccurred())
			Expect(resource.Spec.DBVersion).To(Equal(""))

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).NotTo(HaveOccurred())
			Expect(resource.Status.Error).To(Equal(""))

		})

		It("Should propagate labels from DatabaseClaim to DBInstance", func() {
			By("Updating CR with labels")

			resource := &v1.DatabaseClaim{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).NotTo(HaveOccurred())

			resource.Labels = map[string]string{
				"app.kubernetes.io/component": resource.Labels["app.kubernetes.io/component"],
				"app.kubernetes.io/instance":  resource.Labels["app.kubernetes.io/instance"],
				"app.kubernetes.io/name":      resource.Labels["app.kubernetes.io/name"],
			}
			Expect(k8sClient.Update(ctx, resource)).To(Succeed())

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			var instance crossplaneaws.DBInstance
			viper := controllerReconciler.Config.Viper
			hostParams, err := hostparams.New(viper, resource)
			Expect(err).ToNot(HaveOccurred())
			instanceName := fmt.Sprintf("%s-%s-%s", env, resourceName, hostParams.Hash())

			By(fmt.Sprintf("Check dbinstance is created with labels: %s", instanceName))
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: instanceName}, &instance)
			}).Should(Succeed())

			Expect(instance.ObjectMeta.Labels).To(Equal(resource.Labels))

		})

		It("Should fail to reconcile a newDB if secret is present", func() {
			By(fmt.Sprintf("creating secret: %s", secretName))
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: "default",
				},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).To(HaveOccurred())
		})

		It("Reconcile rotates the username", func() {
			By("Updating CR with a DB Version")

			resource := &persistancev1.DatabaseClaim{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).NotTo(HaveOccurred())
			Expect(resource.Spec.DBVersion).To(Equal(""))

			By("Rotating to UserSuffixA")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).NotTo(HaveOccurred())
			Expect(resource.Status.Error).To(Equal(""))
			Expect(resource.Status.ActiveDB.ConnectionInfo.Username).To(Equal("postgres_a"))

			By("Rotating to UserSuffixB")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).NotTo(HaveOccurred())
			Expect(resource.Status.Error).To(Equal(""))
			Expect(resource.Status.ActiveDB.ConnectionInfo.Username).To(Equal("postgres_b"))

			By("Rotating to UserSuffixA")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).NotTo(HaveOccurred())
			Expect(resource.Status.Error).To(Equal(""))
			Expect(resource.Status.ActiveDB.ConnectionInfo.Username).To(Equal("postgres_a"))

		})
	})
})
