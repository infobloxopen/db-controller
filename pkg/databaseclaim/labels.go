package databaseclaim

import v1 "github.com/infobloxopen/db-controller/api/v1"

// PropagateLabels creates and updates the labels 'app.kubernetes.io/dbclaim-name'
// and 'app.kubernetes.io/dbclaim-namespace' for a DBInstance from a DatabaseClaim.
func PropagateLabels(dbClaim *v1.DatabaseClaim) map[string]string {
	// Specific labels from the DatabaseClaim that we want to preserve.
	dbClaimSpecificLabels := []string{
		"app.kubernetes.io/component",
		"app.kubernetes.io/instance",
		"app.kubernetes.io/name",
	}

	// DBInstance-specific labels that we want to add/update.
	dbInstanceSpecificLabels := map[string]string{
		"app.kubernetes.io/dbclaim-name":      dbClaim.Name,
		"app.kubernetes.io/dbclaim-namespace": dbClaim.Namespace,
	}

	// Combine both types of labels.
	labels := make(map[string]string)

	// Add specific labels from the DatabaseClaim.
	for _, key := range dbClaimSpecificLabels {
		if value, exists := dbClaim.Labels[key]; exists {
			labels[key] = value
		}
	}

	// Add/update DBInstance-specific labels.
	for key, value := range dbInstanceSpecificLabels {
		labels[key] = value
	}

	return labels
}
