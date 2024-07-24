package roleclaim

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/go-logr/logr"
	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
	"github.com/infobloxopen/db-controller/internal/dockerdb"
	basefun "github.com/infobloxopen/db-controller/pkg/basefunctions"
	"github.com/spf13/viper"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

type reconciler struct {
	Client             client.Client
	Log                logr.Logger
	Scheme             *runtime.Scheme
	Config             *RoleConfig
	DbIdentifierPrefix string
	Context            context.Context
	Request            controllerruntime.Request
}

var viperObj = viper.New()

func TestDBRoleClaimController_CreateSchemasAndRoles(t *testing.T) {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))
	RegisterFailHandler(Fail)

	_, dsn, close := dockerdb.Run(dockerdb.Config{
		Username: "mainUser",
		Password: "masterpassword",
		Database: "postgres",
	})
	defer close()

	viperObj.Set("passwordconfig::passwordRotationPeriod", 60)
	viperObj.Set("defaultMasterUsername", "root")
	viperObj.Set("defaultMasterPort", "5432")
	viperObj.Set("defaultSslMode", "require")
	viperObj.Set("defaultMinStorageGB", "10")
	viperObj.Set("defaultSslMode", "disable")

	tests := []struct {
		name    string
		rec     reconciler
		wantErr bool
	}{
		{
			"Get UserSchema claim 1",
			reconciler{
				Client: &mockClient{dsn: dsn},
				Config: &RoleConfig{
					Viper:     viperObj,
					Class:     "default",
					Namespace: "testNamespace",
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
		r := &DbRoleClaimReconciler{
			Client: tt.rec.Client,
			Config: tt.rec.Config,
		}

		result, err := r.Reconcile(tt.rec.Context, tt.rec.Request)
		Expect(err != nil != tt.wantErr).Should(BeFalse())

		Expect(result.Requeue).Should(BeFalse())

		existingDBConnInfo, err := persistancev1.ParseUri(dsn)
		Expect(err).ShouldNot(HaveOccurred())

		dbClient, err := basefun.GetClientForExistingDB(existingDBConnInfo, &controllerruntime.Log)
		Expect(err).ShouldNot(HaveOccurred())

		var responseUpdate = r.Client.(*mockClient).GetResponseUpdate()
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
	}
}

func TestDBRoleClaimController_ExistingSchemaRoleAndUser(t *testing.T) {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))
	RegisterFailHandler(Fail)

	_, dsn, close := dockerdb.Run(dockerdb.Config{
		Username: "mainUser",
		Password: "masterpassword",
		Database: "postgres",
	})
	defer close()

	viperObj.Set("passwordconfig::passwordRotationPeriod", 60)
	viperObj.Set("defaultMasterUsername", "root")
	viperObj.Set("defaultMasterPort", "5432")
	viperObj.Set("defaultSslMode", "require")
	viperObj.Set("defaultMinStorageGB", "10")
	viperObj.Set("defaultSslMode", "disable")

	test := struct {
		rec     reconciler
		wantErr bool
	}{
		reconciler{
			Client: &mockClient{dsn: dsn},
			Config: &RoleConfig{
				Viper:     viperObj,
				Class:     "default",
				Namespace: "testNamespace",
			},
			Request: controllerruntime.Request{
				NamespacedName: types.NamespacedName{Namespace: "schema-user-test", Name: "schema-user-claim-2"},
			},
			Log: zap.New(zap.UseDevMode(true)),
		},
		false,
	}

	r := &DbRoleClaimReconciler{
		Client: test.rec.Client,
		Config: test.rec.Config,
	}

	existingDBConnInfo, err := persistancev1.ParseUri(dsn)
	Expect(err).ShouldNot(HaveOccurred())

	dbClient, err := basefun.GetClientForExistingDB(existingDBConnInfo, &controllerruntime.Log)
	Expect(err).ShouldNot(HaveOccurred())

	//seed database to simulate existing user
	dbClient.CreateSchema("schema1")
	dbClient.CreateRegularRole(existingDBConnInfo.DatabaseName, "schema1_admin", "schema1")
	dbClient.CreateUser("testclaim_user_a", "schema1_admin", "123")

	result, err := r.Reconcile(test.rec.Context, test.rec.Request)
	Expect((err != nil) != test.wantErr).Should(BeFalse())

	Expect(result.Requeue).Should(BeFalse())

	var responseUpdate = r.Client.(*mockClient).GetResponseUpdate()
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

}

func TestDBRoleClaimController_RevokeRolesAndAssignNew(t *testing.T) {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))
	RegisterFailHandler(Fail)

	_, dsn, close := dockerdb.Run(dockerdb.Config{
		Username: "mainUser",
		Password: "masterpassword",
		Database: "postgres",
	})
	defer close()

	viperObj.Set("passwordconfig::passwordRotationPeriod", 60)

	test := struct {
		rec     reconciler
		wantErr bool
	}{
		reconciler{
			Client: &mockClient{dsn: dsn},
			Config: &RoleConfig{
				Viper:     viperObj,
				Class:     "default",
				Namespace: "testNamespace",
			},
			Request: controllerruntime.Request{
				NamespacedName: types.NamespacedName{Namespace: "schema-user-test", Name: "schema-user-claim-3"},
			},
			Log: zap.New(zap.UseDevMode(true)),
		},
		false,
	}

	r := &DbRoleClaimReconciler{
		Client: test.rec.Client,
		Config: test.rec.Config,
	}

	existingDBConnInfo, err := persistancev1.ParseUri(dsn)
	Expect(err).ShouldNot(HaveOccurred())

	dbClient, err := basefun.GetClientForExistingDB(existingDBConnInfo, &controllerruntime.Log)
	Expect(err).ShouldNot(HaveOccurred())

	//seed database to simulate existing user with access to 3 roles
	_, err = dbClient.CreateSchema("schema1")
	Expect(err).Should(BeNil())
	_, err = dbClient.CreateSchema("schema2")
	Expect(err).Should(BeNil())
	_, err = dbClient.CreateSchema("schema3")
	Expect(err).Should(BeNil())
	_, err = dbClient.CreateRegularRole(existingDBConnInfo.DatabaseName, "schema1_regular", "schema1")
	Expect(err).Should(BeNil())
	_, err = dbClient.CreateRegularRole(existingDBConnInfo.DatabaseName, "schema2_admin", "schema2")
	Expect(err).Should(BeNil())
	_, err = dbClient.CreateRegularRole(existingDBConnInfo.DatabaseName, "schema3_readonly", "schema3")
	Expect(err).Should(BeNil())
	_, err = dbClient.CreateUser("testclaim_user_a", "", "123")
	Expect(err).Should(BeNil())
	err = dbClient.AssignRoleToUser("testclaim_user_a", "schema1_regular")
	Expect(err).Should(BeNil())
	err = dbClient.AssignRoleToUser("testclaim_user_a", "schema2_admin")
	Expect(err).Should(BeNil())
	err = dbClient.AssignRoleToUser("testclaim_user_a", "schema3_readonly")
	Expect(err).Should(BeNil())

	result, err := r.Reconcile(test.rec.Context, test.rec.Request)
	Expect((err != nil) != test.wantErr).Should(BeFalse())

	Expect(result.Requeue).Should(BeFalse())

	var responseUpdate = r.Client.(*mockClient).GetResponseUpdate()
	Expect(responseUpdate).Should(Not(BeNil()))
	var schemaUserClaimStatus = responseUpdate.(*persistancev1.DbRoleClaim).Status
	Expect(schemaUserClaimStatus).Should(Not(BeNil()))
	Expect(schemaUserClaimStatus.Error).Should(BeEmpty())
	Expect(schemaUserClaimStatus.Username).Should(Equal("testclaim_user_a"))
	Expect(schemaUserClaimStatus.SchemaRoleStatus.SchemaStatus).Should(HaveLen(1))

	Expect(schemaUserClaimStatus.SchemaRoleStatus.SchemaStatus["schema4"]).Should(Equal("valid"))

	Expect(schemaUserClaimStatus.SchemaRoleStatus.RoleStatus["schema4_admin"]).Should(Equal("valid"))

	//-----------------
	//VERIFY THAT USER MUST HAVE ACCESS TO ONLY 1 ROLE AFTER RECONCILE
	userRoles, err := dbClient.GetUserRoles("testclaim_user_a")
	Expect(err).Should(BeNil())
	Expect(userRoles).Should(HaveLen(1))
	Expect(userRoles).Should(ContainElement("schema4_admin"))
	//-----------------
	exists, err := dbClient.SchemaExists("schema4")
	Expect(exists).Should(BeTrue())
	Expect(err).Should(BeNil())

	exists, err = dbClient.RoleExists("schema4_admin")
	Expect(exists).Should(BeTrue())
	Expect(err).Should(BeNil())

}
