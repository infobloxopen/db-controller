package databaseclaim

// PropagateLabels filters and propagates specific labels from DatabaseClaim.
func PropagateLabels(dbClaimLabels map[string]string) map[string]string {
	keysToPropagate := []string{
		"app.kubernetes.io/component",
		"app.kubernetes.io/instance",
		"app.kubernetes.io/name",
	}

	// Initialize the map to store propagated labels
	labels := make(map[string]string)

	for _, key := range keysToPropagate {
		if value, exists := dbClaimLabels[key]; exists {
			labels[key] = value
		}
	}

	return labels
}
