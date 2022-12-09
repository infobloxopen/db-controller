package hostparams

import (
	"fmt"
	"hash/crc32"
	"strconv"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
	"github.com/spf13/viper"
)

type HostParams struct {
	Engine                          string
	Shape                           string
	MinStorageGB                    int
	EngineVersion                   string
	MasterUsername                  string
	SkipFinalSnapshotBeforeDeletion bool
	PubliclyAccessible              bool
	EnableIAMDatabaseAuthentication bool
	DeletionPolicy                  xpv1.DeletionPolicy
	Port                            int64
	isDefaultEngine                 bool
	isDefaultShape                  bool
	isDefaultStorage                bool
	isDefaultVersion                bool
}

func (p *HostParams) String() string {
	return fmt.Sprintf("%s-%s-%s-%s", p.Engine, p.Shape, p.EngineVersion, strconv.Itoa(p.MinStorageGB))
}

func (p *HostParams) Hash() string {
	crc32q := crc32.MakeTable(0xD5828281)
	return fmt.Sprintf("%08x", crc32.Checksum([]byte(p.String()), crc32q))
}

func (p *HostParams) HasShapeChanged(activeShape string) bool {
	if p.isDefaultShape {
		// request is for a "" shape
		// default request should not trigger an upgrade
		return false
	}
	return activeShape != p.Shape
}

func (p *HostParams) HasStorageChanged(activeStorage int) bool {
	if p.isDefaultStorage {
		return false
	}
	return activeStorage != p.MinStorageGB
}

func (p *HostParams) HasEngineChanged(activeEngine string) bool {
	if p.isDefaultEngine {
		return false
	}
	return activeEngine != p.Engine
}

func (p *HostParams) HasVersionChanged(activeVersion string) bool {
	if p.isDefaultVersion {
		return false
	}
	return activeVersion != p.EngineVersion
}

func (p *HostParams) IsUpgradeRequested(np *HostParams) bool {
	return p.HasEngineChanged(np.Engine) ||
		p.HasShapeChanged(np.Shape) ||
		p.HasStorageChanged(np.MinStorageGB) ||
		p.HasVersionChanged(np.EngineVersion)

}

func New(config *viper.Viper, fragmentKey string, dbClaim *persistancev1.DatabaseClaim) (*HostParams, error) {
	var (
		err   error
		port  string
		iport int
	)
	hostParams := HostParams{}

	if fragmentKey == "" {
		hostParams.Engine = string(dbClaim.Spec.Type)
		hostParams.EngineVersion = dbClaim.Spec.DBVersion
		hostParams.Shape = dbClaim.Spec.Shape
		hostParams.MinStorageGB = dbClaim.Spec.MinStorageGB
		port = dbClaim.Spec.Port
	} else {
		hostParams.MasterUsername = config.GetString(fmt.Sprintf("%s::masterUsername", fragmentKey))
		hostParams.Engine = config.GetString(fmt.Sprintf("%s::Engine", fragmentKey))
		hostParams.EngineVersion = config.GetString(fmt.Sprintf("%s::Engineversion", fragmentKey))
		hostParams.Shape = config.GetString(fmt.Sprintf("%s::shape", fragmentKey))
		hostParams.MinStorageGB = config.GetInt(fmt.Sprintf("%s::minStorageGB", fragmentKey))
		port = config.GetString(fmt.Sprintf("%s::Port", fragmentKey))
	}

	if port == "" {
		port = config.GetString("defaultMasterPort")
	}
	iport, err = strconv.Atoi(port)
	if err != nil {
		return nil, fmt.Errorf("invalid master port")
	}
	hostParams.Port = int64(iport)

	if hostParams.MasterUsername == "" {
		hostParams.MasterUsername = config.GetString("defaultMasterUsername")
	}

	if hostParams.EngineVersion == "" {
		hostParams.isDefaultVersion = true
		hostParams.EngineVersion = config.GetString("defaultEngineVersion")
	}

	if hostParams.Shape == "" {
		hostParams.isDefaultShape = true
		hostParams.Shape = config.GetString("defaultShape")
	}

	if hostParams.Engine == "" {
		hostParams.isDefaultEngine = true
		hostParams.Engine = config.GetString("defaultEngine")
	}

	if hostParams.MinStorageGB == 0 {
		hostParams.isDefaultStorage = true
		hostParams.MinStorageGB = config.GetInt("defaultMinStorageGB")
	}

	hostParams.SkipFinalSnapshotBeforeDeletion = config.GetBool("defaultSkipFinalSnapshotBeforeDeletion")
	hostParams.PubliclyAccessible = config.GetBool("defaultPubliclyAccessible")
	if config.GetString("defaultDeletionPolicy") == "delete" {
		hostParams.DeletionPolicy = xpv1.DeletionDelete
	} else {
		hostParams.DeletionPolicy = xpv1.DeletionOrphan
	}

	// TODO - Enable IAM auth based on authSource config
	hostParams.EnableIAMDatabaseAuthentication = false

	return &hostParams, nil
}

func GetActiveHostParams(dbClaim *persistancev1.DatabaseClaim) *HostParams {

	hostParams := HostParams{}

	hostParams.Engine = string(dbClaim.Status.ActiveDB.Type)
	hostParams.EngineVersion = dbClaim.Status.ActiveDB.DBVersion
	hostParams.Shape = dbClaim.Status.ActiveDB.Shape
	hostParams.MinStorageGB = dbClaim.Status.ActiveDB.MinStorageGB

	return &hostParams
}
