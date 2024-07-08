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

	v1 "github.com/infobloxopen/db-controller/api/v1"
	"github.com/infobloxopen/db-controller/pkg/roleclaim"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type DbRoleClaimReconciler struct {
	client.Client
	Class      string
	Scheme     *runtime.Scheme
	Config     *roleclaim.RoleConfig
	reconciler *roleclaim.DbRoleClaimReconciler
}

const (
	dbClaimField  = ".spec.sourceDatabaseClaim.name"
	finalizerName = "dbroleclaims.persistance.atlas.infoblox.com/finalizer"
)

//+kubebuilder:rbac:groups=persistance.atlas.infoblox.com,resources=dbroleclaims,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=persistance.atlas.infoblox.com,resources=dbroleclaims/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=persistance.atlas.infoblox.com,resources=dbroleclaims/finalizers,verbs=update
//+kubebuilder:rbac:groups=persistance.atlas.infoblox.com,resources=databaseclaims/finalizers,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the DbRoleClaim object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.18.2/pkg/reconcile
func (r *DbRoleClaimReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	return r.reconciler.Reconcile(ctx, req)
}

// SetupWithManager sets up the controller with the Manager.
func (r *DbRoleClaimReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &v1.DbRoleClaim{}, dbClaimField, func(rawObj client.Object) []string {
		dbRoleClaim := rawObj.(*v1.DbRoleClaim)
		return []string{dbRoleClaim.Spec.SourceDatabaseClaim.Name}
	}); err != nil {
		return err
	}

	r.reconciler = &roleclaim.DbRoleClaimReconciler{
		Client: r.Client,
		Config: r.Config,
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.DbRoleClaim{}).
		Watches(
			client.Object(&v1.DatabaseClaim{}),
			handler.EnqueueRequestsFromMapFunc(r.findObjectsForDatabaseClaim),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Complete(r)
}

func (r *DbRoleClaimReconciler) findObjectsForDatabaseClaim(ctx context.Context, databaseClaim client.Object) []reconcile.Request {
	associatedDbRoleClaims := &v1.DbRoleClaimList{}
	listOps := &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(dbClaimField, databaseClaim.GetName()),
	}
	err := r.List(context.TODO(), associatedDbRoleClaims, listOps)
	if err != nil {
		return []reconcile.Request{}
	}

	requests := make([]reconcile.Request, len(associatedDbRoleClaims.Items))
	for i, item := range associatedDbRoleClaims.Items {
		requests[i] = reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      item.GetName(),
				Namespace: item.GetNamespace(),
			},
		}
	}
	return requests
}
