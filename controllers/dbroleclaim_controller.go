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

package controllers

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	//    "k8s.io/apimachinery/pkg/fields" // Required for Watching
	// "k8s.io/apimachinery/pkg/types" // Required for Watching
	// "sigs.k8s.io/controller-runtime/pkg/builder" // Required for Watching
	// "sigs.k8s.io/controller-runtime/pkg/handler" // Required for Watching
	// "sigs.k8s.io/controller-runtime/pkg/predicate" // Required for Watching
	// "sigs.k8s.io/controller-runtime/pkg/reconcile" // Required for Watching
	// "sigs.k8s.io/controller-runtime/pkg/source" // Required for Watching

	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
)

const (
	dbClaimField          = ".spec.sourceDatabaseClaim.name"
	dbClaimNamespaceField = ".spec.sourceDatabaseClaim.namespace"
)

// DbRoleClaimReconciler reconciles a DbRoleClaim object
type DbRoleClaimReconciler struct {
	Class string
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=persistance.atlas.infoblox.com,resources=dbroleclaims,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=persistance.atlas.infoblox.com,resources=dbroleclaims/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=persistance.atlas.infoblox.com,resources=dbroleclaims/finalizers,verbs=update
//+kubebuilder:rbac:groups=persistance.atlas.infoblox.com,resources=databaseclaims/finalizers,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the DbRoleClaim object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.14.1/pkg/reconcile
func (r *DbRoleClaimReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx).WithValues("databaserole", req.NamespacedName)

	return ctrl.Result{}, nil

}

// SetupWithManager sets up the controller with the Manager.
func (r *DbRoleClaimReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&persistancev1.DbRoleClaim{}).
		Complete(r)
}

func (r *DbRoleClaimReconciler) isClassPermitted(claimClass string) bool {
	// r.Log.Info("in isClassPermitted", "claimClass", claimClass, "r.Class", r.Class)
	controllerClass := r.Class

	if claimClass == "" {
		claimClass = "default"
	}
	if controllerClass == "" {
		controllerClass = "default"
	}
	if claimClass != controllerClass {
		return false
	}

	return true
}
