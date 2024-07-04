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
	"strconv"
	"testing"

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

	basefun "github.com/infobloxopen/db-controller/pkg/basefunctions"
	. "github.com/infobloxopen/db-controller/pkg/dbclient"
	"github.com/infobloxopen/db-controller/pkg/roleclaim"
	. "github.com/infobloxopen/db-controller/testutils"
	_ "github.com/lib/pq"
)

func TestReconcileDbRoleClaim_Simple(t *testing.T) {
	// FIXME: make this test do things

	const resourceName = "test-resource"

	ctx := context.Background()

	typeNamespacedName := types.NamespacedName{
		Name:      resourceName,
		Namespace: "default",
	}
	dbroleclaim := &persistancev1.DbRoleClaim{}

	BeforeEach(func() {
		By("creating the custom resource for the Kind DbRoleClaim")
		err := k8sClient.Get(ctx, typeNamespacedName, dbroleclaim)
		if err != nil && errors.IsNotFound(err) {
			resource := &persistancev1.DbRoleClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		}
	})

	AfterEach(func() {
		resource := &persistancev1.DbRoleClaim{}
		err := k8sClient.Get(ctx, typeNamespacedName, resource)
		Expect(err).NotTo(HaveOccurred())

		By("Cleanup the specific resource instance DbRoleClaim")
		Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
	})
	It("should successfully reconcile the resource", func() {
		By("Reconciling the created resource")
		controllerReconciler := &DbRoleClaimReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}

		_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: typeNamespacedName,
		})
		Expect(err).NotTo(HaveOccurred())
		// TODO(user): Add more specific assertions depending on your controller's reconciliation logic.
		// Example: If you expect a certain status condition after reconciliation, verify it here.
	})
}

func TestSchemaUserClaimReconcile_WithNewUserSchemasRoles_MissingParameter(t *testing.T) {
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
				Client: &MockClient{Port: "123"},
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
		t.Run(tt.name, func(t *testing.T) {
			r := &DbRoleClaimReconciler{
				Client: tt.rec.Client,
				Scheme: tt.rec.Scheme,
				Config: tt.rec.Config,
			}
			r.SetupWithManager(nil)

			_, err := r.reconciler.Reconcile(tt.rec.Context, tt.rec.Request)
			if (err != nil) != tt.wantErr {
				t.Errorf("error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}

func TestSchemaUserClaimReconcile_WithNewUserSchemasRoles(t *testing.T) {
	RegisterFailHandler(Fail)
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	testDB := SetupSqlDB(t, "user_a", "masterpassword")
	defer testDB.Close()

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
				Client: &MockClient{Port: strconv.Itoa(testDB.Port)},
				Config: &roleclaim.RoleConfig{
					Viper: viperObj,
					Class: "default",
				},
				Request: controllerruntime.Request{
					NamespacedName: types.NamespacedName{Namespace: "schema-user-test", Name: "schema-user-claim-1"},
				},
				Log: zap.New(zap.UseDevMode(true)),
			},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &DbRoleClaimReconciler{
				Client: tt.rec.Client,
				Scheme: tt.rec.Scheme,
				Config: tt.rec.Config,
			}
			r.SetupWithManager(nil)

			result, err := r.reconciler.Reconcile(tt.rec.Context, tt.rec.Request)
			if (err != nil) != tt.wantErr {
				t.Errorf("error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			Expect(result.Requeue).Should(BeFalse())

			existingDBConnInfo, err := persistancev1.ParseUri(testDB.URL())
			if err != nil {
				t.Errorf("error = %v", err)
				return
			}

			dbClient, err := basefun.GetClientForExistingDB(existingDBConnInfo, &controllerruntime.Log)
			if err != nil {
				t.Errorf("error = %v", err)
				return
			}

			var responseUpdate = r.Client.(*MockClient).GetResponseUpdate()
			Expect(responseUpdate).Should(Not(BeNil()))
			var schemaUserClaimStatus = responseUpdate.(*persistancev1.DbRoleClaim).Status
			Expect(schemaUserClaimStatus).Should(Not(BeNil()))
			Expect(schemaUserClaimStatus.Error).Should(BeEmpty())
			Expect(schemaUserClaimStatus.SchemaRoleStatus.SchemaStatus).Should(HaveLen(5))

			Expect(schemaUserClaimStatus.SchemaRoleStatus.SchemaStatus["schema1"]).Should(Equal("valid"))
			Expect(schemaUserClaimStatus.SchemaRoleStatus.SchemaStatus["schema2"]).Should(Equal("valid"))
			Expect(schemaUserClaimStatus.SchemaRoleStatus.SchemaStatus["schema3"]).Should(Equal("valid"))
			Expect(schemaUserClaimStatus.SchemaRoleStatus.SchemaStatus["schema4"]).Should(Equal("valid"))
			Expect(schemaUserClaimStatus.SchemaRoleStatus.SchemaStatus["public"]).Should(Equal("valid"))

			Expect(schemaUserClaimStatus.SchemaRoleStatus.RoleStatus["schema1_regular"]).Should(Equal("valid"))
			Expect(schemaUserClaimStatus.SchemaRoleStatus.RoleStatus["schema2_admin"]).Should(Equal("valid"))
			Expect(schemaUserClaimStatus.SchemaRoleStatus.RoleStatus["schema3_readonly"]).Should(Equal("valid"))
			Expect(schemaUserClaimStatus.SchemaRoleStatus.RoleStatus["schema4_admin"]).Should(Equal("valid"))
			Expect(schemaUserClaimStatus.SchemaRoleStatus.RoleStatus["public_admin"]).Should(Equal("valid"))

			//-----------------
			exists, err := dbClient.SchemaExists("schema1")
			Expect(exists).Should(BeTrue())
			Expect(err).Should(BeNil())

			exists, err = dbClient.RoleExists("schema1_regular")
			Expect(exists).Should(BeTrue())
			Expect(err).Should(BeNil())
			exists, err = dbClient.UserExists("user1_a")
			Expect(exists).Should(BeTrue())
			Expect(err).Should(BeNil())
			//-----------------
			exists, err = dbClient.SchemaExists("schema2")
			Expect(exists).Should(BeTrue())
			Expect(err).Should(BeNil())

			exists, err = dbClient.RoleExists("schema2_admin")
			Expect(exists).Should(BeTrue())
			Expect(err).Should(BeNil())
			//-----------------
			exists, err = dbClient.SchemaExists("schema3")
			Expect(exists).Should(BeTrue())
			Expect(err).Should(BeNil())

			exists, err = dbClient.RoleExists("schema3_readonly")
			Expect(exists).Should(BeTrue())
			Expect(err).Should(BeNil())
			//----------PUBLIC-------
			exists, err = dbClient.RoleExists("public_admin")
			Expect(exists).Should(BeTrue())
			Expect(err).Should(BeNil())

		})
	}
}

func TestSchemaUserClaimReconcile_WithNewUserSchemasRoles_UpdatePassword(t *testing.T) {
	RegisterFailHandler(Fail)
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	testDB := SetupSqlDB(t, "user_a", "masterpassword")
	defer testDB.Close()

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
			"Get UserSchema claim 2",
			reconciler{
				Client: &MockClient{Port: strconv.Itoa(testDB.Port)},
				Config: &roleclaim.RoleConfig{
					Viper: viperObj,
					Class: "default",
				},
				Request: controllerruntime.Request{
					NamespacedName: types.NamespacedName{Namespace: "schema-user-test", Name: "schema-user-claim-2"},
				},
				Log: zap.New(zap.UseDevMode(true)),
			},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &DbRoleClaimReconciler{
				Client: tt.rec.Client,
				Scheme: tt.rec.Scheme,
				Config: tt.rec.Config,
			}
			r.SetupWithManager(nil)
			existingDBConnInfo, err := persistancev1.ParseUri(testDB.URL())
			if err != nil {
				t.Errorf("error = %v", err)
				return
			}

			dbClient, err := basefun.GetClientForExistingDB(existingDBConnInfo, &controllerruntime.Log)
			if err != nil {
				t.Errorf("error = %v", err)
				return
			}

			//seed database to simulate existing user
			dbClient.CreateSchema("schema1")
			dbClient.CreateRegularRole(existingDBConnInfo.DatabaseName, "schema2_admin", "schema1")
			dbClient.CreateUser("user2_b", "schema1_regular", "123")

			result, err := r.reconciler.Reconcile(tt.rec.Context, tt.rec.Request)
			if (err != nil) != tt.wantErr {
				t.Errorf("error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			Expect(result.Requeue).Should(BeFalse())

			var responseUpdate = r.Client.(*MockClient).GetResponseUpdate()
			Expect(responseUpdate).Should(Not(BeNil()))
			var schemaUserClaimStatus = responseUpdate.(*persistancev1.DbRoleClaim).Status
			Expect(schemaUserClaimStatus).Should(Not(BeNil()))
			Expect(schemaUserClaimStatus.Error).Should(BeEmpty())
			Expect(schemaUserClaimStatus.Username).Should(Equal("user2_b"))
			Expect(schemaUserClaimStatus.SchemaRoleStatus.SchemaStatus).Should(HaveLen(3))

			Expect(schemaUserClaimStatus.SchemaRoleStatus.SchemaStatus["schema1"]).Should(Equal("valid"))
			Expect(schemaUserClaimStatus.SchemaRoleStatus.SchemaStatus["schema2"]).Should(Equal("valid"))
			Expect(schemaUserClaimStatus.SchemaRoleStatus.SchemaStatus["schema3"]).Should(Equal("valid"))

			Expect(schemaUserClaimStatus.SchemaRoleStatus.RoleStatus["schema1_regular"]).Should(Equal("valid"))
			Expect(schemaUserClaimStatus.SchemaRoleStatus.RoleStatus["schema2_admin"]).Should(Equal("valid"))
			Expect(schemaUserClaimStatus.SchemaRoleStatus.RoleStatus["schema3_readonly"]).Should(Equal("valid"))

			//-----------------
			exists, err := dbClient.SchemaExists("schema1")
			Expect(exists).Should(BeTrue())
			Expect(err).Should(BeNil())

			exists, err = dbClient.RoleExists("schema1_regular")
			Expect(exists).Should(BeTrue())
			Expect(err).Should(BeNil())
			exists, err = dbClient.UserExists("user2_b")
			Expect(exists).Should(BeTrue())
			Expect(err).Should(BeNil())
			//-----------------
			exists, err = dbClient.SchemaExists("schema2")
			Expect(exists).Should(BeTrue())
			Expect(err).Should(BeNil())

			exists, err = dbClient.RoleExists("schema2_admin")
			Expect(exists).Should(BeTrue())
			Expect(err).Should(BeNil())
			//-----------------
			exists, err = dbClient.SchemaExists("schema3")
			Expect(exists).Should(BeTrue())
			Expect(err).Should(BeNil())

			exists, err = dbClient.RoleExists("schema3_readonly")
			Expect(exists).Should(BeTrue())
			Expect(err).Should(BeNil())

		})
	}
}
