package basefunctions

import (
	"fmt"
	"time"

	"github.com/go-logr/logr"
	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
	"github.com/infobloxopen/db-controller/pkg/dbclient"
	gopassword "github.com/sethvargo/go-password/password"
	"github.com/spf13/viper"
)

var (
	minRotationTime = 60 * time.Minute // rotation time in minutes
	maxRotationTime = 1440 * time.Minute
	maxWaitTime     = 10 * time.Minute
	defaultPassLen  = 32
	defaultNumDig   = 10
	defaultNumSimb  = 10
)

func GetMinRotationTime() time.Duration {
	return minRotationTime
}

func GetMaxRotationTime() time.Duration {
	return maxRotationTime
}

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

func GetClientForExistingDB(connInfo *persistancev1.DatabaseClaimConnectionInfo, log *logr.Logger) (dbclient.Client, error) {

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

func GeneratePassword(config *viper.Viper) (string, error) {
	var pass string
	var err error
	minPasswordLength := GetMinPasswordLength(config)
	complEnabled := isPasswordComplexity(config)

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

func isPasswordComplexity(config *viper.Viper) bool {
	complEnabled := config.GetString("passwordconfig::passwordComplexity")

	return complEnabled == "enabled"
}

func GetMinPasswordLength(config *viper.Viper) int {
	return config.GetInt("passwordconfig::minPasswordLength")
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
