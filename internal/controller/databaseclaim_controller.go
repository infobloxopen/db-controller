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

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
	"github.com/infobloxopen/db-controller/pkg/databaseclaim"
)

// DatabaseClaimReconciler reconciles a DatabaseClaim object
type DatabaseClaimReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	Config     *databaseclaim.DatabaseClaimConfig
	reconciler *databaseclaim.DatabaseClaimReconciler
}

// FIXME: Temporary workaround to spliting up the reconciler
// code
func (r *DatabaseClaimReconciler) Reconciler() *databaseclaim.DatabaseClaimReconciler {
	return r.reconciler
}

// +kubebuilder:rbac:groups=persistance.atlas.infoblox.com,resources=databaseclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=persistance.atlas.infoblox.com,resources=databaseclaims/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=persistance.atlas.infoblox.com,resources=databaseclaims/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the DatabaseClaim object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.18.2/pkg/reconcile
func (r *DatabaseClaimReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	return r.reconciler.Reconcile(ctx, req)
}

func (r *DatabaseClaimReconciler) Setup() {
	r.reconciler = &databaseclaim.DatabaseClaimReconciler{
		Client: r.Client,
		Config: r.Config,
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *DatabaseClaimReconciler) SetupWithManager(mgr ctrl.Manager) error {

	r.Setup()

	return ctrl.NewControllerManagedBy(mgr).
		For(&persistancev1.DatabaseClaim{}).
		Complete(r)
}
