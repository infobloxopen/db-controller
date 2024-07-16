package databaseclaim

import (
	"context"

	v1 "github.com/infobloxopen/db-controller/api/v1"
)

// CanTagResources checks if there's claims matching the instance
// label of the dbClaim. If there's only one claim, it can tag
func CanTagResources(ctx context.Context, dbClaimList v1.DatabaseClaimList, dbClaim v1.DatabaseClaim) (bool, error) {

	if dbClaim.Spec.InstanceLabel == "" {
		return true, nil
	}

	if len(dbClaimList.Items) == 1 {
		return true, nil
	}

	return false, nil
}
