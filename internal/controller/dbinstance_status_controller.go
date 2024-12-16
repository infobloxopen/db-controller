package controller

import (
	"context"
	"fmt"
	"time"

	crossplaneaws "github.com/crossplane-contrib/provider-aws/apis/rds/v1alpha1"
	"github.com/go-logr/logr"
	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	ConditionReady            = "Ready"
	ConditionSynced           = "Synced"
	ConditionReadyAtProvider  = "ReadyAtProvider"
	ConditionSyncedAtProvider = "SyncedAtProvider"
)

// DBInstanceStatusReconciler reconciles the status of DBInstance resources with DatabaseClaims
type DBInstanceStatusReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// RBAC markers
// +kubebuilder:rbac:groups=database.aws.crossplane.io,resources=dbinstances,verbs=get;list;watch
// +kubebuilder:rbac:groups=persistance.atlas.infoblox.com,resources=databaseclaims,verbs=get;list;watch
// +kubebuilder:rbac:groups=persistance.atlas.infoblox.com,resources=databaseclaims/status,verbs=get;update;patch

// Reconcile reconciles DBInstance with its corresponding DatabaseClaim
func (r *DBInstanceStatusReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Starting reconciliation", "DBInstance", req.NamespacedName)

	// Fetch the DBInstance.
	dbInstance, err := r.fetchDBInstance(ctx, req)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Retrieve the DBInstance labels for the associated DatabaseClaim.
	dbClaimInstance, dbClaimComponent, err := r.validateDBInstanceLabels(dbInstance, logger)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Retrieve the associated DatabaseClaim.
	dbClaim, err := r.fetchDatabaseClaim(ctx, dbClaimInstance, dbClaimComponent, logger)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Update DatabaseClaim based on DBInstance status.
	if err := r.updateDatabaseClaimStatus(ctx, dbInstance, dbClaim, logger); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("Reconciliation complete", "DBInstance", req.NamespacedName)
	return ctrl.Result{}, nil
}

// fetchDBInstance retrieves the DBInstance resource.
func (r *DBInstanceStatusReconciler) fetchDBInstance(ctx context.Context, req ctrl.Request) (*crossplaneaws.DBInstance, error) {
	var dbInstance crossplaneaws.DBInstance
	if err := r.Get(ctx, req.NamespacedName, &dbInstance); err != nil {
		if client.IgnoreNotFound(err) != nil {
			log.FromContext(ctx).Error(err, "Failed to get DBInstance")
		}
		return nil, client.IgnoreNotFound(err)
	}
	return &dbInstance, nil
}

// validateDBInstanceLabels checks if the DBInstance has the required labels
func (r *DBInstanceStatusReconciler) validateDBInstanceLabels(dbInstance *crossplaneaws.DBInstance, logger logr.Logger) (string, string, error) {
	labels := dbInstance.GetLabels()
	if labels == nil {
		logger.Error(fmt.Errorf("missing labels"), "DBInstance has no labels", "DBInstance", dbInstance.Name)
		return "", "", fmt.Errorf("DBInstance %s has no labels", dbInstance.Name)
	}

	instanceLabel, exists := labels["app.kubernetes.io/instance"]
	if !exists {
		logger.Info("DBInstance missing app.kubernetes.io/instance required label", "DBInstance", dbInstance.Name)
		return "", "", fmt.Errorf("missing app.kubernetes.io/instance required label")
	}

	componentLabel, exists := labels["app.kubernetes.io/component"]
	if !exists {
		logger.Info("DBInstance missing app.kubernetes.io/component required label", "DBInstance", dbInstance.Name)
		return "", "", fmt.Errorf("missing app.kubernetes.io/component required label")
	}

	return instanceLabel, componentLabel, nil
}

// fetchDatabaseClaim retrieves the DatabaseClaim resource.
func (r *DBInstanceStatusReconciler) fetchDatabaseClaim(ctx context.Context, dbClaimInstance, dbClaimComponent string, logger logr.Logger) (*persistancev1.DatabaseClaim, error) {
	var dbClaim persistancev1.DatabaseClaim
	if err := r.Get(ctx, types.NamespacedName{
		Name:      fmt.Sprintf("%s-%s", dbClaimInstance, dbClaimComponent),
		Namespace: dbClaimInstance,
	}, &dbClaim); err != nil {
		logger.Error(err, "Failed to get DatabaseClaim", "DatabaseClaim", fmt.Sprintf("%s/%s-%s", dbClaimInstance, dbClaimInstance, dbClaimComponent))
		return nil, err
	}

	return &dbClaim, nil
}

// updateDatabaseClaimStatus updates the status of the DatabaseClaim
func (r *DBInstanceStatusReconciler) updateDatabaseClaimStatus(ctx context.Context, dbInstance *crossplaneaws.DBInstance, dbClaim *persistancev1.DatabaseClaim, logger logr.Logger) error {
	updated := false

	if dbClaim.Status.Conditions == nil {
		dbClaim.Status.Conditions = []metav1.Condition{}
	}

	// Check "Ready" and "Synced" conditions
	for _, condition := range dbInstance.Status.Conditions {
		logger.Info("Checking condition", "Type", condition.Type, "Status", condition.Status)

		if condition.Type == ConditionReady && condition.Status == corev1.ConditionTrue {
			logger.Info("Ready condition found", "DBInstance", dbInstance.Name)

			newCondition := metav1.Condition{
				Type:    ConditionReadyAtProvider,
				Status:  metav1.ConditionStatus(condition.Status),
				Reason:  string(condition.Reason),
				Message: condition.Message,
			}

			if len(dbClaim.Status.Conditions) == 0 || dbClaim.Status.Conditions[0].Type != string(newCondition.Type) {
				dbClaim.Status.Conditions = []metav1.Condition{newCondition}
				updated = true
			}
			break
		}

		if condition.Type == ConditionSynced && condition.Status == corev1.ConditionTrue {
			logger.Info("Synced condition found", "DBInstance", dbInstance.Name)

			newCondition := metav1.Condition{
				Type:               ConditionSyncedAtProvider,
				Status:             metav1.ConditionStatus(condition.Status),
				LastTransitionTime: condition.LastTransitionTime,
				Reason:             string(condition.Reason),
				Message:            condition.Message,
			}

			if len(dbClaim.Status.Conditions) == 0 || dbClaim.Status.Conditions[0].Type != string(newCondition.Type) {
				dbClaim.Status.Conditions = []metav1.Condition{newCondition}
				updated = true
			}
			break
		}
	}

	if updated {
		if err := r.retryDatabaseClaimUpdate(ctx, dbClaim, logger); err != nil {
			return err
		}
		logger.Info("DatabaseClaim status updated with new conditions", "DBInstance", dbInstance.Name)
	}

	return nil
}

// retryDatabaseClaimUpdate retries updating the DatabaseClaim status with exponential backoff
func (r *DBInstanceStatusReconciler) retryDatabaseClaimUpdate(ctx context.Context, dbClaim *persistancev1.DatabaseClaim, logger logr.Logger) error {
	return wait.ExponentialBackoff(wait.Backoff{
		Steps:    5,
		Duration: 100 * time.Millisecond,
		Factor:   2.0,
		Jitter:   0.1,
	}, func() (bool, error) {
		if err := r.Status().Update(ctx, dbClaim); err != nil {
			logger.Error(err, "Failed to update DatabaseClaim status, retrying")
			return false, nil
		}
		return true, nil
	})
}

// SetupWithManager configures the controller with the Manager
func (r *DBInstanceStatusReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&crossplaneaws.DBInstance{}).
		Complete(r)
}
