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
	"fmt"

	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	dbClaimField  = ".spec.sourceDatabaseClaim.name"
	finalizerName = "dbroleclaims.persistance.atlas.infoblox.com/finalizer"
)

type DbRoleClaimReconciler struct {
	client.Client
	Class    string
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

//+kubebuilder:rbac:groups=persistance.atlas.infoblox.com,resources=dbroleclaims,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=persistance.atlas.infoblox.com,resources=dbroleclaims/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=persistance.atlas.infoblox.com,resources=dbroleclaims/finalizers,verbs=update
//+kubebuilder:rbac:groups=persistance.atlas.infoblox.com,resources=databaseclaims/finalizers,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch

func (r *DbRoleClaimReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx).WithValues("databaserole", req.NamespacedName).V(InfoLevel)
	var dbRoleClaim persistancev1.DbRoleClaim
	if err := r.Get(ctx, req.NamespacedName, &dbRoleClaim); err != nil {
		log.Error(err, "unable to fetch DatabaseClaim")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if permitted := r.isClassPermitted(*dbRoleClaim.Spec.Class); !permitted {
		log.Info("ignoring this claim as this controller does not own this class", "claimClass", *dbRoleClaim.Spec.Class, "controllerClas", r.Class)
		return ctrl.Result{}, nil
	}

	isObjectDeleted, err := r.deleteWorkflow(ctx, &dbRoleClaim)
	if err != nil {
		log.Error(err, "error in delete workflow")
		return ctrl.Result{}, err
	}
	if isObjectDeleted {
		return ctrl.Result{}, nil
	}

	if dbRoleClaim.Spec.SourceDatabaseClaim.Name == "" {
		log.Error(fmt.Errorf("sourcedatabaseclaim cannot be nil"), "invalid_spec_source_database_claim_name")
		return r.manageError(ctx, &dbRoleClaim, fmt.Errorf("sourcedatabaseclaim cannot be nil"))
	}

	dbclaimName := dbRoleClaim.Spec.SourceDatabaseClaim.Name
	dbclaimNamespace := dbRoleClaim.Spec.SourceDatabaseClaim.Namespace
	foundDbClaim := &persistancev1.DatabaseClaim{}
	err = r.Get(ctx, types.NamespacedName{Name: dbclaimName, Namespace: dbclaimNamespace}, foundDbClaim)
	if err != nil {
		log.Error(err, "specified dbclaim not found", "dbclaimname", dbclaimName, "dbclaimNamespace", dbclaimNamespace)
		r.Recorder.Event(&dbRoleClaim, "Warning", "Not found", fmt.Sprintf("DatabaseClaim %s/%s", dbclaimNamespace, dbclaimName))
		dbRoleClaim.Status.MatchedSourceClaim = ""
		return r.manageError(ctx, &dbRoleClaim, fmt.Errorf("%s dbclaim not found", dbclaimName))
	}
	log.Info("found dbclaim", "secretName", foundDbClaim.Spec.SecretName)
	dbRoleClaim.Status.MatchedSourceClaim = foundDbClaim.Namespace + "/" + foundDbClaim.Name
	r.Recorder.Event(&dbRoleClaim, "Normal", "Found", fmt.Sprintf("DatabaseClaim %s/%s", dbclaimNamespace, dbclaimName))

	foundSecret := &corev1.Secret{}
	err = r.Get(ctx, types.NamespacedName{Name: foundDbClaim.Spec.SecretName, Namespace: dbclaimNamespace}, foundSecret)
	if err != nil {
		log.Error(err, "dbclaim_secret_not_found", "secret_name", foundDbClaim.Spec.SecretName, "secret_namespace", dbclaimNamespace)
		r.Recorder.Event(&dbRoleClaim, "Warning", "Not found", fmt.Sprintf("Secret %s/%s", dbclaimNamespace, foundDbClaim.Spec.SecretName))
		dbRoleClaim.Status.SourceSecret = ""
		return r.manageError(ctx, &dbRoleClaim, fmt.Errorf("%s source secret not found", foundDbClaim.Spec.SecretName))
	}
	log.V(DebugLevel).Info("dbclaim_secret", "secret_name", foundDbClaim.Spec.SecretName, "secret_namespace", dbclaimNamespace)

	dbRoleClaim.Status.SourceSecret = foundSecret.Namespace + "/" + foundSecret.Name
	r.Recorder.Event(&dbRoleClaim, "Normal", "Found", fmt.Sprintf("Secret %s/%s", dbclaimNamespace, foundDbClaim.Spec.SecretName))

	if foundSecret.GetResourceVersion() == dbRoleClaim.Status.SourceSecretResourceVersion {
		log.Info("source secret has not changed, update not called",
			"sourceVersion", foundSecret.GetResourceVersion(),
			"statusVersion", dbRoleClaim.Status.SourceSecretResourceVersion)
		return r.manageSuccess(ctx, &dbRoleClaim)
	}

	if err = r.copySourceSecret(ctx, foundSecret, &dbRoleClaim); err != nil {
		log.Error(err, "failed_copy_source_secret")
		r.Recorder.Event(&dbRoleClaim, "Warning", "Update Failed", fmt.Sprintf("Secret %s/%s", dbRoleClaim.Namespace, dbRoleClaim.Spec.SecretName))
		return r.manageError(ctx, &dbRoleClaim, err)
	}

	dbRoleClaim.Status.SourceSecretResourceVersion = foundSecret.GetResourceVersion()
	timeNow := metav1.Now()
	dbRoleClaim.Status.SecretUpdatedAt = &timeNow
	r.Recorder.Event(&dbRoleClaim, "Normal", "Updated", fmt.Sprintf("Secret %s/%s", dbRoleClaim.Namespace, dbRoleClaim.Spec.SecretName))

	return r.manageSuccess(ctx, &dbRoleClaim)
}

// SetupWithManager sets up the controller with the Manager.
func (r *DbRoleClaimReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &persistancev1.DbRoleClaim{}, dbClaimField, func(rawObj client.Object) []string {
		dbRoleClaim := rawObj.(*persistancev1.DbRoleClaim)
		return []string{dbRoleClaim.Spec.SourceDatabaseClaim.Name}
	}); err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&persistancev1.DbRoleClaim{}).
		Watches(
			client.Object(&persistancev1.DatabaseClaim{}),
			handler.EnqueueRequestsFromMapFunc(r.findObjectsForDatabaseClaim),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Complete(r)
}

func (r *DbRoleClaimReconciler) findObjectsForDatabaseClaim(ctx context.Context, databaseClaim client.Object) []reconcile.Request {
	associatedDbRoleClaims := &persistancev1.DbRoleClaimList{}
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

func (r *DbRoleClaimReconciler) deleteWorkflow(ctx context.Context, dbRoleClaim *persistancev1.DbRoleClaim) (bool, error) {

	// examine DeletionTimestamp to determine if object is under deletion
	if dbRoleClaim.ObjectMeta.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(dbRoleClaim, finalizerName) {
			controllerutil.AddFinalizer(dbRoleClaim, finalizerName)
			if err := r.Update(ctx, dbRoleClaim); err != nil {
				return false, err
			}
		}
		return false, nil
	}
	// The object is being deleted
	if controllerutil.ContainsFinalizer(dbRoleClaim, finalizerName) {
		// remove our finalizer from the list and update it.
		controllerutil.RemoveFinalizer(dbRoleClaim, finalizerName)
		if err := r.Update(ctx, dbRoleClaim); err != nil {
			return false, err
		}
	}
	// Stop reconciliation as the item is being deleted
	return true, nil
}

func (r *DbRoleClaimReconciler) isClassPermitted(claimClass string) bool {
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

func (r *DbRoleClaimReconciler) manageError(ctx context.Context, dbRoleClaim *persistancev1.DbRoleClaim, inErr error) (ctrl.Result, error) {

	dbRoleClaim.Status.Error = inErr.Error()

	err := r.Client.Status().Update(ctx, dbRoleClaim)
	if err != nil {
		// Ignore conflicts, resource might just be outdated.
		if errors.IsConflict(err) {
			err = nil
		}
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, inErr
}

func (r *DbRoleClaimReconciler) manageSuccess(ctx context.Context, dbRoleClaim *persistancev1.DbRoleClaim) (ctrl.Result, error) {
	dbRoleClaim.Status.Error = ""

	err := r.Client.Status().Update(ctx, dbRoleClaim)
	if err != nil {
		// Ignore conflicts, resource might just be outdated.
		if errors.IsConflict(err) {
			err = nil
		}
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *DbRoleClaimReconciler) copySourceSecret(ctx context.Context, sourceSecret *corev1.Secret,
	dbRoleClaim *persistancev1.DbRoleClaim) error {
	log := log.FromContext(ctx).WithValues("databaserole", "copySourceSecret")

	secretName := dbRoleClaim.Spec.SecretName
	sourceSecretData := sourceSecret.Data
	role_secret := &corev1.Secret{}

	err := r.Client.Get(ctx, client.ObjectKey{
		Namespace: dbRoleClaim.Namespace,
		Name:      secretName,
	}, role_secret)
	if err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
		role_secret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: dbRoleClaim.Namespace,
				Name:      secretName,
				Labels:    map[string]string{"app.kubernetes.io/managed-by": "dbrole-controller"},
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion:         "persistance.atlas.infoblox.com/v1",
						Kind:               "DbRoleClaim",
						Name:               dbRoleClaim.Name,
						UID:                dbRoleClaim.UID,
						Controller:         ptr.To(true),
						BlockOwnerDeletion: ptr.To(true),
					},
				},
			},
			Data: sourceSecretData,
		}
		log.Info("creating secret", "secret", secretName, "namespace", dbRoleClaim.Namespace)
		return r.Client.Create(ctx, role_secret)
	}

	role_secret.Data = sourceSecretData
	log.Info("updating secret", "secret", secretName, "namespace", dbRoleClaim.Namespace)
	return r.Client.Update(ctx, role_secret)

}
