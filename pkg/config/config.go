package config

import (
	"fmt"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/go-logr/logr"
	"github.com/spf13/viper"
)

// Authentication tokens have a lifespan of 15 minutes
const tokenExpirationTime = 900 * time.Second

const (
	AWSAuthSourceType    = "aws"
	SecretAuthSourceType = "secret"
)

func NewMasterAuth() *MasterAuth {
	return &MasterAuth{}
}

type MasterAuth struct {
	sync.RWMutex

	authToken string
	updatedAt time.Time
}

// Get gets new master token/password
func (ma *MasterAuth) Get() string {
	ma.RLock()
	defer ma.RUnlock()

	return ma.authToken
}

// IsExpired checks if RDS auth toke expired
func (ma *MasterAuth) IsExpired() bool {
	// consider that token is expired if more than 15 - 1 minutes have passed since the token update
	return time.Since(ma.updatedAt) > tokenExpirationTime-time.Minute
}

// Set sets new master token/password
func (ma *MasterAuth) Set(newToken string) {
	ma.Lock()
	defer ma.Unlock()

	ma.authToken = newToken
	ma.updatedAt = time.Now()
}

// NewConfig creates a new db-controller config
func NewConfig(logger logr.Logger, configFile string) *viper.Viper {
	c := viper.NewWithOptions(viper.KeyDelimiter("::"))
	c.SetDefault("config", "config.yaml")
	c.SetConfigFile(configFile)
	logger.Info("loading config")
	err := c.ReadInConfig() // Find and read the config file
	if err != nil {         // Handle errors reading the config file
		panic(fmt.Errorf("Fatal error config file: %s \n", err))
	}
	// monitor the changes in the config file
	fmt.Println(viper.AllSettings())
	c.WatchConfig()
	c.OnConfigChange(func(e fsnotify.Event) {
		logger.Info("Config file changed", "file", e.Name)
		logger.Info(fmt.Sprint(c.AllSettings()))
	})

	return c
}
