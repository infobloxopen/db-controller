package controller

import (
	"context"
	"fmt"

	crossplaneaws "github.com/crossplane-contrib/provider-aws/apis/rds/v1alpha1"
	"github.com/go-logr/logr"
	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
	statusmanager "github.com/infobloxopen/db-controller/pkg/databaseclaim"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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
	Scheme        *runtime.Scheme
	StatusManager *statusmanager.StatusManager
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
	conditionSyncedAtProvider := persistancev1.CreateCondition(
		persistancev1.ConditionSync,
		metav1.ConditionUnknown,
		"Unknown",
		"Condition not set",
	)

	conditionReadyAtProvider := persistancev1.CreateCondition(
		persistancev1.ConditionReady,
		metav1.ConditionUnknown,
		"Unknown",
		"Condition not set",
	)

	// Process conditions from DBInstance
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

	// TODO: implement the code to include/update the AtProvider statuses in the DBClaim using the StatusManager.
	// Update in the DatabaseClaim only the Synced condition.

	// If Ready and Synced from the DBInstance are true, then set the Synced condition in the DatabaseClaim will be true,
	// otherwise it will be false.

	// If Synced is false in the DatabaseClaim, then get the reason and message from the DBInstance and set it in
	// the DatabaseClaim.
	// Example of a message: "ReconcileError: cannot determine creation result - remove the crossplane.io/external-create-pending annotation if it is safe to proceed";
	// The reason can be "ReasonUnavailable".
	// Utilize the functions from condition.go to set the status conditions

	if conditionReadyAtProvider.Status == metav1.ConditionTrue && conditionSyncedAtProvider.Status == metav1.ConditionTrue {
		// Set Synced condition to true
		if err := r.StatusManager.SetConditionAndUpdateStatus(ctx, dbClaim, persistancev1.DatabaseReadyCondition()); err != nil {
			logger.Error(err, "Failed to set success condition in DatabaseClaim")
			return err
		}
		logger.Info("DatabaseClaim Synced condition set to true")
	} else {
		// Set Synced condition to false
		reason := conditionSyncedAtProvider.Reason
		if reason == "" {
			reason = persistancev1.ReasonUnavailable
		}

		message := conditionSyncedAtProvider.Message
		if message == "" {
			message = "ReconcileError: cannot determine creation result - remove the crossplane.io/external-create-pending annotation if it is safe to proceed"
		}

		if err := r.StatusManager.SetConditionAndUpdateStatus(ctx, dbClaim, persistancev1.CreateCondition(
			persistancev1.ConditionSync,
			metav1.ConditionFalse,
			reason,
			message,
		)); err != nil {
			logger.Error(err, "Failed to set error condition in DatabaseClaim")
			return err
		}
		logger.Info("DatabaseClaim Synced condition set to false with message", "Message", message)
	}

	return nil

}

// SetupWithManager configures the controller with the Manager
func (r *DBInstanceStatusReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&crossplaneaws.DBInstance{}).
		Complete(r)
}
