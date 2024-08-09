package config

import (
	"fmt"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	AWSAuthSourceType    = "aws"
	SecretAuthSourceType = "secret"
)

var configLog = ctrl.Log.WithName("configLog")

// NewConfig creates a new db-controller config
func NewConfig(configFile string) *viper.Viper {
	c := viper.NewWithOptions(viper.KeyDelimiter("::"))
	c.SetDefault("config", "config.yaml")
	c.SetConfigFile(configFile)
	configLog.Info("loading config")
	err := c.ReadInConfig() // Find and read the config file
	if err != nil {         // Handle errors reading the config file
		panic(fmt.Errorf("fatal error config file: %s", err))
	}

	// monitor the changes in the config file
	c.WatchConfig()
	c.OnConfigChange(func(e fsnotify.Event) {
		configLog.Info("Config file changed", "file", e.Name)
		configLog.Info(fmt.Sprint(c.AllSettings()))
	})

	return c
}
