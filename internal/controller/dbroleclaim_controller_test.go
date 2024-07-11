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

	"github.com/aws/smithy-go/ptr"
	"github.com/go-logr/logr"
	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/viper"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/infobloxopen/db-controller/pkg/roleclaim"
	. "github.com/infobloxopen/db-controller/testutils"
	_ "github.com/lib/pq"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("TestReconcileDbRoleClaim_CopyExistingSecret", Ordered, func() {

	const resourceName = "test-resource"

	ctx := context.Background()

	typeNamespacedName := types.NamespacedName{
		Name:      resourceName,
		Namespace: "default",
	}
	typeNamespacedClaimName := types.NamespacedName{
		Name:      "testdbclaim",
		Namespace: "default",
	}
	typeNamespacedSecretName := types.NamespacedName{
		Name:      "master-secret",
		Namespace: "default",
	}

	dbroleclaim := &persistancev1.DbRoleClaim{}
	viperObj := viper.New()
	viperObj.Set("passwordconfig::passwordRotationPeriod", 60)

	BeforeEach(func() {
		By("creating the custom resource for the Kind DbRoleClaim")
		err := k8sClient.Get(ctx, typeNamespacedName, dbroleclaim)
		if err != nil && errors.IsNotFound(err) {
			resource := &persistancev1.DbRoleClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: persistancev1.DbRoleClaimSpec{
					SourceDatabaseClaim: &persistancev1.SourceDatabaseClaim{
						Namespace: "default",
						Name:      "testdbclaim",
					},
					SecretName: "copy-secret",
					Class:      ptr.String("default"),
				},
				Status: persistancev1.DbRoleClaimStatus{},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		}

		dbclaim := persistancev1.DatabaseClaim{}
		err = k8sClient.Get(ctx, typeNamespacedClaimName, &dbclaim)
		if err != nil && errors.IsNotFound(err) {
			dbClaim := &persistancev1.DatabaseClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "testdbclaim",
					Namespace: "default",
				},
				Spec: persistancev1.DatabaseClaimSpec{
					SecretName: "master-secret",
					Class:      ptr.String("default"),
					Username:   "user1",
				},
				Status: persistancev1.DatabaseClaimStatus{},
			}
			Expect(k8sClient.Create(ctx, dbClaim)).To(Succeed())
		}

		secret := corev1.Secret{}
		err = k8sClient.Get(ctx, typeNamespacedSecretName, &secret)
		if err != nil && errors.IsNotFound(err) {

			sec := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "master-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"password": []byte("masterpassword"),
					"username": []byte("user_a"),
				},
			}
			Expect(k8sClient.Create(ctx, sec)).To(Succeed())
		}
	})

	It("should successfully reconcile the resource", func() {
		By("Reconciling the created resource")

		controllerReconciler := &DbRoleClaimReconciler{
			Client: k8sClient,
			Config: &roleclaim.RoleConfig{
				Viper: viperObj,
			},
		}

		controllerReconciler.Reconciler = &roleclaim.DbRoleClaimReconciler{
			Client: controllerReconciler.Client,
			Config: controllerReconciler.Config,
		}

		_, err := controllerReconciler.Reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())

		var secret = &corev1.Secret{}
		secretName := types.NamespacedName{
			Name:      "copy-secret",
			Namespace: "default",
		}
		err = k8sClient.Get(ctx, secretName, secret)

		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		resource := &persistancev1.DbRoleClaim{}
		err := k8sClient.Get(ctx, typeNamespacedName, resource)
		Expect(err).NotTo(HaveOccurred())

		By("Cleanup the specific resource instance DbRoleClaim")
		Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
	})
})

var _ = Describe("TestSchemaUserClaimReconcile_WithNewUserSchemasRoles_MissingParameter", Ordered, func() {
	RegisterFailHandler(Fail)
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	type reconciler struct {
		Client             client.Client
		Log                logr.Logger
		Scheme             *runtime.Scheme
		Config             *roleclaim.RoleConfig
		DbIdentifierPrefix string
		Context            context.Context
		Request            controllerruntime.Request
	}

	viperObj := viper.New()
	viperObj.Set("passwordconfig::passwordRotationPeriod", 60)

	tests := []struct {
		name    string
		rec     reconciler
		wantErr bool
	}{
		{
			"Get UserSchema claim 1",
			reconciler{
				Client: &MockClient{},
				Config: &roleclaim.RoleConfig{
					Viper: viperObj,
					Class: "default",
				},
				Request: controllerruntime.Request{
					NamespacedName: types.NamespacedName{Namespace: "schema-user-test", Name: "missing_parameter"},
				},
				Log: zap.New(zap.UseDevMode(true)),
			},
			true,
		},
	}

	for _, tt := range tests {
		r := &DbRoleClaimReconciler{
			Client: tt.rec.Client,
			Config: tt.rec.Config,
		}

		r.Reconciler = &roleclaim.DbRoleClaimReconciler{
			Client: r.Client,
			Config: r.Config,
		}

		_, err := r.Reconciler.Reconcile(tt.rec.Context, tt.rec.Request)
		if (err != nil) != tt.wantErr {
			Expect(err).ToNot(BeNil())
			return
		}
	}
})
