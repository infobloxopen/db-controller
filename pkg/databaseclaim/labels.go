package databaseclaim

import "fmt"

// PropagateLabels filters and propagates specific labels from DatabaseClaim.
func PropagateLabels(dbClaimLabels map[string]string) map[string]string {
	// Log the received labels
	fmt.Printf("Labels recebidas: %+v\n", dbClaimLabels)

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

	// Log the propagated labels
	fmt.Printf("Labels propagadas: %+v\n", labels)

	return labels
}
