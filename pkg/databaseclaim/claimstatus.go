package databaseclaim

import (
	"context"
	"errors"
	"fmt"
	"time"

	v1 "github.com/infobloxopen/db-controller/api/v1"
	basefun "github.com/infobloxopen/db-controller/pkg/basefunctions"
	"github.com/infobloxopen/db-controller/pkg/hostparams"
	"github.com/spf13/viper"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	TypeUseExistingDB           = "UseExistingDB"
	TypeMigrationToNewDB        = "MigrationToNewDB"
	TypeMigrationInProgress     = "MigrationInProgress"
	TypeUseNewDB                = "UseNewDB"
	TypeDBUpgradeInitiated      = "DBUpgradeInitiated"
	TypeDBUpgradeInProgress     = "DBUpgradeInProgress"
	TypePostMigrationInProgress = "PostMigrationInProgress"
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

type StatusManager interface {
	SetStatusCondition(ctx context.Context, dbClaim *v1.DatabaseClaim, condition metav1.Condition)
	SuccessAndUpdateCondition(ctx context.Context, dbClaim *v1.DatabaseClaim, condType string) (ctrl.Result, error)
	SetErrorStatus(ctx context.Context, dbClaim *v1.DatabaseClaim, inErr error) (ctrl.Result, error)
	UpdateStatus(ctx context.Context, dbClaim *v1.DatabaseClaim) error
	UpdateUserStatus(status *v1.Status, reqInfo *requestInfo, userName, userPassword string)
	UpdateDBStatus(status *v1.Status, dbName string)
	UpdateHostPortStatus(status *v1.Status, host, port, sslMode string)
	UpdateClusterStatus(status *v1.Status, hostParams *hostparams.HostParams)
}

type manager struct {
	client               client.Client
	passwordRotationTime time.Duration
}

func NewStatusManager(c client.Client, viper *viper.Viper) StatusManager {
	return &manager{client: c, passwordRotationTime: basefun.GetPasswordRotationPeriod(viper)}
}

func (m *manager) UpdateStatus(ctx context.Context, dbClaim *v1.DatabaseClaim) error {
	return m.client.Status().Update(ctx, dbClaim)
}

func (m *manager) SetErrorStatus(ctx context.Context, dbClaim *v1.DatabaseClaim, inErr error) (reconcile.Result, error) {
	// If the error is non-critical and doesn't require a status update, skip processing
	if errors.Is(inErr, ErrDoNotUpdateStatus) {
		return ctrl.Result{}, nil
	}

	nname := types.NamespacedName{
		Namespace: dbClaim.Namespace,
		Name:      dbClaim.Name,
	}
	logr := log.FromContext(ctx).WithValues("databaseclaim", nname)

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

func (m *manager) SuccessAndUpdateCondition(ctx context.Context, dbClaim *v1.DatabaseClaim, condType string) (reconcile.Result, error) {
	dbClaim.Status.Error = ""

	updateConditionStatus(&dbClaim.Status.Conditions, condType, metav1.ConditionTrue)
	if err := m.client.Status().Update(ctx, dbClaim); err != nil {
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

	return ctrl.Result{RequeueAfter: m.passwordRotationTime}, nil
}

func (m *manager) SetStatusCondition(ctx context.Context, dbClaim *v1.DatabaseClaim, condition metav1.Condition) {
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

func (m *manager) UpdateClusterStatus(status *v1.Status, hostParams *hostparams.HostParams) {
	status.DBVersion = hostParams.DBVersion
	status.Type = v1.DatabaseType(hostParams.Type)
	status.Shape = hostParams.Shape
	status.MinStorageGB = hostParams.MinStorageGB
	if hostParams.Type == string(v1.Postgres) {
		status.MaxStorageGB = hostParams.MaxStorageGB
	}
}

func (m *manager) UpdateDBStatus(status *v1.Status, dbName string) {
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

func (m *manager) UpdateHostPortStatus(status *v1.Status, host string, port string, sslMode string) {
	timeNow := metav1.Now()
	if status.ConnectionInfo == nil {
		status.ConnectionInfo = &v1.DatabaseClaimConnectionInfo{}
	}
	status.ConnectionInfo.Host = host
	status.ConnectionInfo.Port = port
	status.ConnectionInfo.SSLMode = sslMode
	status.ConnectionInfoUpdatedAt = &timeNow
}

func (m *manager) UpdateUserStatus(status *v1.Status, reqInfo *requestInfo, userName string, userPassword string) {
	timeNow := metav1.Now()
	if status.ConnectionInfo == nil {
		status.ConnectionInfo = &v1.DatabaseClaimConnectionInfo{}
	}

	status.UserUpdatedAt = &timeNow
	status.ConnectionInfo.Username = userName
	reqInfo.TempSecret = userPassword
	status.ConnectionInfoUpdatedAt = &timeNow
}

func NewConditionFromDBClaimMode(mode ModeEnum) metav1.Condition {
	switch mode {
	case M_UseExistingDB:
		return metav1.Condition{
			Type:    TypeUseExistingDB,
			Status:  metav1.ConditionFalse,
			Reason:  "UsingExistingDatabase",
			Message: "The database claim is configured to use an existing database.",
		}
	case M_MigrateExistingToNewDB:
		return metav1.Condition{
			Type:    TypeMigrationToNewDB,
			Status:  metav1.ConditionFalse,
			Reason:  "MigratingDatabase",
			Message: "The database claim is migrating from an existing database to a new one.",
		}
	case M_MigrationInProgress:
		return metav1.Condition{
			Type:    TypeMigrationInProgress,
			Status:  metav1.ConditionFalse,
			Reason:  "MigrationOngoing",
			Message: "The database migration is currently in progress.",
		}
	case M_UseNewDB:
		return metav1.Condition{
			Type:    TypeUseNewDB,
			Status:  metav1.ConditionFalse,
			Reason:  "UsingNewDatabase",
			Message: "The database claim is configured to use a new database.",
		}
	case M_InitiateDBUpgrade:
		return metav1.Condition{
			Type:    TypeDBUpgradeInitiated,
			Status:  metav1.ConditionFalse,
			Reason:  "UpgradeStarted",
			Message: "The database upgrade has been initiated.",
		}
	case M_UpgradeDBInProgress:
		return metav1.Condition{
			Type:    TypeDBUpgradeInProgress,
			Status:  metav1.ConditionFalse,
			Reason:  "UpgradeOngoing",
			Message: "The database upgrade is currently in progress.",
		}
	case M_PostMigrationInProgress:
		return metav1.Condition{
			Type:    TypePostMigrationInProgress,
			Status:  metav1.ConditionFalse,
			Reason:  "FinalizingMigration",
			Message: "Finalizing post-migration tasks for the database claim.",
		}
	default:
		return metav1.Condition{
			Type:    "InvalidCondition",
			Status:  metav1.ConditionFalse,
			Reason:  "UnsupportedMode",
			Message: "The operation mode is not supported.",
		}
	}
}

func updateConditionStatus(conditions *[]metav1.Condition, conditionType string, status metav1.ConditionStatus) {
	for i := range *conditions {
		if (*conditions)[i].Type == conditionType {
			(*conditions)[i].Status = status
			(*conditions)[i].LastTransitionTime = metav1.Now()
			return
		}
	}
}