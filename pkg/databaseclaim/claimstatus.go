package databaseclaim

import (
	"context"
	"errors"
	"fmt"
	"time"

	v1 "github.com/infobloxopen/db-controller/api/v1"
	basefun "github.com/infobloxopen/db-controller/pkg/basefunctions"
	"github.com/infobloxopen/db-controller/pkg/hostparams"
	"github.com/infobloxopen/db-controller/pkg/pgctl"
	"github.com/spf13/viper"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var (
	ErrDoNotUpdateStatus = fmt.Errorf("do not update status for this error")
)

type managedErr struct {
	err error
}

func (m *managedErr) Error() string {
	return m.err.Error()
}

type StatusManager struct {
	client               client.Client
	passwordRotationTime time.Duration
}

func NewStatusManager(c client.Client, viper *viper.Viper) *StatusManager {
	return &StatusManager{client: c, passwordRotationTime: basefun.GetPasswordRotationPeriod(viper)}
}

func (m *StatusManager) UpdateStatus(ctx context.Context, dbClaim *v1.DatabaseClaim) error {
	return m.client.Status().Update(ctx, dbClaim)
}

func (m *StatusManager) SetErrorStatus(ctx context.Context, dbClaim *v1.DatabaseClaim, inErr error) (reconcile.Result, error) {
	// If the error is non-critical and doesn't require a status update, skip processing
	if errors.Is(inErr, ErrDoNotUpdateStatus) {
		return ctrl.Result{}, nil
	}

	nname := types.NamespacedName{
		Namespace: dbClaim.Namespace,
		Name:      dbClaim.Name,
	}
	logr := log.FromContext(ctx).WithValues("databaseclaim", nname)

	m.SetStatusCondition(ctx, dbClaim, ReconcileErrorCondition(inErr))
	var wrappedErr *managedErr
	if existingErr, isManaged := inErr.(*managedErr); isManaged {
		logr.Error(existingErr, "manageError called multiple times for the same error")
		wrappedErr = existingErr
	} else {
		wrappedErr = &managedErr{err: inErr}
	}

	refreshedClaim := dbClaim.DeepCopy()
	if err := m.client.Get(ctx, nname, refreshedClaim); err != nil {
		logr.Error(err, "Failed to refresh DatabaseClaim")
		return ctrl.Result{}, wrappedErr
	}

	refreshedClaim.Status.Error = wrappedErr.Error()
	if err := m.UpdateStatus(ctx, dbClaim); err != nil {
		logr.Error(err, "Failed to update DatabaseClaim status")
		return ctrl.Result{}, wrappedErr
	}

	logr.Info("DatabaseClaim status updated with error", "error", wrappedErr.Error())
	return ctrl.Result{}, wrappedErr
}

func (m *StatusManager) SuccessAndUpdateCondition(ctx context.Context, dbClaim *v1.DatabaseClaim) (reconcile.Result, error) {
	logf := log.FromContext(ctx).WithValues("databaseclaim", dbClaim.Name)

	dbClaim.Status.Error = ""
	m.SetStatusCondition(ctx, dbClaim, ReconcileSuccessCondition())
	if err := m.UpdateStatus(ctx, dbClaim); err != nil {
		logf.Error(err, "Error updating DatabaseClaim status")
		return ctrl.Result{}, err
	}

	//if object is getting deleted then call requeue immediately
	if !dbClaim.ObjectMeta.DeletionTimestamp.IsZero() {
		return ctrl.Result{Requeue: true}, nil
	}

	if dbClaim.Status.OldDB.DbState == v1.PostMigrationInProgress {
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}

	return ctrl.Result{RequeueAfter: m.passwordRotationTime}, nil
}

func (m *StatusManager) SetStatusCondition(ctx context.Context, dbClaim *v1.DatabaseClaim, condition metav1.Condition) {
	logf := log.FromContext(ctx).WithValues("databaseclaim", dbClaim.Name)

	condition.LastTransitionTime = metav1.Now()
	condition.ObservedGeneration = dbClaim.Generation

	for idx, cond := range dbClaim.Status.Conditions {
		if cond.Type == condition.Type {
			// No status change, so do not update LastTransitionTime
			if condition.Status == cond.Status && !condition.LastTransitionTime.IsZero() {
				condition.LastTransitionTime = cond.LastTransitionTime
			} else {
				logf.V(1).Info("Condition status changed %s -> %s", string(cond.Status), string(condition.Status))
			}
			dbClaim.Status.Conditions[idx] = condition
			return
		}
	}

	dbClaim.Status.Conditions = append(dbClaim.Status.Conditions, condition)
}

func (m *StatusManager) UpdateClusterStatus(status *v1.Status, hostParams *hostparams.HostParams) {
	status.DBVersion = hostParams.DBVersion
	status.Type = v1.DatabaseType(hostParams.Type)
	status.Shape = hostParams.Shape
	status.MinStorageGB = hostParams.MinStorageGB
	if hostParams.Type == string(v1.Postgres) {
		status.MaxStorageGB = hostParams.MaxStorageGB
	}
}

func (m *StatusManager) UpdateDBStatus(status *v1.Status, dbName string) {
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

func (m *StatusManager) UpdateHostPortStatus(status *v1.Status, host string, port string, sslMode string) {
	timeNow := metav1.Now()
	if status.ConnectionInfo == nil {
		status.ConnectionInfo = &v1.DatabaseClaimConnectionInfo{}
	}
	status.ConnectionInfo.Host = host
	status.ConnectionInfo.Port = port
	status.ConnectionInfo.SSLMode = sslMode
	status.ConnectionInfoUpdatedAt = &timeNow
}

func (m *StatusManager) UpdateUserStatus(status *v1.Status, reqInfo *requestInfo, userName string, userPassword string) {
	timeNow := metav1.Now()
	if status.ConnectionInfo == nil {
		status.ConnectionInfo = &v1.DatabaseClaimConnectionInfo{}
	}

	status.UserUpdatedAt = &timeNow
	status.ConnectionInfo.Username = userName
	reqInfo.TempSecret = userPassword
	status.ConnectionInfoUpdatedAt = &timeNow
}

func (m *StatusManager) MigrationInProgressStatus(ctx context.Context, dbClaim *v1.DatabaseClaim) (reconcile.Result, error) {
	dbClaim.Status.MigrationState = pgctl.S_MigrationInProgress.String()

	m.SetStatusCondition(ctx, dbClaim, MigratingCondition())

	err := m.UpdateStatus(ctx, dbClaim)
	return ctrl.Result{Requeue: true}, err
}
