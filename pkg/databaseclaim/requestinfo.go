package databaseclaim

import (
	"fmt"
	v1 "github.com/infobloxopen/db-controller/api/v1"
	basefun "github.com/infobloxopen/db-controller/pkg/basefunctions"
	"github.com/infobloxopen/db-controller/pkg/hostparams"
	"github.com/spf13/viper"
)

// requestInfo is a struct that holds the information needed to create a database.
type requestInfo struct {
	DbType                     v1.DatabaseType
	SharedDBHost               bool
	MasterConnInfo             v1.DatabaseClaimConnectionInfo
	TempSecret                 string
	HostParams                 hostparams.HostParams
	EnableReplicationRole      bool
	EnableSuperUser            bool
	EnablePerfInsight          bool
	EnableCloudwatchLogsExport []*string
	BackupRetentionDays        int64
	CACertificateIdentifier    string
}

// NewRequestInfo creates a new requestInfo struct.
func NewRequestInfo(cfg *viper.Viper, dbClaim *v1.DatabaseClaim) (requestInfo, error) {
	var (
		sharedDBHost            bool
		enablePerfInsight       bool
		cloudwatchLogsExport    []*string
		backupRetentionDays     int64
		caCertificateIdentifier string
	)

	backupRetentionDays = basefun.GetBackupRetentionDays(cfg)
	caCertificateIdentifier = basefun.GetCaCertificateIdentifier(cfg)
	enablePerfInsight = basefun.GetEnablePerfInsight(cfg)
	enableCloudwatchLogsExport := basefun.GetEnableCloudwatchLogsExport(cfg)
	postgresCloudwatchLogsExportLabels := []string{"postgresql", "upgrade"}

	switch enableCloudwatchLogsExport {
	case "all":
		for _, export := range postgresCloudwatchLogsExportLabels {
			cloudwatchLogsExport = append(cloudwatchLogsExport, &export)
		}
	case "none":
		cloudwatchLogsExport = nil
	default:
		cloudwatchLogsExport = append(cloudwatchLogsExport, &enableCloudwatchLogsExport)
	}

	hostParams, err := hostparams.New(cfg, dbClaim)
	if err != nil {
		return requestInfo{}, fmt.Errorf("error creating host params: %w", err)
	}

	//check if dbclaim.name is > maxNameLen and if so, error out
	if len(dbClaim.Name) > maxNameLen {
		return requestInfo{}, ErrMaxNameLen
	}

	var enableSuperUser bool
	if basefun.GetSuperUserElevation(cfg) {
		enableSuperUser = *dbClaim.Spec.EnableSuperUser
	}

	var enableReplicationRole bool
	if enableSuperUser {
		// if superuser elevation is enabled, enabling replication role is redundant
		enableReplicationRole = false
	} else {
		enableReplicationRole = *dbClaim.Spec.EnableReplicationRole
	}

	masterConnInfo := v1.DatabaseClaimConnectionInfo{
		DatabaseName: dbClaim.Spec.DatabaseName,
	}

	ri := requestInfo{
		SharedDBHost:               sharedDBHost,
		DbType:                     dbClaim.Spec.Type,
		MasterConnInfo:             masterConnInfo,
		HostParams:                 *hostParams,
		EnableReplicationRole:      enableReplicationRole,
		EnableSuperUser:            enableSuperUser,
		EnablePerfInsight:          enablePerfInsight,
		EnableCloudwatchLogsExport: cloudwatchLogsExport,
		BackupRetentionDays:        backupRetentionDays,
		CACertificateIdentifier:    caCertificateIdentifier,
	}

	return ri, nil
}
