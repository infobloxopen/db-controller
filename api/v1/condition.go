package v1

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ConditionType string

const (
	// ConditionReady indicates the database is not just synced but available for user.
	ConditionReady ConditionType = "Ready"

	// ConditionSync indicates whether the db-controller is actively performing operations such as provisioning, deleting, or migrating.
	// It is set to True when the state is fully synchronized with Crossplane/AWS/GCP
	ConditionSync ConditionType = "Synced"

	// ReasonAvailable indicates that the database is fully synchronized and ready for use.
	ReasonAvailable = "Available"

	// ReasonUnavailable indicates that the database is not available due to an issue during reconciliation.
	ReasonUnavailable = "Unavailable"

	// ReasonProvisioning indicates that the database is in the process of being created or provisioned.
	ReasonProvisioning = "Provisioning"

	// ReasonMigrating indicates that the database is undergoing migration to a new target state.
	ReasonMigrating = "Migrating"

	// ReasonDeleting indicates that the database is in the process of being deleted.
	ReasonDeleting = "Deleting"

	// ReasonConnectionIssue indicates that there is a connectivity issue with the database, such as being unable to establish a connection using the provisioned secrets.
	ReasonConnectionIssue = "ConnectionIssue"

	// ReasonNeedsMigrate indicates that the database requires migration due to versioning mismatches or other critical changes.
	ReasonNeedsMigrate = "NeedsMigrate"
)

func CreateCondition(condType ConditionType, status metav1.ConditionStatus, reason, message string) metav1.Condition {
	return metav1.Condition{
		Type:    string(condType),
		Status:  status,
		Reason:  reason,
		Message: message,
	}
}

func ProvisioningCondition() metav1.Condition {
	return CreateCondition(
		ConditionSync,
		metav1.ConditionFalse,
		ReasonProvisioning,
		"Database provisioning is in progress",
	)
}

func DeletingCondition() metav1.Condition {
	return CreateCondition(
		ConditionSync,
		metav1.ConditionFalse,
		ReasonDeleting,
		"Database deletion is in progress",
	)
}

func MigratingCondition() metav1.Condition {
	return CreateCondition(
		ConditionSync,
		metav1.ConditionFalse,
		ReasonMigrating,
		"Database migration is underway",
	)
}

func DatabaseReadyCondition() metav1.Condition {
	return CreateCondition(
		ConditionSync,
		metav1.ConditionTrue,
		ReasonAvailable,
		"Database is provisioned",
	)
}

func ConnectionIssueCondition(err error) metav1.Condition {
	return CreateCondition(
		ConditionReady,
		metav1.ConditionFalse,
		ReasonConnectionIssue,
		fmt.Sprintf("Database connection error: %v", err),
	)
}

func ReconcileErrorCondition(err error) metav1.Condition {
	return CreateCondition(
		ConditionReady,
		metav1.ConditionFalse,
		ReasonUnavailable,
		fmt.Sprintf("Reconciliation encountered an issue: %v", err),
	)
}

func ReconcileSyncErrorCondition(err error) metav1.Condition {
	return CreateCondition(
		ConditionSync,
		metav1.ConditionFalse,
		ReasonUnavailable,
		fmt.Sprintf("Reconciliation encountered an issue: %v", err),
	)
}

func ReconcileSuccessCondition() metav1.Condition {
	return CreateCondition(
		ConditionReady,
		metav1.ConditionTrue,
		ReasonAvailable,
		"Database successfully synchronized and ready for use",
	)
}

// NoDbVersionStatus reflect whether a DatabaseClaim is using an implied older version and should specify its version in .spec.dbVersion
func NoDbVersionStatus() metav1.Condition {
	return CreateCondition(
		ReasonNeedsMigrate,
		metav1.ConditionTrue,
		ReasonAvailable,
		"No database version specified",
	)
}
