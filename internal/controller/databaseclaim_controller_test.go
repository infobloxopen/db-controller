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

	"github.com/infobloxopen/db-controller/internal/dockerdb"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	crossplaneaws "github.com/crossplane-contrib/provider-aws/apis/rds/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1 "github.com/infobloxopen/db-controller/api/v1"
	"github.com/infobloxopen/db-controller/pkg/hostparams"
)

var _ = Describe("DatabaseClaim Controller", func() {

	Context("When updating DB Claim Status", func() {
		const (
			resourceName = "test-dbclaim"
			secretName   = "postgres-postgresql"
			namespace    = "default"
			databaseName = "test-db"
			databaseType = "postgres"
		)
		var (
			ctx                      = context.Background()
			typeNamespacedName       = types.NamespacedName{Name: resourceName, Namespace: namespace}
			typeNamespacedSecretName = types.NamespacedName{Name: secretName, Namespace: namespace}
			claim                    = &v1.DatabaseClaim{}
		)

		createDatabaseClaim := func() {
			parsedDSN, err := url.Parse(testDSN)
			Expect(err).NotTo(HaveOccurred())
			resource := &v1.DatabaseClaim{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "persistance.atlas.infoblox.com/v1",
					Kind:       "DatabaseClaim",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: namespace,
				},
				Spec: v1.DatabaseClaimSpec{
					DSNName:               "fixme.txt",
					Class:                 ptr.To(""),
					DatabaseName:          databaseName,
					SecretName:            secretName,
					Username:              parsedDSN.User.Username(),
					EnableSuperUser:       ptr.To(false),
					EnableReplicationRole: ptr.To(false),
					UseExistingSource:     ptr.To(false),
					Type:                  databaseType,
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		}

		BeforeEach(func() {
			By("Verify environment")
			viper := controllerReconciler.Config.Viper
			Expect(viper.Get("env")).To(Equal(env))

			By("Ensuring the resource does not exist")
			Expect(k8sClient.Get(ctx, typeNamespacedName, claim)).To(HaveOccurred())

			By(fmt.Sprintf("Creating DatabaseClaim: %s", resourceName))
			createDatabaseClaim()

			By("Mocking master credentials")
			Expect(k8sClient.Get(ctx, typeNamespacedName, claim)).To(Succeed())
			hostParams, err := hostparams.New(controllerReconciler.Config.Viper, claim)
			Expect(err).NotTo(HaveOccurred())
			credSecretName := fmt.Sprintf("%s-%s-%s", env, resourceName, hostParams.Hash())
			cleanup := dockerdb.MockRDSCredentials(GinkgoT(), ctx, k8sClient, testDSN, credSecretName)
			DeferCleanup(cleanup)
		})

		AfterEach(func() {
			By("Cleanup the specific resource instance DatabaseClaim")
			resource := &v1.DatabaseClaim{}
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

			By("Cleanup DbClaim associated secret")
			secret := &corev1.Secret{}
			err = k8sClient.Get(ctx, typeNamespacedSecretName, secret)
			// this secret is created without resource owner, so it does not get deleted after dbclain is deleted
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

		It("Should succeed with no error status to reconcile CR with DBVersion", func() {
			By("Updating the DatabaseClaim resource with a DB Version 13.3")
			resource := &v1.DatabaseClaim{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())
			Expect(resource.Status.ActiveDB.DbCreatedAt).To(BeNil())
			resource.Spec.DBVersion = "13.3"
			Expect(k8sClient.Update(ctx, resource)).To(Succeed())

			By("Mocking RDS master credentials")
			hostParams, err := hostparams.New(controllerReconciler.Config.Viper, resource)
			Expect(err).ToNot(HaveOccurred())

			// given db version changed, we need to mock master creds again as the hash changed
			credSecretName := fmt.Sprintf("%s-%s-%s", env, resourceName, hostParams.Hash())
			cleanup := dockerdb.MockRDSCredentials(GinkgoT(), ctx, k8sClient, testDSN, credSecretName)
			DeferCleanup(cleanup)

			By("Reconciling the updated resource")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).ToNot(HaveOccurred())
			Expect(resource.Status.Error).To(BeEmpty())

			By(fmt.Sprintf("Verifying that the DBInstance is created: %s", credSecretName))
			var instance crossplaneaws.DBInstance
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: credSecretName}, &instance)
			}).Should(Succeed())

			By("Verifying that the DBInstance is updated")
			var reconciledClaim v1.DatabaseClaim
			Expect(k8sClient.Get(ctx, typeNamespacedName, &reconciledClaim)).NotTo(HaveOccurred())
			Expect(reconciledClaim.Status.Error).To(Equal(""))

			By("Validating the fields in ActiveDB")
			activeDB := reconciledClaim.Status.ActiveDB
			Expect(activeDB.Shape).To(Equal(hostParams.Shape))
			Expect(string(activeDB.Type)).To(Equal(hostParams.Type))
			Expect(activeDB.MinStorageGB).To(Equal(hostParams.MinStorageGB))
			Expect(activeDB.MaxStorageGB).To(Equal(hostParams.MaxStorageGB))

			Expect(activeDB.DBVersion).To(Equal(resource.Spec.DBVersion))
			Expect(activeDB.Shape).To(Equal(*instance.Spec.ForProvider.DBInstanceClass))
			Expect(string(activeDB.Type)).To(Equal(*instance.Spec.ForProvider.Engine))
			Expect(int64(activeDB.MinStorageGB)).To(Equal(*instance.Spec.ForProvider.AllocatedStorage))

			Expect(activeDB.DbState).To(Equal(v1.Ready))
			Expect(activeDB.SourceDataFrom).To(BeNil())

			By("Validating timestamps in ActiveDB")
			Expect(activeDB.DbCreatedAt).NotTo(BeNil())
			Expect(activeDB.ConnectionInfoUpdatedAt).NotTo(BeNil())
			Expect(activeDB.UserUpdatedAt).NotTo(BeNil())

			By("Validating ConnectionInfo")
			connectionInfo := activeDB.ConnectionInfo
			Expect(connectionInfo).NotTo(BeNil())
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
	})
})
