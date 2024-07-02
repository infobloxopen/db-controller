/*
Copyright 2022.

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

	"github.com/go-logr/logr"
	v1 "github.com/infobloxopen/db-controller/api/v1"
	"github.com/infobloxopen/db-controller/pkg/schemauserclaim"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type SchemaUserClaimReconciler struct {
	client.Client
	// Class      string
	// Config     *viper.Viper
	Log    logr.Logger
	Scheme *runtime.Scheme
	// Recorder   record.EventRecorder
	Config     *schemauserclaim.SchemaUserConfig
	reconciler *schemauserclaim.SchemaUserClaimReconciler
}

// FIXME: Temporary workaround to spliting up the reconciler
// code
func (r *SchemaUserClaimReconciler) Reconciler() *schemauserclaim.SchemaUserClaimReconciler {
	return r.reconciler
}

//+kubebuilder:rbac:groups=persistance.atlas.infoblox.com,resources=schemauserclaims,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=persistance.atlas.infoblox.com,resources=schemauserclaims/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=persistance.atlas.infoblox.com,resources=schemauserclaims/finalizers,verbs=update
//+kubebuilder:rbac:groups=persistance.atlas.infoblox.com,resources=databaseclaims/finalizers,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch

func (r *SchemaUserClaimReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	return r.reconciler.Reconcile(ctx, req)
}

// SetupWithManager sets up the controller with the Manager.
func (r *SchemaUserClaimReconciler) SetupWithManager(mgr ctrl.Manager) error {

	r.reconciler = &schemauserclaim.SchemaUserClaimReconciler{
		Client: r.Client,
		Config: r.Config,
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.SchemaUserClaim{}).
		Complete(r)
}
