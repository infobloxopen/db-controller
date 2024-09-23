package hostparams

import (
	"errors"
	"fmt"
	"hash/crc32"
	"strconv"
	"strings"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	v1 "github.com/infobloxopen/db-controller/api/v1"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/spf13/viper"
)

var (
	defaultAuroraPostgresStr = "aurora-postgresql"
	shapeDelimiter           = "!"
	INSTANCE_CLASS_INDEX     = 0
	STORAGE_TYPE_INDEX       = 1
	// DO NOT CHANGE THE DEFAULTS -
	// They are used to determine the hash for RDS names. Any change will result in a new RDS instance being created for all applications!
	// These values are purposely moved from the config file to the code to avoid accidental changes.
	defaultShape         = "db.t4g.medium"
	defaultEngineVersion = "15.3"
	defaultEngine        = v1.Postgres
)

var (
	ErrMaxStorageReduced         = errors.New("reducing .spec.maxStorageGB value is not allowed (Also not spacifying maxStorageGB if specified earlier is not allowed.)")
	ErrMaxStorageLesser          = errors.New(".spec.maxStorageGB should always be greater than spec.minStorageGB")
	ErrEngineVersionNotSpecified = errors.New(".spec.dbVersion is a mandatory field and cannot be empty")
)

type HostParams struct {
	// FIXME: this should be DatabaseType, not string
	Type string

	Shape                           string
	MinStorageGB                    int
	MaxStorageGB                    int64
	DBVersion                       string
	MasterUsername                  string
	InstanceClass                   string
	StorageType                     string
	SkipFinalSnapshotBeforeDeletion bool
	PubliclyAccessible              bool
	EnableIAMDatabaseAuthentication bool
	DeletionPolicy                  xpv1.DeletionPolicy
	Port                            int64
	isDefaultEngine                 bool
	isDefaultShape                  bool
	isDefaultInstanceClass          bool
	isDefaultStorage                bool
	IsDefaultVersion                bool
}

func (p *HostParams) String() string {
	return fmt.Sprintf("%s-%s-%s", p.Type, p.InstanceClass, p.DBVersion)
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

func (p *HostParams) HasInstanceClassChanged(activeInstanceClass string) bool {
	if p.isDefaultInstanceClass {
		// request is for a "" shape
		// default request should not trigger an upgrade
		return false
	}
	return activeInstanceClass != p.InstanceClass
}

func (p *HostParams) HasStorageChanged(activeStorage int) bool {
	// storage is not applicable to aurora postgres
	if p.Type == defaultAuroraPostgresStr {
		return false
	}
	if p.isDefaultStorage {
		return false
	}
	return activeStorage != p.MinStorageGB
}

func (p *HostParams) HasEngineChanged(activeEngine string) bool {
	if p.isDefaultEngine {
		return false
	}
	return activeEngine != p.Type
}

func (p *HostParams) HasVersionChanged(activeVersion string) bool {
	if p.IsDefaultVersion {
		return false
	}
	return strings.Split(activeVersion, ".")[0] != strings.Split(p.DBVersion, ".")[0]
}

func (p *HostParams) IsUpgradeRequested(active *HostParams) bool {
	return p.HasEngineChanged(active.Type) ||
		p.HasInstanceClassChanged(active.InstanceClass) ||
		p.HasVersionChanged(active.DBVersion)
}

func New(config *viper.Viper, dbClaim *v1.DatabaseClaim) (*HostParams, error) {
	var (
		err   error
		port  string
		iport int
	)
	hostParams := HostParams{}

	hostParams.DeletionPolicy = xpv1.DeletionPolicy(
		cases.Title(language.English, cases.Compact).String(string(dbClaim.Spec.DeletionPolicy)))
	hostParams.Type = string(dbClaim.Spec.Type)
	hostParams.DBVersion = dbClaim.Spec.DBVersion
	hostParams.Shape = dbClaim.Spec.Shape
	hostParams.MinStorageGB = dbClaim.Spec.MinStorageGB
	hostParams.MaxStorageGB = dbClaim.Spec.MaxStorageGB

	port = config.GetString("defaultMasterPort")
	iport, err = strconv.Atoi(port)
	if err != nil {
		return nil, fmt.Errorf("invalid master port")
	}
	hostParams.Port = int64(iport)

	if hostParams.MasterUsername == "" {
		hostParams.MasterUsername = config.GetString("defaultMasterUsername")
	}

	if hostParams.DBVersion == "" {
		if dbClaim.Status.ActiveDB.DBVersion != "" {
			hostParams.DBVersion = dbClaim.Status.ActiveDB.DBVersion
		} else {
			hostParams.IsDefaultVersion = true
			hostParams.DBVersion = defaultEngineVersion
		}
	}

	if hostParams.Shape == "" {
		hostParams.isDefaultShape = true
		hostParams.isDefaultInstanceClass = true
		hostParams.Shape = defaultShape
	}

	if hostParams.Type == "" {
		hostParams.isDefaultEngine = true
		hostParams.Type = string(defaultEngine)
	}

	if hostParams.MinStorageGB == 0 {
		hostParams.isDefaultStorage = true
		hostParams.MinStorageGB = config.GetInt("defaultMinStorageGB")
	}

	hostParams.SkipFinalSnapshotBeforeDeletion = config.GetBool("defaultSkipFinalSnapshotBeforeDeletion")
	hostParams.PubliclyAccessible = config.GetBool("defaultPubliclyAccessible")
	if hostParams.DeletionPolicy == "" {
		if config.GetString("defaultDeletionPolicy") == "delete" {
			hostParams.DeletionPolicy = xpv1.DeletionDelete
		} else {
			hostParams.DeletionPolicy = xpv1.DeletionOrphan
		}
	}

	// TODO - Enable IAM auth based on authSource config
	hostParams.EnableIAMDatabaseAuthentication = false

	hostParams.InstanceClass = getInstanceClass(hostParams.Shape)
	hostParams.StorageType, err = getStorageType(config, hostParams.Type, hostParams.Shape)
	if err != nil {
		return &HostParams{}, err
	}

	if hostParams.Type == string(v1.Postgres) {
		if hostParams.MaxStorageGB == 0 {
			if dbClaim.Status.ActiveDB.MaxStorageGB != 0 {
				return &HostParams{}, ErrMaxStorageReduced
			}
		} else if hostParams.MaxStorageGB < dbClaim.Status.ActiveDB.MaxStorageGB {
			return &HostParams{}, ErrMaxStorageReduced
		} else if hostParams.MaxStorageGB <= int64(hostParams.MinStorageGB) {
			return &HostParams{}, ErrMaxStorageLesser
		}
	}

	return &hostParams, nil
}

// Retrieves the current scenario, wha engine, version, instance, etc. actually is deployed
func GetActiveHostParams(dbClaim *v1.DatabaseClaim) *HostParams {

	hostParams := HostParams{}

	hostParams.Type = string(dbClaim.Status.ActiveDB.Type)
	hostParams.DBVersion = dbClaim.Status.ActiveDB.DBVersion
	hostParams.Shape = dbClaim.Status.ActiveDB.Shape
	hostParams.InstanceClass = getInstanceClass(hostParams.Shape)
	hostParams.MinStorageGB = dbClaim.Status.ActiveDB.MinStorageGB

	return &hostParams
}

func getInstanceClass(shape string) string {
	shapeParts := strings.Split(shape, shapeDelimiter)
	return shapeParts[INSTANCE_CLASS_INDEX]
}

func getStorageType(config *viper.Viper, engine string, shape string) (string, error) {
	storageType := ""
	if engine == defaultAuroraPostgresStr {
		shapeParts := strings.Split(shape, shapeDelimiter)
		if len(shapeParts) > STORAGE_TYPE_INDEX {
			switch strings.ToLower(shapeParts[STORAGE_TYPE_INDEX]) {
			case "io1":
				storageType = "aurora-iopt1"
			case "io-optimized":
				storageType = "aurora-iopt1"
			case "standard":
				storageType = "aurora"
			case "aurora":
				storageType = "aurora"
			case "":
				storageType = "aurora"
			default:
				return "", fmt.Errorf("invalid shape")
			}
		} else {
			storageType = "aurora"
		}
	} else {
		// TODO - add support for custom storage type for postgres
		storageType = config.GetString("storageType")
	}

	return storageType, nil
}
