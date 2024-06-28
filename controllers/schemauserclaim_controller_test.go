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
			//-----------------
			exists, err := dbClient.SchemaExists("schema0")
			Expect(exists).Should(BeTrue())
			Expect(err).Should(BeNil())
			//-----------------
			exists, err = dbClient.SchemaExists("schema1")
			Expect(exists).Should(BeTrue())
			Expect(err).Should(BeNil())

			exists, err = dbClient.RoleExists("user1_regular")
			Expect(exists).Should(BeTrue())
			Expect(err).Should(BeNil())
			exists, err = dbClient.UserExists("user1_a")
			Expect(exists).Should(BeTrue())
			Expect(err).Should(BeNil())
			//-----------------
			exists, err = dbClient.SchemaExists("schema2")
			Expect(exists).Should(BeTrue())
			Expect(err).Should(BeNil())

			exists, err = dbClient.RoleExists("user2_1_regular")
			Expect(exists).Should(BeTrue())
			Expect(err).Should(BeNil())
			exists, err = dbClient.UserExists("user2_1_a")
			Expect(exists).Should(BeTrue())
			Expect(err).Should(BeNil())

			exists, err = dbClient.RoleExists("user2_2_admin")
			Expect(exists).Should(BeTrue())
			Expect(err).Should(BeNil())
			exists, err = dbClient.UserExists("user2_2_a")
			Expect(exists).Should(BeTrue())
			Expect(err).Should(BeNil())
			//-----------------
			exists, err = dbClient.SchemaExists("schema3")
			Expect(exists).Should(BeTrue())
			Expect(err).Should(BeNil())

			exists, err = dbClient.RoleExists("user3_1_readonly")
			Expect(exists).Should(BeTrue())
			Expect(err).Should(BeNil())
			exists, err = dbClient.UserExists("user3_1_a")
			Expect(exists).Should(BeTrue())
			Expect(err).Should(BeNil())

			exists, err = dbClient.RoleExists("user3_2_regular")
			Expect(exists).Should(BeTrue())
			Expect(err).Should(BeNil())
			exists, err = dbClient.UserExists("user3_2_a")
			Expect(exists).Should(BeTrue())
			Expect(err).Should(BeNil())

			exists, err = dbClient.RoleExists("user3_3_admin")
			Expect(exists).Should(BeTrue())
			Expect(err).Should(BeNil())
			exists, err = dbClient.UserExists("user3_3_a")
			Expect(exists).Should(BeTrue())
			Expect(err).Should(BeNil())
			//----------PUBLIC-------
			exists, err = dbClient.RoleExists("user4_admin")
			Expect(exists).Should(BeTrue())
			Expect(err).Should(BeNil())
			exists, err = dbClient.UserExists("user4_a")
			Expect(exists).Should(BeTrue())
			Expect(err).Should(BeNil())

		})
	}
}
