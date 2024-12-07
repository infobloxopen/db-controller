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
	"database/sql"
	"fmt"
	"net/url"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/go-logr/logr"
	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
	_ "github.com/lib/pq"
	"github.com/spf13/viper"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/infobloxopen/db-controller/pkg/roleclaim"
)

var _ = Describe("RoleClaim Controller", Ordered, func() {

	const resourceName = "test-resource"
	const newNamespace = "test-namespace-2"
	const claimSecretName = "migrate-dbclaim-creds"

	var (
		ctxLogger context.Context
		cancel    func()
	)

	const sourceSecretName = "postgres-source"
	var migratedowner = "migrate"

	ctx := context.Background()
	claim := &persistancev1.DatabaseClaim{}

	typeNamespacedName := types.NamespacedName{
		Name:      resourceName,
		Namespace: "default",
	}

	typeNamespacedDBResourceName := types.NamespacedName{
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
	typeNamespacedSourceSecretName := types.NamespacedName{
		Name:      sourceSecretName,
		Namespace: "default",
	}

	typeNamespacedClaimSecretName := types.NamespacedName{
		Name:      claimSecretName,
		Namespace: "default",
	}

	typeNamespacedNameInvalidParam := types.NamespacedName{Namespace: "default", Name: "missing-parameter"}

	viperObj := viper.New()
	viperObj.Set("passwordconfig::passwordRotationPeriod", 60)
	viperObj.Set("defaultMasterUsername", "root")
	viperObj.Set("defaultMasterPort", "5432")
	viperObj.Set("defaultSslMode", "require")
	viperObj.Set("defaultMinStorageGB", "10")
	viperObj.Set("defaultSslMode", "disable")

	BeforeEach(func() {
		ctxLogger, cancel = context.WithCancel(context.Background())
		ctxLogger = log.IntoContext(ctxLogger, NewGinkgoLogger())

		By("Creating the custom resource for the Kind DatabaseClaim if not present")
		err := k8sClient.Get(ctx, typeNamespacedDBResourceName, claim)
		if err != nil && errors.IsNotFound(err) {
			claim = &persistancev1.DatabaseClaim{
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
								Name: sourceSecretName,

								Namespace: "default",
							},
						},
					},
					Username: migratedowner,
				},
			}
			Expect(k8sClient.Create(ctx, claim)).To(Succeed())
		}

		By("creating the custom resource for the Kind DatabaseClaim")
		_, err = url.Parse(testDSN)
		Expect(err).NotTo(HaveOccurred())
		Expect(client.IgnoreNotFound(err)).To(Succeed())

		By("Creating the source master creds")
		srcSecret := &corev1.Secret{}
		err = k8sClient.Get(ctxLogger, typeNamespacedSourceSecretName, srcSecret)
		if err != nil && errors.IsNotFound(err) {
			srcSecret = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      sourceSecretName,
					Namespace: "default",
				},
				StringData: map[string]string{
					"uri_dsn.txt": testDSN,
				},
				Type: "Opaque",
			}
			Expect(k8sClient.Create(ctxLogger, srcSecret)).To(Succeed())
		}

		By("Create a new Namespace for creation new Role Claim if not present")
		namespace := &corev1.Namespace{}
		err = k8sClient.Get(ctx, types.NamespacedName{Name: newNamespace}, namespace)
		if err != nil && errors.IsNotFound(err) {
			namespace = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: newNamespace,
				},
			}
			Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
		} else {
			Expect(err).NotTo(HaveOccurred())
		}

		dbroleclaim := &persistancev1.DbRoleClaim{}
		By("creating the custom resource for the Kind DbRoleClaim")
		err = k8sClient.Get(ctx, typeNamespacedName, dbroleclaim)
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
					Class:      ptr.To("default"),
				},
				Status: persistancev1.DbRoleClaimStatus{},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		}

		By("creating an invalid DBRoleClaim")
		err = k8sClient.Get(ctx, typeNamespacedNameInvalidParam, dbroleclaim)
		if err != nil && errors.IsNotFound(err) {
			resource := &persistancev1.DbRoleClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      typeNamespacedNameInvalidParam.Name,
					Namespace: typeNamespacedNameInvalidParam.Namespace,
				},
				Spec: persistancev1.DbRoleClaimSpec{
					SourceDatabaseClaim: &persistancev1.SourceDatabaseClaim{
						Namespace: "schema-user-test",
						Name:      "testclaim",
					},
					SecretName: "copy-secret",
					Class:      ptr.To("default"),
					SchemaRoleMap: map[string]persistancev1.RoleType{
						"schema0": "",
					},
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
					Class:      ptr.To("default"),
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
					"port":     []byte("5432"),
					"database": []byte("postgres"),
					"hostname": []byte("localhost"),
					"sslmode":  []byte("disable"),
				},
			}
			Expect(k8sClient.Create(ctx, sec)).To(Succeed())
		}

		By("Getting the DatabaseClaim resource created in the default namespace")
		err = k8sClient.Get(ctx, typeNamespacedDBResourceName, claim)
		Expect(err).NotTo(HaveOccurred())

		By("Creating the custom resource for the Kind DbRoleClaim in the new namespace")
		dbroleclaimdiffnamespace := &persistancev1.DbRoleClaim{}
		err = k8sClient.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: newNamespace}, dbroleclaimdiffnamespace)
		if err != nil && errors.IsNotFound(err) {
			dbroleclaimdiffnamespace = &persistancev1.DbRoleClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: newNamespace,
				},
				Spec: persistancev1.DbRoleClaimSpec{
					SourceDatabaseClaim: &persistancev1.SourceDatabaseClaim{
						Name:      claim.Name,      // Claim is created in the default namespace and used as source
						Namespace: claim.Namespace, // Claim is created in the default namespace and used as source
					},
					SecretName: "copy-secret",
				},
			}
			Expect(k8sClient.Create(ctx, dbroleclaimdiffnamespace)).To(Succeed())
		} else {
			Expect(err).NotTo(HaveOccurred())
		}

	})

	AfterEach(func() {
		resource := &persistancev1.DbRoleClaim{}
		err := k8sClient.Get(ctx, typeNamespacedName, resource)
		Expect(err).NotTo(HaveOccurred())

		By("Cleanup the specific resource instance DbRoleClaim")
		Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

		By("Cleanup the specific resource instance DatabaseClaim")
		dbroleclaim := &persistancev1.DatabaseClaim{}
		err = k8sClient.Get(ctx, typeNamespacedDBResourceName, dbroleclaim)
		fmt.Println("Error: ", err)
		Expect(k8sClient.Delete(ctx, dbroleclaim)).To(Succeed())
		cancel()

	})

	It("should successfully reconcile the resource", func() {
		By("Reconciling the created claim in default namespace")
		_, err := controllerReconciler.Reconcile(ctxLogger, reconcile.Request{NamespacedName: typeNamespacedDBResourceName})
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

		By("Ensuring the active db connection info is set")
		Eventually(func() *persistancev1.DatabaseClaimConnectionInfo {
			Expect(k8sClient.Get(ctxLogger, typeNamespacedName, claim)).NotTo(HaveOccurred())
			return claim.Status.ActiveDB.ConnectionInfo
		}).ShouldNot(BeNil())

		By("Getting the newly created Namespace resource")
		namespace := &corev1.Namespace{}
		err = k8sClient.Get(ctx, types.NamespacedName{Name: newNamespace}, namespace)
		fmt.Println("Namespace Name:", namespace.Name)
		Expect(err).NotTo(HaveOccurred())

		By("Creating a DBRoleclaim reconciler")
		dbroleclaimreconciler := &DbRoleClaimReconciler{
			Client: k8sClient,
			Config: &roleclaim.RoleConfig{
				Viper:     viperObj,
				Namespace: "default",
			},
		}
		dbroleclaimreconciler.Reconciler = &roleclaim.DbRoleClaimReconciler{
			Client: dbroleclaimreconciler.Client,
			Config: dbroleclaimreconciler.Config,
		}

		By("Reconciling the created resource")
		_, err = dbroleclaimreconciler.Reconciler.Reconcile(ctx, reconcile.Request{
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

		By("Reconciling the created DbRoleClaim resource in new namespace")
		_, err = dbroleclaimreconciler.Reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      resourceName,
				Namespace: newNamespace,
			},
		})
		Expect(err).NotTo(HaveOccurred())

		By("Getting the DbRoleClaim resource created in the new namespace")
		dbroleclaimdiffnamespace := &persistancev1.DbRoleClaim{}
		err = k8sClient.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: newNamespace}, dbroleclaimdiffnamespace)
		fmt.Printf("Status of DbRoleClaim: %+v", dbroleclaimdiffnamespace.Status)
		Expect(err).NotTo(HaveOccurred())

		By("Checking the secret in the new namespace")
		secretName = types.NamespacedName{
			Name:      "copy-secret",
			Namespace: newNamespace,
		}
		err = k8sClient.Get(ctx, secretName, secret)
		Expect(err).NotTo(HaveOccurred())

		fmt.Printf("Secret Name: %s, Secret Data: %s", secret.Name, secret.Data)
		dsnString := string(secret.Data["uri_dsn.txt"])

		By("Opening a connection to the database using the DSN from the secret in the new namespace")
		db, err := sql.Open("postgres", dsnString)
		if err == nil {
			fmt.Println("Connection to the database successful")
		}
		Expect(err).NotTo(HaveOccurred())
		defer db.Close()

		By("Running the query to get the tables in the new namespace")
		rows, err := db.Query("SELECT tablename, tableowner, pg_size_pretty(pg_total_relation_size(tablename::text)) AS size, obj_description(pg_class.oid) AS description FROM pg_tables JOIN pg_class ON tablename = relname WHERE schemaname = 'public'")
		Expect(err).NotTo(HaveOccurred())
		defer rows.Close()
		for rows.Next() {
			var tableName, tableOwner, tableSize string
			var tableDescription sql.NullString
			err := rows.Scan(&tableName, &tableOwner, &tableSize, &tableDescription)
			Expect(err).NotTo(HaveOccurred())
			description := ""
			if tableDescription.Valid {
				description = tableDescription.String
			}
			fmt.Printf("Table: %s, Owner: %s, Size: %s, Description: %s", tableName, tableOwner, tableSize, description)
		}
		Expect(rows.Err()).NotTo(HaveOccurred())
	})

	It("should fail to reconcile the resource", func() {
		RegisterFailHandler(Fail)
		logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

		type reconciler struct {
			Log                logr.Logger
			Scheme             *runtime.Scheme
			Config             *roleclaim.RoleConfig
			DbIdentifierPrefix string
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
					Config: &roleclaim.RoleConfig{
						Viper:     viperObj,
						Class:     "default",
						Namespace: "default",
					},
					Request: controllerruntime.Request{
						NamespacedName: typeNamespacedNameInvalidParam,
					},
					Log: zap.New(zap.UseDevMode(true)),
				},
				true,
			},
		}

		for _, tt := range tests {
			r := &DbRoleClaimReconciler{
				Client: k8sClient,
				Config: tt.rec.Config,
			}

			r.Reconciler = &roleclaim.DbRoleClaimReconciler{
				Client: r.Client,
				Config: r.Config,
			}

			_, err := r.Reconciler.Reconcile(context.Background(), tt.rec.Request)
			if (err != nil) != tt.wantErr {
				Expect(err).ToNot(BeNil())
				return
			}
		}
	})

})
