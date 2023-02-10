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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
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
	finalizerName         = "dbroleclaims.persistance.atlas.infoblox.com/finalizer"
)

// DbRoleClaimReconciler reconciles a DbRoleClaim object
type DbRoleClaimReconciler struct {
	Class string
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

//+kubebuilder:rbac:groups=persistance.atlas.infoblox.com,resources=dbroleclaims,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=persistance.atlas.infoblox.com,resources=dbroleclaims/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=persistance.atlas.infoblox.com,resources=dbroleclaims/finalizers,verbs=update
//+kubebuilder:rbac:groups=persistance.atlas.infoblox.com,resources=databaseclaims/finalizers,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

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
	log := log.FromContext(ctx).WithValues("databaserole", req.NamespacedName)
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

	if dbRoleClaim.Spec.SourceDatabaseClaim.Name != "" {
		dbclaimName := dbRoleClaim.Spec.SourceDatabaseClaim.Name
		dbclaimNamespace := dbRoleClaim.Spec.SourceDatabaseClaim.Namespace
		foundDbClaim := &persistancev1.DatabaseClaim{}
		err := r.Get(ctx, types.NamespacedName{Name: dbclaimName, Namespace: dbclaimNamespace}, foundDbClaim)
		if err != nil {
			// If a configMap name is provided, then it must exist
			// You will likely want to create an Event for the user to understand why their reconcile is failing.
			log.Error(err, "specified dbclaim not found", "dbclaimname", dbclaimName, "dbclaimNamespace", dbclaimNamespace)
			r.Recorder.Event(&dbRoleClaim, "Warning", "Not found", fmt.Sprintf("DatabaseClaim %s/%s", dbclaimNamespace, dbclaimName))
			return ctrl.Result{}, err
		}
		log.Info("found dbclaim", "secretName", foundDbClaim.Spec.SecretName)
		dbRoleClaim.Status.MatchedSourceClaim = foundDbClaim.Namespace + "/" + foundDbClaim.Name
		r.Recorder.Event(&dbRoleClaim, "Normal", "Found", fmt.Sprintf("DatabaseClaim %s/%s", dbclaimNamespace, dbclaimName))

		foundSecret := &corev1.Secret{}
		err = r.Get(ctx, types.NamespacedName{Name: foundDbClaim.Spec.SecretName, Namespace: dbclaimNamespace}, foundSecret)
		if err != nil {
			log.Error(err, "specified dbclaimSecret not found", "dbclaim secret name", foundDbClaim.Spec.SecretName, "dbclaimNamespace", dbclaimNamespace)
			r.Recorder.Event(&dbRoleClaim, "Warning", "Not found", fmt.Sprintf("Secret %s/%s", dbclaimNamespace, foundDbClaim.Spec.SecretName))
			return ctrl.Result{}, err
		}
		log.Info("found dbclaimsecret", "secretName", foundDbClaim.Spec.SecretName, "secret", foundSecret)

		dbRoleClaim.Status.SourceSecret = foundSecret.Namespace + "/" + foundSecret.Name
		r.Recorder.Event(&dbRoleClaim, "Normal", "Found", fmt.Sprintf("Secret %s/%s", dbclaimNamespace, foundDbClaim.Spec.SecretName))

		if err = r.copySourceSecret(ctx, foundSecret, &dbRoleClaim); err != nil {
			r.Recorder.Event(&dbRoleClaim, "Warning", "Update Failed", fmt.Sprintf("Secret %s/%s", dbRoleClaim.Namespace, dbRoleClaim.Spec.SecretName))
			return r.manageError(ctx, &dbRoleClaim, err)
		}
		r.Recorder.Event(&dbRoleClaim, "Normal", "Updated", fmt.Sprintf("Secret %s/%s", dbRoleClaim.Namespace, dbRoleClaim.Spec.SecretName))
	}

	return r.manageSuccess(ctx, &dbRoleClaim)

}

// SetupWithManager sets up the controller with the Manager.
func (r *DbRoleClaimReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&persistancev1.DbRoleClaim{}).
		Complete(r)
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
	// r.Log.Error(inErr, "error")
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

func (r *DbRoleClaimReconciler) copySourceSecret(ctx context.Context, sourceSecret *corev1.Secret, dbRoleClaim *persistancev1.DbRoleClaim) error {

	secretName := dbRoleClaim.Spec.SecretName
	truePtr := true
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
						Controller:         &truePtr,
						BlockOwnerDeletion: &truePtr,
					},
				},
			},
			Data: sourceSecretData,
		}
		// r.Log.Info("creating connection info secret", "secret", secret.Name, "namespace", secret.Namespace)

		r.Client.Create(ctx, role_secret)
	} else {
		role_secret.Data = sourceSecretData
		return r.Client.Update(ctx, role_secret)
	}
	return nil
}
