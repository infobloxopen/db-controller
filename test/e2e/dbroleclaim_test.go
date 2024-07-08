package e2e

import (
	"context"
	"strconv"

	"github.com/go-logr/logr"
	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
	basefun "github.com/infobloxopen/db-controller/pkg/basefunctions"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/viper"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	. "github.com/infobloxopen/db-controller/internal/controller"
	"github.com/infobloxopen/db-controller/pkg/roleclaim"
	. "github.com/infobloxopen/db-controller/testutils"
	_ "github.com/lib/pq"
)

var _ = Describe("DBRoleClaim Controller", func() {

	Context("Test With New Schemas Roles", func() {
		It("Create schemas and roles", func() {

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
						Client: &MockClient{Port: strconv.Itoa(TestDb.Port)},
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
				//t.Run(tt.name, func(t *testing.T) {
				r := &DbRoleClaimReconciler{
					Client: tt.rec.Client,
					Config: tt.rec.Config,
				}

				r.Reconciler = &roleclaim.DbRoleClaimReconciler{
					Client: r.Client,
					Config: r.Config,
				}

				result, err := r.Reconciler.Reconcile(tt.rec.Context, tt.rec.Request)
				Expect(err != nil != tt.wantErr).Should(BeFalse())

				Expect(result.Requeue).Should(BeFalse())

				existingDBConnInfo, err := persistancev1.ParseUri(TestDb.URL())
				Expect(err).ShouldNot(HaveOccurred())

				dbClient, err := basefun.GetClientForExistingDB(existingDBConnInfo, &controllerruntime.Log)
				Expect(err).ShouldNot(HaveOccurred())

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
				exists, err = dbClient.UserExists("testclaim_user_a")
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

				//})
			}
		})

		It("Update password", func() {
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
						Client: &MockClient{Port: strconv.Itoa(TestDb.Port)},
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
				//t.Run(tt.name, func(t *testing.T) {
				r := &DbRoleClaimReconciler{
					Client: tt.rec.Client,
					Config: tt.rec.Config,
				}

				r.Reconciler = &roleclaim.DbRoleClaimReconciler{
					Client: r.Client,
					Config: r.Config,
				}

				existingDBConnInfo, err := persistancev1.ParseUri(TestDb.URL())
				Expect(err).ShouldNot(HaveOccurred())

				dbClient, err := basefun.GetClientForExistingDB(existingDBConnInfo, &controllerruntime.Log)
				Expect(err).ShouldNot(HaveOccurred())

				//seed database to simulate existing user
				dbClient.CreateSchema("schema1")
				dbClient.CreateRegularRole(existingDBConnInfo.DatabaseName, "schema1_admin", "schema1")
				dbClient.CreateUser("user2_b", "schema1_admin", "123")

				result, err := r.Reconciler.Reconcile(tt.rec.Context, tt.rec.Request)
				Expect((err != nil) != tt.wantErr).Should(BeFalse())

				Expect(result.Requeue).Should(BeFalse())

				var responseUpdate = r.Client.(*MockClient).GetResponseUpdate()
				Expect(responseUpdate).Should(Not(BeNil()))
				var schemaUserClaimStatus = responseUpdate.(*persistancev1.DbRoleClaim).Status
				Expect(schemaUserClaimStatus).Should(Not(BeNil()))
				Expect(schemaUserClaimStatus.Error).Should(BeEmpty())
				Expect(schemaUserClaimStatus.Username).Should(Equal("testclaim_user_b"))
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
				exists, err = dbClient.UserExists("testclaim_user_b")
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
				//})
			}
		})
	})
})
