package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/infobloxopen/db-controller/dsnexec/pkg/dsnexec"
	"github.com/infobloxopen/hotload/fsnotify"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"
)

// runCmd represents the run command
var runCmd = &cobra.Command{
	Use:   "run",
	Short: "run a dsnexec watcher",
	Long:  `dsnexec run will run a dsnexec watcher based on the config file provided.`,
	Run: func(cmd *cobra.Command, args []string) {
		c, err := parseConfig(confFile)
		if err != nil {
			log.Fatalf("failed to parse config: %s", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		signalChan := make(chan os.Signal, 1)
		signal.Notify(signalChan, os.Interrupt)

		exited := make(chan error, len(c.Configs))
		for k := range c.Configs {
			cfg := c.Configs[k]
			go func(c Config) {
				exited <- c.run(ctx)
			}(cfg)
		}

		select {
		case err := <-exited:
			if err != nil {
				log.Fatalf("failed to run: %s", err)
			}
		case <-signalChan:
			fmt.Println("exiting...")
			cancel()
			for i := 0; i < len(c.Configs); i++ {
				err := <-exited
				if err != nil {
					log.Fatalf("failed to run: %s", err)
				}
			}
			return
		}

	},
}

func parseConfig(f string) (*InputFile, error) {
	var c InputFile
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

type InputFile struct {
	Configs map[string]Config `yaml:"configs"`
}

type Config struct {
	Sources     []Source           `yaml:"sources"`
	Destination dsnexec.DBConnInfo `yaml:"destination"`
	Commands    []dsnexec.Command  `yaml:"commands"`
}

type Destination struct {
	dsnexec.DBConnInfo
	Iterate bool
}

func (c Config) run(ctx context.Context) error {
	notifyS := fsnotify.NewStrategy()

	type update struct {
		filename string
		value    string
		driver   string
	}
	dsnConfig := dsnexec.Config{
		Sources:     make(map[string]dsnexec.DBConnInfo),
		Destination: c.Destination,
		Commands:    c.Commands,
	}
	updates := make(chan update, len(c.Sources))

	for _, s := range c.Sources {
		val, values, err := notifyS.Watch(ctx, s.Filename, nil)
		if err != nil {
			return err
		}
		dsnConfig.Sources[s.Filename] = dsnexec.DBConnInfo{
			Driver: s.Driver,
			DSN:    val,
		}
		go func(filename string, values <-chan string, driver string) {
			for {
				select {
				case <-ctx.Done():
					return
				case v := <-values:
					updates <- update{
						filename: filename,
						value:    v,
						driver:   driver,
					}
				}
			}
		}(s.Filename, values, s.Driver)
	}

	handler, err := dsnexec.NewHanlder(dsnexec.WithConfig(dsnConfig))
	if err != nil {
		return err
	}

	// initial sync
	if err := handler.Exec(); err != nil {
		return fmt.Errorf("failed initial execute: %v", err)
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case u := <-updates:
			log.Infof("updating dsn for %s", u.filename)
			if err := handler.UpdateDSN(u.filename, u.value); err != nil {
				return fmt.Errorf("failed to update dsn: %s", err)
			}
		}
	}
}

type Source struct {
	Driver   string `yaml:"driver"`
	Filename string `yaml:"filename"`
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
	runCmd.Flags().StringVarP(&confFile, "config-file", "c", "config.yaml", "Path to config file")
}
