package fdsnexec

import "github.com/infobloxopen/db-controller/dsnexec/pkg/dsnexec"

type Source struct {
	Driver   string `yaml:"driver"`
	Filename string `yaml:"filename"`
}

// InputFile is the input file format for fdsnexec. It is a yaml file with
// a top level key of configs. The configs key is a map of config names to
// Configs.
type InputFile struct {
	Configs map[string]*Config `yaml:"configs"`
}

// Config is the config for a single fdsnexec instance.
type Config struct {
	Disabled    bool               `yaml:"disabled"`
	Sources     []Source           `yaml:"sources"`
	Destination dsnexec.DBConnInfo `yaml:"destination"`
	Commands    []dsnexec.Command  `yaml:"commands"`
}
