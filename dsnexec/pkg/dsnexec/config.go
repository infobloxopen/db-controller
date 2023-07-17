package dsnexec

import (
	"fmt"
)

// Command is a command to execute
type Command struct {
	CommandStr string
	Args       []string
}

// DBConnInfo is a database connection info
type DBConnInfo struct {
	Driver string
	DSN    string
}

// Config is the configuration for the dsnexec
type Config struct {
	Sources     map[string]DBConnInfo `yaml:"sources"`
	Destination DBConnInfo            `yaml:"destination"`
	Commands    []Command             `yaml:"commands"`
}

// Validate validates the db conn info
func (c DBConnInfo) Validate() error {
	if c.Driver == "" {
		return fmt.Errorf("type must be set on db conn info")
	}
	return nil
}

// Validate validates the config
func (c *Config) Validate() error {
	if len(c.Sources) == 0 {
		return fmt.Errorf("sources must be set")
	}
	for k, v := range c.Sources {
		if k == "" {
			return fmt.Errorf("file key must be set")
		}
		if err := v.Validate(); err != nil {
			return fmt.Errorf("file value must be set")
		}
	}
	if len(c.Commands) == 0 {
		return fmt.Errorf("commands must be set")
	}
	for _, v := range c.Commands {
		if v.CommandStr == "" {
			return fmt.Errorf("command_str must be set")
		}
	}

	if err := c.Destination.Validate(); err != nil {
		return fmt.Errorf("database_dsn must be set: %s", err)
	}
	return nil
}
