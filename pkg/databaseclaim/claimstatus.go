package databaseclaim

import (
	"context"
	"errors"
	"fmt"
	"time"

	v1 "github.com/infobloxopen/db-controller/api/v1"
	"github.com/infobloxopen/db-controller/pkg/hostparams"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var (
	ErrDoNotUpdateStatus = fmt.Errorf("do not update status for this error")
)

func (r *DatabaseClaimReconciler) manageError(ctx context.Context, dbClaim *v1.DatabaseClaim, inErr error) (ctrl.Result, error) {
	// Class of errors that should stop the reconciliation loop
	// but not cause a status change on the CR
	if errors.Is(inErr, ErrDoNotUpdateStatus) {
		return ctrl.Result{RequeueAfter: r.getPasswordRotationTime()}, nil
	}
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

func SetDBClaimCondition(ctx context.Context, dbClaim *v1.DatabaseClaim, condition metav1.Condition) {
	logf := log.FromContext(ctx)

	condition.LastTransitionTime = metav1.Now()
	condition.ObservedGeneration = dbClaim.Generation

	for idx, cond := range dbClaim.Status.Conditions {
		if cond.Type == condition.Type {
			// No status change, so do not update LastTransitionTime
			if condition.Status == cond.Status && !condition.LastTransitionTime.IsZero() {
				condition.LastTransitionTime = cond.LastTransitionTime
			} else {
				logf.V(1).Info("Condition status changed %s -> %s", cond.Status, condition.Status)
			}
			dbClaim.Status.Conditions[idx] = condition
			return
		}
	}

	dbClaim.Status.Conditions = append(dbClaim.Status.Conditions, condition)
}

func (r *DatabaseClaimReconciler) manageSuccess(ctx context.Context, dbClaim *v1.DatabaseClaim) (ctrl.Result, error) {

	dbClaim.Status.Error = ""
	SetDBClaimCondition(ctx, dbClaim, metav1.Condition{
		Type:    "Ready",
		Status:  metav1.ConditionTrue,
		Reason:  "Ready",
		Message: "databaseclaim is up to date and ready",
	})

	if err := r.Client.Status().Update(ctx, dbClaim); err != nil {
		logr := log.FromContext(ctx).WithValues("databaseclaim", dbClaim.Name)
		logr.Error(err, "manage_success_update_claim")
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

func updateUserStatus(status *v1.Status, reqInfo *requestInfo, userName, userPassword string) {
	timeNow := metav1.Now()
	if status.ConnectionInfo == nil {
		status.ConnectionInfo = &v1.DatabaseClaimConnectionInfo{}
	}

	status.UserUpdatedAt = &timeNow
	status.ConnectionInfo.Username = userName
	reqInfo.TempSecret = userPassword
	status.ConnectionInfoUpdatedAt = &timeNow
}

func updateDBStatus(status *v1.Status, dbName string) {
	timeNow := metav1.Now()
	if status.DbCreatedAt == nil {
		status.DbCreatedAt = &timeNow
	}
	if status.ConnectionInfo == nil {
		status.ConnectionInfo = &v1.DatabaseClaimConnectionInfo{}
	}
	if status.ConnectionInfo.DatabaseName == "" {
		status.ConnectionInfo.DatabaseName = dbName
		status.ConnectionInfoUpdatedAt = &timeNow
	}
}

func updateHostPortStatus(status *v1.Status, host, port, sslMode string) {
	timeNow := metav1.Now()
	if status.ConnectionInfo == nil {
		status.ConnectionInfo = &v1.DatabaseClaimConnectionInfo{}
	}
	status.ConnectionInfo.Host = host
	status.ConnectionInfo.Port = port
	status.ConnectionInfo.SSLMode = sslMode
	status.ConnectionInfoUpdatedAt = &timeNow
}

func updateClusterStatus(status *v1.Status, hostParams *hostparams.HostParams) {
	status.DBVersion = hostParams.DBVersion
	status.Type = v1.DatabaseType(hostParams.Type)
	status.Shape = hostParams.Shape
	status.MinStorageGB = hostParams.MinStorageGB
	if hostParams.Type == string(v1.Postgres) {
		status.MaxStorageGB = hostParams.MaxStorageGB
	}
}
