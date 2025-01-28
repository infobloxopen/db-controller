package controller

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	v1 "github.com/infobloxopen/db-controller/api/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane-contrib/provider-aws/apis/rds/v1alpha1"
	crossplaneaws "github.com/crossplane-contrib/provider-aws/apis/rds/v1alpha1"
	"github.com/spf13/viper"
)

// SyncDBInstances ensures that each DBInstance has the appropriate dbclaim-name and dbclaim-namespace labels.
func SyncDBInstances(ctx context.Context, viper *viper.Viper, kubeClient client.Client, logger logr.Logger) error {
	logger.Info("starting synchronization of DBInstance labels")

	// List all DBInstances.
	var dbInstances crossplaneaws.DBInstanceList
	if err := kubeClient.List(ctx, &dbInstances); err != nil {
		return fmt.Errorf("error listing DBInstances: %w", err)
	}

	// Process each DBInstance.
	for _, dbInstance := range dbInstances.Items {
		instanceLogger := logger.WithValues("DBInstanceName", dbInstance.Name)
		instanceLogger.Info("processing DBInstance")

		// Extract dbclaim-name and dbclaim-namespace from existing DBInstance labels.
		dbClaimName := dbInstance.Labels["app.kubernetes.io/dbclaim-name"]
		dbClaimNamespace := dbInstance.Labels["app.kubernetes.io/dbclaim-namespace"]

		// Update labels if any are missing.
		if dbClaimName == "" || dbClaimNamespace == "" {
			instanceLogger.Info("dbclaim-name or dbclaim-namespace labels are missing; proceeding to update")

			newLabels := map[string]string{
				"app.kubernetes.io/dbclaim-name":      dbInstance.Name,
				"app.kubernetes.io/dbclaim-namespace": "default",
			}
			if err := updateDBInstanceLabels(ctx, kubeClient, &dbInstance, newLabels, instanceLogger); err != nil {
				instanceLogger.Error(err, "failed to update labels for DBInstance")
				continue
			}
		} else {
			instanceLogger.Info("DBInstance has valid dbclaim-name and dbclaim-namespace labels; no update necessary")
		}

		// Check if the DatabaseClaim exists.
		var dbClaim v1.DatabaseClaim
		if err := kubeClient.Get(ctx, client.ObjectKey{Name: dbClaimName, Namespace: dbClaimNamespace}, &dbClaim); err != nil {
			instanceLogger.Error(err, "DatabaseClaim not found for DBInstance")
			return fmt.Errorf("error updating DBInstance %s: %w", dbInstance.Name, err)
		}

		instanceLogger.Info("labels updated successfully")
	}

	logger.Info("synchronization of DBInstance labels completed successfully")
	return nil
}

// updateDBInstanceLabels updates the labels of a DBInstance while preserving existing ones.
func updateDBInstanceLabels(ctx context.Context, kubeClient client.Client, dbInstance *v1alpha1.DBInstance, newLabels map[string]string, logger logr.Logger) error {
	logger.Info("starting update of DBInstance labels")

	if dbInstance.Labels == nil {
		logger.Info("DBInstance has no labels; initializing")
		dbInstance.Labels = make(map[string]string)
	}

	updated := false

	// Update or add new labels.
	for key, value := range newLabels {
		if oldValue, exists := dbInstance.Labels[key]; exists && oldValue == value {
			continue
		}
		dbInstance.Labels[key] = value
		updated = true
		logger.Info("label added or updated", "key", key, "value", value)
	}

	if !updated {
		logger.Info("no label updates required for DBInstance")
		return nil
	}

	// Apply the updated labels to the DBInstance.
	logger.Info("applying updated labels to DBInstance", "updatedLabels", dbInstance.Labels)
	if err := kubeClient.Update(ctx, dbInstance); err != nil {
		return fmt.Errorf("error updating DBInstance labels: %w", err)
	}

	return nil
}
