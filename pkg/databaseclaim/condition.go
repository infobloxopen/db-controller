package databaseclaim

import (
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ConditionType string

const (
	ReasonAvailable   = "Available"
	ReasonUnavailable = "Unavailable"
	ReasonCreating    = "Creating"
	ReasonDeleting    = "Deleting"
)

const (
	ConditionReady           ConditionType = "Ready"
	ConditionProvisioning    ConditionType = "Provisioning"
	ConditionDeleting        ConditionType = "Deleting"
	ConditionMigrating       ConditionType = "Migrating"
	PasswordRotation         ConditionType = "PasswordRotation"
	ConditionConnectionIssue ConditionType = "ConnectionFailed"
)

func CreateCondition(condType ConditionType, status metav1.ConditionStatus, reason string, message string) metav1.Condition {
	now := metav1.NewTime(time.Now())
	return metav1.Condition{
		Type:               string(condType),
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: now,
	}
}

func ProvisioningCondition() metav1.Condition {
	return CreateCondition(
		ConditionProvisioning,
		metav1.ConditionFalse,
		ReasonCreating,
		"Database provisioning is in progress.",
	)
}

func DeletingCondition() metav1.Condition {
	return CreateCondition(
		ConditionDeleting,
		metav1.ConditionFalse,
		ReasonDeleting,
		"Database deletion process has been initiated.",
	)
}

func MigratingCondition() metav1.Condition {
	return CreateCondition(
		ConditionMigrating,
		metav1.ConditionFalse,
		ReasonCreating,
		"Database migration is underway. Data is being transferred from the existing database to the new target database.",
	)
}

func PwdRotationCondition() metav1.Condition {
	return CreateCondition(
		PasswordRotation,
		metav1.ConditionTrue,
		ReasonAvailable,
		"Database credentials are being securely rotated.",
	)
}

func ReconcileErrorCondition(err error) metav1.Condition {
	return CreateCondition(
		ConditionReady,
		metav1.ConditionFalse,
		ReasonUnavailable,
		fmt.Sprintf("Reconciliation encountered an issue: %v. The database state may not match the desired configuration.", err),
	)
}

func ReconcileSuccessCondition() metav1.Condition {
	return CreateCondition(
		ConditionReady,
		metav1.ConditionTrue,
		ReasonAvailable,
		"Database successfully synchronized. The current state fully matches the specified configuration and is ready for use.",
	)
}
