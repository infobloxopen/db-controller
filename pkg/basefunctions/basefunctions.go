package basefunctions

import (
	"fmt"
	"net/url"
	"time"

	"github.com/go-logr/logr"
	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
	"github.com/infobloxopen/db-controller/pkg/dbclient"
	gopassword "github.com/sethvargo/go-password/password"
	"github.com/spf13/viper"
	"k8s.io/utils/ptr"
)

var (
	maxWaitTime    = 10 * time.Minute
	defaultPassLen = 32
	defaultNumDig  = 10
	defaultNumSimb = 10
)

func GetMaxWaitTime() time.Duration {
	return maxWaitTime
}

func GetDefaultPassLen() int {
	return defaultPassLen
}

func GetDefaultNumDig() int {
	return defaultNumDig
}

func GetDefaultNumSimb() int {
	return defaultNumSimb
}

func IsClassPermitted(claimClass, controllerClass string) bool {
	if claimClass == "" {
		claimClass = "default"
	}
	if controllerClass == "" {
		controllerClass = "default"
	}
	if claimClass != controllerClass {
		return false
	}

	return true
}

func GetClientForExistingDB(connInfo *persistancev1.DatabaseClaimConnectionInfo, log *logr.Logger) (dbclient.Clienter, error) {

	err := ValidateConnectionParameters(connInfo)
	if err != nil {
		return nil, err
	}

	return dbclient.New(dbclient.Config{Log: *log, DBType: "postgres", DSN: connInfo.Uri()})
}

func ValidateConnectionParameters(connInfo *persistancev1.DatabaseClaimConnectionInfo) error {
	if connInfo == nil {
		return fmt.Errorf("invalid connection info")
	}

	if connInfo.Host == "" {
		return fmt.Errorf("invalid host name")
	}

	if connInfo.Port == "" {
		return fmt.Errorf("cannot get master port")
	}

	if connInfo.Username == "" {
		return fmt.Errorf("invalid credentials (username)")
	}

	if connInfo.SSLMode == "" {
		return fmt.Errorf("invalid sslMode")
	}

	if connInfo.Password == "" {
		return fmt.Errorf("invalid credentials (password)")
	}
	return nil
}

func GeneratePassword(viperConfig *viper.Viper) (string, error) {
	var pass string
	var err error
	minPasswordLength := GetMinPasswordLength(viperConfig)
	complEnabled := GetIsPasswordComplexity(viperConfig)

	// Customize the list of symbols.
	// Removed \ ` @ ! from the default list as the encoding/decoding was treating it as an escape character
	// In some cases downstream application was not able to handle it
	gen, err := gopassword.NewGenerator(&gopassword.GeneratorInput{
		Symbols: "~#%^&*()_+-={}|[]:<>?,.",
	})
	if err != nil {
		return "", err
	}

	if complEnabled {
		count := minPasswordLength / 4
		pass, err = gen.Generate(minPasswordLength, count, count, false, false)
		if err != nil {
			return "", err
		}
	} else {
		pass, err = gen.Generate(defaultPassLen, defaultNumDig, defaultNumSimb, false, false)
		if err != nil {
			return "", err
		}
	}

	return pass, nil
}

func GetMinPasswordLength(viperConfig *viper.Viper) int {
	return viperConfig.GetInt("passwordconfig::minPasswordLength")
}

func GenerateMasterPassword() (string, error) {
	var pass string
	var err error
	minPasswordLength := 30

	pass, err = gopassword.Generate(minPasswordLength, 3, 0, false, true)
	if err != nil {
		return "", err
	}
	return pass, nil
}

func SanitizeDsn(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return ""
	}
	u.User = url.UserPassword(u.User.Username(), "redacted")
	return u.String()
}

func GetDefaultMasterUser(viperConfig *viper.Viper) string {
	return viperConfig.GetString("defaultMasterUsername")
}

func GetDefaultMasterPort(viperConfig *viper.Viper) string {
	return viperConfig.GetString("defaultMasterPort")
}

func GetDefaultSSLMode(viperConfig *viper.Viper) string {
	return viperConfig.GetString("defaultSslMode")
}

func GetSuperUserElevation(viperConfig *viper.Viper) bool {
	return viperConfig.GetBool("supportSuperUserElevation")
}

func GetPasswordRotationPeriod(viperConfig *viper.Viper) time.Duration {
	return viperConfig.GetDuration("passwordconfig::passwordRotationPeriod")
}

func GetBackupRetentionDays(viperConfig *viper.Viper) int64 {
	return viperConfig.GetInt64("backupRetentionDays")
}
func GetDefaultBackupPolicy(viperConfig *viper.Viper) string {
	return viperConfig.GetString("defaultBackupPolicyValue")
}
func GetCaCertificateIdentifier(viperConfig *viper.Viper) string {
	return viperConfig.GetString("caCertificateIdentifier")
}
func GetEnablePerfInsight(viperConfig *viper.Viper) bool {
	return viperConfig.GetBool("enablePerfInsight")
}
func GetEnableCloudwatchLogsExport(viperConfig *viper.Viper) string {
	return viperConfig.GetString("enableCloudwatchLogsExport")
}
func GetPgTempFolder(viperConfig *viper.Viper) string {
	return viperConfig.GetString("pgTemp")
}
func GetDefaultReclaimPolicy(viperConfig *viper.Viper) string {
	return viperConfig.GetString("defaultReclaimPolicy")
}
func GetIsPasswordComplexity(viperConfig *viper.Viper) bool {
	complEnabled := viperConfig.GetString("passwordconfig::passwordComplexity")

	return complEnabled == "enabled"
}

func GetCloud(viperConfig *viper.Viper) string {
	return viperConfig.GetString("cloud")
}

func GetRegion(viperConfig *viper.Viper) string {
	return viperConfig.GetString("region")
}

// GCP only
func GetNetwork(viperConfig *viper.Viper) string {
	return fmt.Sprintf("projects/%s/global/networks/%s", GetProject(viperConfig), viperConfig.GetString("network"))
}

// GCP only
func GetSubNetwork(viperConfig *viper.Viper) string {
	return fmt.Sprintf("projects/%s/regions/%s/subnetworks/%s", GetProject(viperConfig), GetRegion(viperConfig), viperConfig.GetString("subnetwork"))
}

// GCP only
func GetProject(viperConfig *viper.Viper) string {
	return viperConfig.GetString("project")
}

// GCP only
func GetNumBackupsToRetain(viperConfig *viper.Viper) *float64 {
	return ptr.To(viperConfig.GetFloat64("numbackupstoretain"))
}

func GetMultiAZEnabled(viperConfig *viper.Viper) bool {
	return viperConfig.GetBool("dbMultiAZEnabled")
}

func GetVpcSecurityGroupIDRefs(viperConfig *viper.Viper) string {
	return viperConfig.GetString("vpcSecurityGroupIDRefs")
}

func GetProviderConfig(viperConfig *viper.Viper) string {
	return viperConfig.GetString("providerConfig")
}

func GetDbSubnetGroupNameRef(viperConfig *viper.Viper) string {
	return viperConfig.GetString("dbSubnetGroupNameRef")
}

func GetSystemFunctions(viperConfig *viper.Viper) map[string]string {
	return viperConfig.GetStringMapString("systemFunctions")
}

func GetDefaultMajorVersion(viperConfig *viper.Viper) string {
	return viperConfig.GetString("defaultMajorVersion")
}

func GetDynamicHostWaitTime(viperConfig *viper.Viper) time.Duration {
	t := time.Duration(viperConfig.GetInt("dynamicHostWaitTimeMin")) * time.Minute

	if t > GetMaxWaitTime() {
		// TODO: add this back maybe
		// r.Log.Info(fmt.Sprintf("dynamic host wait time is out of range, should be between 1min and %s", maxWaitTime))
		return time.Minute
	}

	return t
}

// GetDBIdentifierPrefix returns the prefix for the database identifier.
func GetDBIdentifierPrefix(viperConfig *viper.Viper) string {
	return viperConfig.GetString("dbIdentifierPrefix")
}
