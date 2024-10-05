package databaseclaim

import (
	"context"
	"fmt"

	v1 "github.com/infobloxopen/db-controller/api/v1"
	"github.com/infobloxopen/db-controller/pkg/hostparams"
	"github.com/infobloxopen/db-controller/pkg/pgctl"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type ModeEnum int

const (
	M_NotSupported ModeEnum = iota
	M_UseExistingDB
	M_MigrateExistingToNewDB
	M_MigrationInProgress
	M_UseNewDB
	M_InitiateDBUpgrade
	M_UpgradeDBInProgress
	M_PostMigrationInProgress
)

// getMode determines the mode of operation for the database claim.
func (r *DatabaseClaimReconciler) getMode(ctx context.Context, reqInfo *requestInfo, dbClaim *v1.DatabaseClaim) ModeEnum {
	return getMode(ctx, reqInfo, dbClaim)
}

func getMode(ctx context.Context, reqInfo *requestInfo, dbClaim *v1.DatabaseClaim) ModeEnum {
	// Shadow variable
	log := log.FromContext(ctx).WithValues("databaseclaim", dbClaim.Namespace+"/"+dbClaim.Name, "func", "getMode")
	//default mode is M_UseNewDB. any non supported combination needs to be identified and set to M_NotSupported

	if dbClaim.Status.OldDB.DbState == v1.PostMigrationInProgress {
		if dbClaim.Status.OldDB.ConnectionInfo == nil || dbClaim.Status.ActiveDB.DbState != v1.Ready ||
			reqInfo.SharedDBHost {
			return M_NotSupported
		}
	}

	if dbClaim.Status.OldDB.DbState == v1.PostMigrationInProgress && dbClaim.Status.ActiveDB.DbState == v1.Ready {
		return M_PostMigrationInProgress
	}

	if reqInfo.SharedDBHost {
		if dbClaim.Status.ActiveDB.DbState == v1.UsingSharedHost {
			activeHostParams := hostparams.GetActiveHostParams(dbClaim)
			if reqInfo.HostParams.IsUpgradeRequested(activeHostParams) {
				log.Info("upgrade requested for a shared host. shared host upgrades are not supported. ignoring upgrade request")
			}
		}
		log.V(debugLevel).Info("selected mode for shared db host", "dbclaim", dbClaim.Spec, "selected mode", "M_UseNewDB")

		return M_UseNewDB
	}

	// use existing is true
	if dbClaim.Spec.UseExistingSource != nil && *dbClaim.Spec.UseExistingSource {
		if dbClaim.Spec.SourceDataFrom == nil {
			log.Error(fmt.Errorf("source data from is nil"), "unsupported mode")
			return M_NotSupported
		}
		if dbClaim.Spec.SourceDataFrom.Type != "database" {
			log.Error(fmt.Errorf("unsupported SourceDataFrom.Type %s must be one of %q", dbClaim.Spec.SourceDataFrom.Type, []string{"database"}), "unsupported mode")
			return M_NotSupported
		}

		log.V(debugLevel).Info("selected mode for", "selected mode", "M_UseExistingDB")
		return M_UseExistingDB
	}

	// use existing is false // source data is present
	if dbClaim.Spec.SourceDataFrom != nil {
		if dbClaim.Spec.SourceDataFrom.Type != "database" {
			log.Error(fmt.Errorf("unsupported sourcedata from type"), "source data from type not supported")
			return M_NotSupported
		}

		if dbClaim.Status.ActiveDB.DbState == v1.UsingExistingDB {
			switch dbClaim.Status.MigrationState {
			case pgctl.S_Initial.String():
				fallthrough
			case "":
				log.V(debugLevel).Info("selected mode for", "dbclaim", dbClaim.Spec, "selected mode", "M_MigrateExistingToNewDB")
				return M_MigrateExistingToNewDB
			case pgctl.S_Completed.String():
				// Does nothing, behavior is undefined in existing logic
			default:
				log.V(debugLevel).Info("selected mode for", "dbclaim", dbClaim.Spec, "selected mode", "M_MigrationInProgress")
				return M_MigrationInProgress
			}
		}
	}
	// use existing is false // source data is not present
	if dbClaim.Spec.SourceDataFrom == nil {
		if dbClaim.Status.ActiveDB.DbState == v1.UsingExistingDB {
			//make sure status contains all the requires sourceDataFrom info
			if dbClaim.Status.ActiveDB.SourceDataFrom != nil {
				dbClaim.Spec.SourceDataFrom = dbClaim.Status.ActiveDB.SourceDataFrom.DeepCopy()
				if dbClaim.Status.MigrationState == "" || dbClaim.Status.MigrationState == pgctl.S_Initial.String() {
					log.V(debugLevel).Info("selected mode for", "dbclaim", dbClaim.Spec, "selected mode", "M_MigrateExistingToNewDB")
					return M_MigrateExistingToNewDB
				} else if dbClaim.Status.MigrationState != pgctl.S_Completed.String() {
					log.V(debugLevel).Info("selected mode for", "dbclaim", dbClaim.Spec, "selected mode", "M_MigrationInProgress")
					return M_MigrationInProgress
				}
			} else {
				log.Info("something is wrong. use existing is false // source data is not present. sourceDataFrom is not present in status")
				return M_NotSupported
			}
		}
	}

	// use existing is false; source data is not present ; active status is using-existing-db or ready
	// activeDB does not have sourceDataFrom info
	if dbClaim.Status.ActiveDB.DbState == v1.Ready {
		activeHostParams := hostparams.GetActiveHostParams(dbClaim)
		if reqInfo.HostParams.IsUpgradeRequested(activeHostParams) {
			if dbClaim.Status.NewDB.DbState == "" {
				dbClaim.Status.NewDB.DbState = v1.InProgress
				dbClaim.Status.MigrationState = ""
			}
			if dbClaim.Status.MigrationState == "" || dbClaim.Status.MigrationState == pgctl.S_Initial.String() {
				log.V(debugLevel).Info("selected mode for", "dbclaim", dbClaim.Spec, "selected mode", "M_InitiateDBUpgrade")
				return M_InitiateDBUpgrade
			} else if dbClaim.Status.MigrationState != pgctl.S_Completed.String() {
				log.V(debugLevel).Info("selected mode for", "dbclaim", dbClaim.Spec, "selected mode", "M_UpgradeDBInProgress")
				return M_UpgradeDBInProgress

			}
		}
	}

	log.V(debugLevel).Info("selected mode for", "dbclaim", dbClaim.Spec, "selected mode", "M_UseNewDB")

	return M_UseNewDB
}
