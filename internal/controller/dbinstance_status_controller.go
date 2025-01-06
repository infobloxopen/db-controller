package controller

import (
	"context"
	"errors"
	"fmt"

	crossplaneaws "github.com/crossplane-contrib/provider-aws/apis/rds/v1alpha1"
	"github.com/go-logr/logr"
	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
	v1 "github.com/infobloxopen/db-controller/api/v1"
	statusmanager "github.com/infobloxopen/db-controller/pkg/databaseclaim"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	ConditionReady  = "Ready"
	ConditionSynced = "Synced"
)

// DBInstanceStatusReconciler reconciles the status of DBInstance resources with DatabaseClaims
type DBInstanceStatusReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	StatusManager *statusmanager.StatusManager
}

// RBAC markers
// +kubebuilder:rbac:groups=database.aws.crossplane.io,resources=dbinstances,verbs=get;list;watch
// +kubebuilder:rbac:groups=persistance.atlas.infoblox.com,resources=databaseclaims,verbs=get;list;watch
// +kubebuilder:rbac:groups=persistance.atlas.infoblox.com,resources=databaseclaims/status,verbs=get;update;patch

// Reconcile reconciles DBInstance with its corresponding DatabaseClaim.
func (r *DBInstanceStatusReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Starting DBInstance Status reconciliation", "DBInstance", req.NamespacedName)

	dbInstance, err := r.getDBInstance(ctx, req)
	if err != nil {
		return ctrl.Result{}, err
	}

	dbClaimRef, err := r.getDBClaimRefFromDBInstance(dbInstance, logger)
	if err != nil {
		return ctrl.Result{}, err
	}

	dbClaim, err := r.getDatabaseClaim(ctx, dbClaimRef)
	if err != nil {
		return ctrl.Result{}, err
	}

	if err := r.updateDatabaseClaimStatus(ctx, dbInstance, dbClaim, logger); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("Reconciliation complete", "DBInstance", req.NamespacedName)
	return ctrl.Result{}, nil
}

// getDBInstance retrieves the DBInstance resource.
func (r *DBInstanceStatusReconciler) getDBInstance(ctx context.Context, req ctrl.Request) (*crossplaneaws.DBInstance, error) {
	var dbInstance crossplaneaws.DBInstance
	if err := r.Get(ctx, req.NamespacedName, &dbInstance); err != nil {
		return nil, fmt.Errorf("failed to get DBInstance: %w", err)
	}
	return &dbInstance, nil
}

// getDBClaimRefLabelsFromDBInstance extracts the DBClaim labels from the DBInstance.
func (r *DBInstanceStatusReconciler) getDBClaimRefFromDBInstance(dbInstance *crossplaneaws.DBInstance, logger logr.Logger) (*types.NamespacedName, error) {
	labels := dbInstance.GetLabels()
	if labels == nil {
		logger.Error(errors.New("missing labels"), "DBInstance has no labels", "DBInstance", dbInstance.Name)
		return nil, fmt.Errorf("DBInstance %s has no labels", dbInstance.Name)
	}

	instanceLabel, exists := labels["app.kubernetes.io/instance"]
	if !exists {
		err := errors.New("DBInstance is missing app.kubernetes.io/instance required label")
		logger.Error(err, err.Error(), "DBInstance", dbInstance.Name)
		return nil, err
	}

	componentLabel, exists := labels["app.kubernetes.io/component"]
	if !exists {
		err := errors.New("DBInstance is missing app.kubernetes.io/component required label")
		logger.Error(err, err.Error(), "DBInstance", dbInstance.Name)
		return nil, err
	}

	dbClaimNSName := types.NamespacedName{
		Name:      fmt.Sprintf("%s-%s", instanceLabel, componentLabel),
		Namespace: instanceLabel,
	}

	return &dbClaimNSName, nil
}

// fetchDatabaseClaim retrieves the DatabaseClaim resource.
func (r *DBInstanceStatusReconciler) getDatabaseClaim(ctx context.Context, dbClaimRef *types.NamespacedName) (*persistancev1.DatabaseClaim, error) {
	var dbClaim persistancev1.DatabaseClaim
	if err := r.Get(ctx, *dbClaimRef, &dbClaim); err != nil {
		return nil, fmt.Errorf("failed to get DatabaseClaim: %w", err)
	}

	return &dbClaim, nil
}

// updateDatabaseClaimStatus updates the status of the DatabaseClaim based on the DBInstance status.
func (r *DBInstanceStatusReconciler) updateDatabaseClaimStatus(ctx context.Context, dbInstance *crossplaneaws.DBInstance, dbClaim *persistancev1.DatabaseClaim, logger logr.Logger) error {
	if dbInstance.Status.Conditions == nil || len(dbInstance.Status.Conditions) == 0 {
		logger.Info("DBInstance has no conditions", "DBInstance", dbInstance.Name)
		return nil
	}

	var conditionSyncedAtProvider, conditionReadyAtProvider metav1.Condition

	// Retrieve the conditions from the DBInstance status.
	for _, condition := range dbInstance.Status.Conditions {
		switch condition.Type {
		case ConditionSynced:
			conditionSyncedAtProvider = persistancev1.CreateCondition(
				persistancev1.ConditionSync,
				metav1.ConditionStatus(condition.Status),
				string(condition.Reason),
				condition.Message,
			)
		case ConditionReady:
			conditionReadyAtProvider = persistancev1.CreateCondition(
				persistancev1.ConditionReady,
				metav1.ConditionStatus(condition.Status),
				string(condition.Reason),
				condition.Message,
			)
		}
	}

	if conditionReadyAtProvider.Status == metav1.ConditionTrue && conditionSyncedAtProvider.Status == metav1.ConditionTrue {
		if err := r.StatusManager.SetConditionAndUpdateStatus(ctx, dbClaim, persistancev1.DatabaseReadyCondition()); err != nil {
			logger.Error(err, "failed to set success condition in DatabaseClaim", "DatabaseClaim", dbClaim.Name)
			return err
		}
		return nil
	}

	errorCondition := v1.ReconcileSyncErrorCondition(fmt.Errorf("%s: %s", conditionSyncedAtProvider.Reason, conditionSyncedAtProvider.Message))
	if err := r.StatusManager.SetConditionAndUpdateStatus(ctx, dbClaim, errorCondition); err != nil {
		logger.Error(err, "failed to set error condition in DatabaseClaim", "DatabaseClaim", dbClaim.Name)
		return err
	}
	return nil
}

// SetupWithManager configures the controller with the Manager.
func (r *DBInstanceStatusReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&crossplaneaws.DBInstance{}).
		Complete(r)
}
