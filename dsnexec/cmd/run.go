package cmd

import (
	"fmt"
	"os"

	"github.com/infobloxopen/db-controller/dsnexec/pkg/dsnexec"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"
)

// runCmd represents the run command
var runCmd = &cobra.Command{
	Use:   "run",
	Short: "run a dsnexec watcher",
	Long:  `dsnexec run will run a dsnexec watcher based on the config file provided.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("run called")
	},
}

func parseConfig(f string) (*Config, error) {
	var c Config
	// read file
	bs, err := os.ReadFile(f)
	if err != nil {
		return nil, err
	}
	// unmarshal
	err = yaml.Unmarshal(bs, &c)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

type Config struct {
	Configs map[string]dsnexec.Config `yaml:"configs"`
}

var confFile string

func init() {
	rootCmd.AddCommand(runCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// runCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	runCmd.Flags().StringVarP(&confFile, "config-file", "c", "conf.yaml", "Path to config file")
}
