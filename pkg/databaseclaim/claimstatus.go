package databaseclaim

import (
	"context"
	"fmt"
	"time"

	v1 "github.com/infobloxopen/db-controller/api/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func (r *DatabaseClaimReconciler) manageError(ctx context.Context, dbClaim *v1.DatabaseClaim, inErr error) (ctrl.Result, error) {
	return manageError(ctx, r.Client, dbClaim, inErr)
}

type managedErr struct {
	err error
}

func (m *managedErr) Error() string {
	return m.err.Error()
}

func refreshClaim(ctx context.Context, cli client.Client, claim *v1.DatabaseClaim) error {
	nname := types.NamespacedName{
		Namespace: claim.Namespace,
		Name:      claim.Name,
	}

	logr := log.FromContext(ctx).WithValues("databaseclaim", nname)
	if err := cli.Get(ctx, nname, claim); err != nil {
		logr.Error(err, "refresh_claim")
		return err
	}

	return nil
}

// manageError updates the status of the DatabaseClaim to
// reflect the error that occurred. It returns the error that
// should be returned by the Reconcile function.
func manageError(ctx context.Context, cli client.Client, claim *v1.DatabaseClaim, inErr error) (ctrl.Result, error) {

	nname := types.NamespacedName{
		Namespace: claim.Namespace,
		Name:      claim.Name,
	}
	logr := log.FromContext(ctx).WithValues("databaseclaim", nname)

	// Prevent manageError being called multiple times on the same error
	wrappedErr, ok := inErr.(*managedErr)
	if ok {
		logr.Error(inErr, fmt.Sprintf("manageError called multiple times on the same error: %#v", inErr))
	} else {
		wrappedErr = &managedErr{err: inErr}
	}

	logr.Error(inErr, "")

	// Refresh the claim to get the latest resource version
	copy := claim.DeepCopy()
	if err := refreshClaim(ctx, cli, copy); err != nil {
		logr.Error(err, "manager_error_refresh_claim")
		return ctrl.Result{}, wrappedErr
	}

	copy.Status.Error = inErr.Error()
	if err := cli.Status().Update(ctx, copy); err != nil {
		logr.Error(err, "manager_error_update_claim")
		return ctrl.Result{}, wrappedErr
	}

	return ctrl.Result{}, wrappedErr
}

func (r *DatabaseClaimReconciler) manageSuccess(ctx context.Context, dbClaim *v1.DatabaseClaim) (ctrl.Result, error) {

	dbClaim.Status.Error = ""

	if err := r.Client.Status().Update(ctx, dbClaim); err != nil {
		return ctrl.Result{}, err
	}
	//if object is getting deleted then call requeue immediately
	if !dbClaim.ObjectMeta.DeletionTimestamp.IsZero() {
		return ctrl.Result{Requeue: true}, nil
	}
	if dbClaim.Status.OldDB.DbState == v1.PostMigrationInProgress {
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}
	return ctrl.Result{RequeueAfter: r.getPasswordRotationTime()}, nil
}

func (r *DatabaseClaimReconciler) updateClientStatus(ctx context.Context, dbClaim *v1.DatabaseClaim) error {

	return r.Client.Status().Update(ctx, dbClaim)
}
