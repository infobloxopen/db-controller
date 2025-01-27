package databaseclaim

// PropagateLabels extracts and propagates the labels 'app.kubernetes.io/dbclaim-name'
// and 'app.kubernetes.io/dbclaim-namespace' from a DatabaseClaim.
func PropagateLabels(dbClaimLabels map[string]string) map[string]string {
	keysToPropagate := []string{
		"app.kubernetes.io/dbclaim-name",
		"app.kubernetes.io/dbclaim-namespace",
	}

	labels := make(map[string]string)

	for _, key := range keysToPropagate {
		if value, exists := dbClaimLabels[key]; exists {
			labels[key] = value
		}
	}

	return labels
}
