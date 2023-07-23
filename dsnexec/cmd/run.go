package cmd

import (
	"context"
	"os"
	"os/signal"

	"github.com/infobloxopen/db-controller/dsnexec/pkg/fdsnexec"
	_ "github.com/infobloxopen/db-controller/dsnexec/pkg/fprintf"
	_ "github.com/infobloxopen/db-controller/dsnexec/pkg/shelldb"
	"github.com/infobloxopen/hotload"
	_ "github.com/infobloxopen/hotload/fsnotify"
	"github.com/lib/pq"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"
)

func init() {
	hotload.RegisterSQLDriver("postgres", pq.Driver{})
}

// runCmd represents the run command
var runCmd = &cobra.Command{
	Use:   "run",
	Short: "run a dsnexec watcher",
	Long:  `dsnexec run will run a dsnexec watcher based on the config file provided.`,
	Run: func(cmd *cobra.Command, args []string) {
		signalChan := make(chan os.Signal, 1)
		signal.Notify(signalChan, os.Interrupt)

		ctx, cancel := context.WithCancel(context.Background())

		go func() {
			<-signalChan
			cancel()
		}()

		c, err := parseConfig(confFile)
		if err != nil {
			log.Fatalf("failed to parse config: %s", err)
		}

		for _, e := range enablingFlags {
			if _, ok := c.Configs[e]; ok {
				c.Configs[e].Disabled = false
			}
		}
		for _, d := range disablingFlags {
			if _, ok := c.Configs[d]; ok {
				c.Configs[d].Disabled = true
			}
		}

		handler, err := fdsnexec.NewHandler(c)
		if err != nil {
			log.Fatalf("failed to create handler: %s", err)
		}
		if err := handler.Run(ctx); err != nil {
			log.Fatalf("failed to run handler: %s", err)
		}
	},
}

func parseConfig(f string) (*fdsnexec.InputFile, error) {
	var c fdsnexec.InputFile
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

var confFile string
var enablingFlags []string
var disablingFlags []string

func init() {
	rootCmd.AddCommand(runCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// runCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	runCmd.Flags().StringVarP(&confFile, "config-file", "c", "config.yaml", "Path to config file")

	runCmd.Flags().StringSliceVarP(&enablingFlags, "enable", "e", []string{}, "Enable a config by name")
	runCmd.Flags().StringSliceVarP(&disablingFlags, "disable", "d", []string{}, "Disable a config by name")
}
