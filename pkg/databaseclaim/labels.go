package databaseclaim

func propagateLabels(dbClaimLabels map[string]string) map[string]string {
	keysToPropagate := []string{
		"app.kubernetes.io/component",
		"app.kubernetes.io/instance",
		"app.kubernetes.io/name",
	}

	labels := make(map[string]string)

	for _, key := range keysToPropagate {
		if value, exists := dbClaimLabels[key]; exists {
			labels[key] = value
		}
	}

	return labels
}
