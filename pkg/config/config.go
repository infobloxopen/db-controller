package config

import (
	"fmt"
	"github.com/fsnotify/fsnotify"
	"github.com/go-logr/logr"
	"github.com/spf13/viper"
)

const (
	AWSAuthSourceType    = "aws"
	SecretAuthSourceType = "secret"
)

// NewConfig creates a new db-controller config
func NewConfig(logger logr.Logger, configFile string) *viper.Viper {
	c := viper.NewWithOptions(viper.KeyDelimiter("::"))
	c.SetDefault("config", "config.yaml")
	c.SetConfigFile(configFile)
	logger.Info("loading config")
	err := c.ReadInConfig() // Find and read the config file
	if err != nil {         // Handle errors reading the config file
		panic(fmt.Errorf("fatal error config file: %s", err))
	}

	// monitor the changes in the config file
	c.WatchConfig()
	c.OnConfigChange(func(e fsnotify.Event) {
		logger.Info("Config file changed", "file", e.Name)
		logger.Info(fmt.Sprint(c.AllSettings()))
	})

	return c
}
