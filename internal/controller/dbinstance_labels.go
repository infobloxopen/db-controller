package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/crossplane-contrib/provider-aws/apis/rds/v1alpha1"
	v1 "github.com/infobloxopen/db-controller/api/v1"
	"github.com/infobloxopen/db-controller/pkg/databaseclaim"
	"github.com/spf13/viper"
)

// SyncDBInstances propagates labels from DatabaseClaims to their associated DBInstances
// in a Kubernetes cluster, ensuring consistency between the resources.
func SyncDBInstances(ctx context.Context, viper *viper.Viper, kubeClient client.Client, logger logr.Logger) error {
	logger.Info("starting synchronization of DBInstance labels")

	// Dynamically retrieve the prefix from the environment configuration.
	prefix := viper.GetString("env")
	logger.Info("using dynamic prefix from environment", "prefix", prefix)

	// List all DBInstances.
	var dbInstances v1alpha1.DBInstanceList
	if err := kubeClient.List(ctx, &dbInstances); err != nil {
		return fmt.Errorf("error listing DBInstances: %w", err)
	}

	logger.Info("DBInstances fetched successfully", "total_dbinstances", len(dbInstances.Items))

	for _, dbInstance := range dbInstances.Items {
		instanceLogger := logger.WithValues("DBInstanceName", dbInstance.Name)
		instanceLogger.Info("processing DBInstance")

		// Extract the DatabaseClaim name from the DBInstance name by removing the prefix and suffix.
		nameWithoutPrefix := strings.TrimPrefix(dbInstance.Name, prefix+"-")
		nameWithoutSuffix := nameWithoutPrefix

		if len(nameWithoutSuffix) > 9 {
			nameWithoutSuffix = nameWithoutSuffix[:len(nameWithoutSuffix)-9]
		}
		dbClaimName := strings.TrimSpace(nameWithoutSuffix)

		instanceLogger.Info("derived databaseclaim name", "dbclaim_ref", dbClaimName)

		if dbClaimName == "" {
			instanceLogger.Error(errors.New("empty DatabaseClaim name"), "skipping DBInstance due to invalid naming format")
			continue
		}

		// Fetch the associated DatabaseClaim by name.
		dbClaimsTest := v1.DatabaseClaimList{}
		fieldSelectorOptions := []client.ListOption{
			client.MatchingFields{
				"metadata.name": dbClaimName,
			},
		}
		if err := kubeClient.List(ctx, &dbClaimsTest, fieldSelectorOptions...); err != nil {
			instanceLogger.Error(err, "failed to fetch DatabaseClaim", "DatabaseClaimName", dbClaimName)
			continue
		}
		if len(dbClaimsTest.Items) == 0 {
			instanceLogger.Info("no DatabaseClaim found for DBInstance", "DatabaseClaimName", dbClaimName)
			continue
		}
		dbClaim := dbClaimsTest.Items[0]
		instanceLogger.Info("DatabaseClaim fetched successfully", "DatabaseClaimLabels", dbClaim.Labels)

		// Propagate labels from the DatabaseClaim.
		newLabels := databaseclaim.PropagateLabels(dbClaim.Labels)
		if len(newLabels) == 0 {
			instanceLogger.Info("no labels to propagate from DatabaseClaim", "DatabaseClaimName", dbClaimName)
			continue
		}

		// Update the DBInstance labels.
		instanceLogger.Info("updating DBInstance labels", "currentlabels", dbInstance.Labels, "newlabels", newLabels)

		if err := updateDBInstanceLabels(ctx, kubeClient, &dbInstance, newLabels, instanceLogger); err != nil {
			instanceLogger.Error(err, "failed to update labels for DBInstance")
			continue
		}

		instanceLogger.Info("labels updated successfully for DBInstance", "updatedlabels", dbInstance.Labels)
	}

	logger.Info("synchronization of DBInstance labels completed successfully")
	return nil
}

// updateDBInstanceLabels updates the labels of a DBInstance while preserving any existing labels.
// It ensures that only new or changed labels are updated in the DBInstance.
func updateDBInstanceLabels(ctx context.Context, kubeClient client.Client, dbInstance *v1alpha1.DBInstance, newLabels map[string]string, logger logr.Logger) error {
	logger.Info("starting update of DBInstance labels")

	if dbInstance.Labels == nil {
		dbInstance.Labels = make(map[string]string)
	}

	updated := false

	for key, value := range newLabels {
		if oldValue, exists := dbInstance.Labels[key]; exists && oldValue == value {
			logger.Info("label already exists and is unchanged", "key", key, "value", value)
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

	// Attempt to apply the updated labels to the DBInstance.
	logger.Info("applying updated labels to DBInstance", "updatedLabels", dbInstance.Labels)
	if err := kubeClient.Update(ctx, dbInstance); err != nil {
		return fmt.Errorf("error updating DBInstance labels: %w", err)
	}

	return nil
}
