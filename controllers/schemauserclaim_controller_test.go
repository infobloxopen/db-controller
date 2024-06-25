package controllers

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
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
				Client: &MockClient{},
				Config: NewConfig(complexityEnabled),
				Request: controllerruntime.Request{
					NamespacedName: types.NamespacedName{Namespace: "schema-user-test", Name: "schema-user-claim-1"},
				},
				Log: zap.New(zap.UseDevMode(true)),
			},
			15,
			false,
		},
		{
			"Get UserSchema claim 2",
			reconciler{
				Client: &MockClient{},
				Config: NewConfig(complexityDisabled),
				Request: controllerruntime.Request{
					NamespacedName: types.NamespacedName{Namespace: "schema-user-test", Name: "schema-user-claim-2"},
				},
				Log: zap.New(zap.UseDevMode(true)),
			},
			defaultPassLen,
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &SchemaUserClaimReconciler{
				Client: tt.rec.Client,
				Log:    tt.rec.Log,
				Scheme: tt.rec.Scheme,
			}
			schemas, err := r.Reconcile(tt.rec.Context, tt.rec.Request)
			if (err != nil) != tt.wantErr {
				t.Errorf("error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			Expect(schemas.Requeue).Should(BeFalse())
		})
	}
}
