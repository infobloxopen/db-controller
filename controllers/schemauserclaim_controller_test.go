package controllers

import (
	"context"
	"strconv"
	"testing"

	"github.com/go-logr/logr"
	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
	. "github.com/infobloxopen/db-controller/pkg/dbclient"
	. "github.com/infobloxopen/db-controller/testutils"
	_ "github.com/lib/pq"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/viper"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func TestSchemaUserClaimReconcile(t *testing.T) {
	RegisterFailHandler(Fail)

	testDB := SetupSqlDB(t, "user_a", "masterpassword")
	defer testDB.Close()

	type reconciler struct {
		Client             client.Client
		Log                logr.Logger
		Scheme             *runtime.Scheme
		Config             *viper.Viper
		DbIdentifierPrefix string
		Context            context.Context
		Request            controllerruntime.Request
	}
	tests := []struct {
		name    string
		rec     reconciler
		want    int
		wantErr bool
	}{
		{
			"Get UserSchema claim 1",
			reconciler{
				Client: &MockClient{Port: strconv.Itoa(testDB.Port)},
				Config: NewConfig(complexityEnabled),
				Request: controllerruntime.Request{
					NamespacedName: types.NamespacedName{Namespace: "schema-user-test", Name: "schema-user-claim-1"},
				},
				Log: zap.New(zap.UseDevMode(true)),
			},
			15,
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &SchemaUserClaimReconciler{
				Client: tt.rec.Client,
				BaseReconciler: BaseReconciler{
					Log:    tt.rec.Log,
					Scheme: tt.rec.Scheme,
					Config: tt.rec.Config,
				},
			}
			result, err := r.Reconcile(tt.rec.Context, tt.rec.Request)
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

			dbClient, err := GetClientForExistingDB(existingDBConnInfo, &r.Log)
			if err != nil {
				t.Errorf("error = %v", err)
				return
			}

			var responseUpdate = r.Client.(*MockClient).GetResponseUpdate()
			Expect(responseUpdate).Should(Not(BeNil()))
			var schemaUserClaimStatus = responseUpdate.(*persistancev1.SchemaUserClaim).Status
			Expect(schemaUserClaimStatus).Should(Not(BeNil()))
			Expect(schemaUserClaimStatus.Error).Should(BeEmpty())
			Expect(schemaUserClaimStatus.Schemas).Should(HaveLen(6))

			Expect(schemaUserClaimStatus.Schemas[0].Name).Should(Equal("schema4"))
			Expect(schemaUserClaimStatus.Schemas[0].Status).Should(Equal("created"))
			Expect(schemaUserClaimStatus.Schemas[0].UsersStatus).Should(HaveLen(1))
			Expect(schemaUserClaimStatus.Schemas[0].UsersStatus[0].UserName).Should(Equal("userAlreadyCreated_b"))
			Expect(schemaUserClaimStatus.Schemas[0].UsersStatus[0].UserStatus).Should(Equal("created"))

			Expect(schemaUserClaimStatus.Schemas[1].Name).Should(Equal("schema0"))
			Expect(schemaUserClaimStatus.Schemas[1].Status).Should(Equal("created"))
			Expect(schemaUserClaimStatus.Schemas[1].UsersStatus).Should(BeEmpty())

			Expect(schemaUserClaimStatus.Schemas[2].Name).Should(Equal("schema1"))
			Expect(schemaUserClaimStatus.Schemas[2].Status).Should(Equal("created"))
			Expect(schemaUserClaimStatus.Schemas[2].UsersStatus).Should(HaveLen(1))
			Expect(schemaUserClaimStatus.Schemas[2].UsersStatus[0].UserName).Should(Equal("user1_a"))
			Expect(schemaUserClaimStatus.Schemas[2].UsersStatus[0].UserStatus).Should(Equal("created"))

			Expect(schemaUserClaimStatus.Schemas[3].Name).Should(Equal("schema2"))
			Expect(schemaUserClaimStatus.Schemas[3].Status).Should(Equal("created"))
			Expect(schemaUserClaimStatus.Schemas[3].UsersStatus).Should(HaveLen(2))
			Expect(schemaUserClaimStatus.Schemas[3].UsersStatus[0].UserName).Should(Equal("user2_1_a"))
			Expect(schemaUserClaimStatus.Schemas[3].UsersStatus[0].UserStatus).Should(Equal("created"))
			Expect(schemaUserClaimStatus.Schemas[3].UsersStatus[1].UserName).Should(Equal("user2_2_a"))

			Expect(schemaUserClaimStatus.Schemas[4].Name).Should(Equal("schema3"))
			Expect(schemaUserClaimStatus.Schemas[4].Status).Should(Equal("created"))
			Expect(schemaUserClaimStatus.Schemas[4].UsersStatus).Should(HaveLen(5))
			Expect(schemaUserClaimStatus.Schemas[4].UsersStatus[0].UserName).Should(Equal("user3_1_a"))
			Expect(schemaUserClaimStatus.Schemas[4].UsersStatus[0].UserStatus).Should(Equal("created"))
			Expect(schemaUserClaimStatus.Schemas[4].UsersStatus[1].UserName).Should(Equal("user3_2_a"))

			Expect(schemaUserClaimStatus.Schemas[5].Name).Should(Equal("public"))
			Expect(schemaUserClaimStatus.Schemas[5].Status).Should(Equal("created"))
			Expect(schemaUserClaimStatus.Schemas[5].UsersStatus).Should(HaveLen(1))
			Expect(schemaUserClaimStatus.Schemas[5].UsersStatus[0].UserName).Should(Equal("user4_a"))
			Expect(schemaUserClaimStatus.Schemas[5].UsersStatus[0].UserStatus).Should(Equal("created"))

			//-----------------
			exists, err := dbClient.SchemaExists("schema0")
			Expect(exists).Should(BeTrue())
			Expect(err).Should(BeNil())
			//-----------------
			exists, err = dbClient.SchemaExists("schema1")
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

			exists, err = dbClient.RoleExists("schema2_regular")
			Expect(exists).Should(BeTrue())
			Expect(err).Should(BeNil())
			exists, err = dbClient.UserExists("user2_1_a")
			Expect(exists).Should(BeTrue())
			Expect(err).Should(BeNil())

			exists, err = dbClient.RoleExists("schema2_admin")
			Expect(exists).Should(BeTrue())
			Expect(err).Should(BeNil())
			exists, err = dbClient.UserExists("user2_2_a")
			Expect(exists).Should(BeTrue())
			Expect(err).Should(BeNil())
			//-----------------
			exists, err = dbClient.SchemaExists("schema3")
			Expect(exists).Should(BeTrue())
			Expect(err).Should(BeNil())

			exists, err = dbClient.RoleExists("schema3_readonly")
			Expect(exists).Should(BeTrue())
			Expect(err).Should(BeNil())
			exists, err = dbClient.UserExists("user3_1_a")
			Expect(exists).Should(BeTrue())
			Expect(err).Should(BeNil())

			exists, err = dbClient.RoleExists("schema3_regular")
			Expect(exists).Should(BeTrue())
			Expect(err).Should(BeNil())
			exists, err = dbClient.UserExists("user3_2_a")
			Expect(exists).Should(BeTrue())
			Expect(err).Should(BeNil())

			exists, err = dbClient.RoleExists("schema3_admin")
			Expect(exists).Should(BeTrue())
			Expect(err).Should(BeNil())
			exists, err = dbClient.UserExists("user3_3_a")
			Expect(exists).Should(BeTrue())
			Expect(err).Should(BeNil())

			exists, err = dbClient.UserExists("user3_4_a")
			Expect(exists).Should(BeTrue())
			Expect(err).Should(BeNil())

			exists, err = dbClient.UserExists("user3_5_a")
			Expect(exists).Should(BeTrue())
			Expect(err).Should(BeNil())
			//----------PUBLIC-------
			exists, err = dbClient.RoleExists("public_admin")
			Expect(exists).Should(BeTrue())
			Expect(err).Should(BeNil())
			exists, err = dbClient.UserExists("user4_a")
			Expect(exists).Should(BeTrue())
			Expect(err).Should(BeNil())

		})
	}
}
