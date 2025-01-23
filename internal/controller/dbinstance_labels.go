package controller

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane-contrib/provider-aws/apis/rds/v1alpha1"
	v1 "github.com/infobloxopen/db-controller/api/v1"
	"github.com/infobloxopen/db-controller/pkg/databaseclaim"
	"github.com/spf13/viper"
)

// SyncDBInstances synchronizes the labels of DBInstances with their associated DatabaseClaims.
func SyncDBInstances(ctx context.Context, viper *viper.Viper, kubeClient client.Client, logger logr.Logger) error {
	logger.Info("Starting synchronization of DBInstance labels")

	// Dynamically retrieve the prefix from the environment configuration
	prefix := viper.GetString("env")
	logger.Info("Using dynamic prefix from environment", "prefix", prefix)

	// List all DBInstances
	var dbInstances v1alpha1.DBInstanceList
	if err := kubeClient.List(ctx, &dbInstances); err != nil {
		logger.Error(err, "Failed to list DBInstances")
		return fmt.Errorf("error listing DBInstances: %w", err)
	}

	logger.Info("DBInstances fetched successfully", "TotalDBInstances", len(dbInstances.Items))

	for _, dbInstance := range dbInstances.Items {
		instanceLogger := logger.WithValues("DBInstanceName", dbInstance.Name)
		instanceLogger.Info("Processing DBInstance")

		// Derive the DatabaseClaim name
		nameWithoutPrefix := strings.TrimPrefix(dbInstance.Name, prefix)
		nameWithoutSuffix := nameWithoutPrefix

		// Check if the prefix was correctly removed; otherwise, log and skip
		if nameWithoutPrefix == dbInstance.Name {
			instanceLogger.Error(fmt.Errorf("prefix not found in DBInstance name"), "Skipping DBInstance due to invalid prefix")
			continue
		}

		if len(nameWithoutSuffix) > 9 {
			nameWithoutSuffix = nameWithoutSuffix[:len(nameWithoutSuffix)-9]
		}
		dbClaimName := strings.TrimSpace(nameWithoutSuffix)

		instanceLogger.Info("Derived DatabaseClaim name", "DerivedName", dbClaimName)

		if dbClaimName == "" {
			instanceLogger.Error(fmt.Errorf("empty DatabaseClaim name"), "Skipping DBInstance due to invalid naming format")
			continue
		}

		// Attempt to fetch the associated DatabaseClaim
		dbClaimRef := client.ObjectKey{Name: dbClaimName, Namespace: "default"} // Fixed namespace "default"
		instanceLogger.Info("Fetching DatabaseClaim", "DatabaseClaimRef", dbClaimRef)

		var dbClaim v1.DatabaseClaim
		if err := kubeClient.Get(ctx, dbClaimRef, &dbClaim); err != nil {
			instanceLogger.Error(err, "Failed to fetch DatabaseClaim", "DatabaseClaimName", dbClaimName)
			continue
		}

		instanceLogger.Info("DatabaseClaim fetched successfully", "DatabaseClaimLabels", dbClaim.Labels)

		// Propagate labels from the DatabaseClaim
		newLabels := databaseclaim.PropagateLabels(dbClaim.Labels)
		instanceLogger.Info("Labels recebidas do DatabaseClaim", "Labels", dbClaim.Labels)
		instanceLogger.Info("Labels propagadas", "PropagatedLabels", newLabels)

		if len(newLabels) == 0 {
			instanceLogger.Info("No labels to propagate from DatabaseClaim", "DatabaseClaimName", dbClaimName)
			continue
		}

		// Fetch the latest version of the DBInstance
		if err := kubeClient.Get(ctx, client.ObjectKey{Name: dbInstance.Name}, &dbInstance); err != nil {
			instanceLogger.Error(err, "Failed to fetch the latest version of DBInstance")
			continue
		}

		// Update the DBInstance labels
		instanceLogger.Info("Updating DBInstance labels", "CurrentLabels", dbInstance.Labels, "NewLabels", newLabels)

		if err := UpdateDBInstanceLabels(ctx, kubeClient, &dbInstance, newLabels, instanceLogger); err != nil {
			instanceLogger.Error(err, "Failed to update labels for DBInstance")
			continue
		}

		instanceLogger.Info("Labels updated successfully for DBInstance", "UpdatedLabels", dbInstance.Labels)
	}

	logger.Info("Synchronization of DBInstance labels completed successfully")
	return nil
}

// UpdateDBInstanceLabels updates the labels of a DBInstance while preserving existing labels.
func UpdateDBInstanceLabels(ctx context.Context, kubeClient client.Client, dbInstance *v1alpha1.DBInstance, newLabels map[string]string, logger logr.Logger) error {
	logger.Info("Starting update of DBInstance labels", "DBInstanceName", dbInstance.Name)

	// Return early if there are no new labels to apply
	if len(newLabels) == 0 {
		logger.Info("No new labels provided. Skipping update.", "DBInstanceName", dbInstance.Name)
		return nil
	}

	// Initialize the labels map if it is nil
	if dbInstance.Labels == nil {
		dbInstance.Labels = make(map[string]string)
		logger.Info("Initialized empty labels map for DBInstance", "DBInstanceName", dbInstance.Name)
	}

	// Add or update new labels
	updated := false
	logger.Info("Processing new labels", "NewLabels", newLabels, "CurrentLabels", dbInstance.Labels)

	for key, value := range newLabels {
		if oldValue, exists := dbInstance.Labels[key]; exists && oldValue == value {
			logger.V(1).Info("Label already exists and is unchanged", "Key", key, "Value", value)
			continue
		}
		dbInstance.Labels[key] = value
		updated = true
		logger.Info("Label added or updated", "Key", key, "Value", value)
	}

	if !updated {
		logger.Info("No label updates required for DBInstance", "DBInstanceName", dbInstance.Name)
		return nil
	}

	// Attempt to update the DBInstance in the Kubernetes API
	logger.Info("Applying updated labels to DBInstance in Kubernetes API", "DBInstanceName", dbInstance.Name, "UpdatedLabels", dbInstance.Labels)
	if err := kubeClient.Update(ctx, dbInstance); err != nil {
		logger.Error(err, "Failed to update DBInstance labels in Kubernetes API", "DBInstanceName", dbInstance.Name)
		return fmt.Errorf("error updating DBInstance labels: %w", err)
	}

	logger.Info("Labels successfully updated in Kubernetes API", "DBInstanceName", dbInstance.Name, "FinalLabels", dbInstance.Labels)
	return nil
}
