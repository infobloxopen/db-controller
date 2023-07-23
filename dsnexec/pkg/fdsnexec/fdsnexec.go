package fdsnexec

import (
	"context"
	"fmt"

	"github.com/infobloxopen/db-controller/dsnexec/pkg/dsnexec"
	"github.com/infobloxopen/hotload/fsnotify"
	log "github.com/sirupsen/logrus"
)

// Handler is an instance of fdsnexec.
type Handler struct {
	config *InputFile
}

// NewHandler creates a new dsnexec handler.
func NewHandler(config *InputFile) (*Handler, error) {

	h := &Handler{
		config: config,
	}
	return h, nil
}

// Run the fdsnexec handler. This will block until the context is canceled
// or and unhandled error occurs. If there are errors parsing the config,
// reading the sources or executing agaist the destination, this will return
// an error.
func (h *Handler) Run(ctx context.Context) error {

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	exited := make(chan error, len(h.config.Configs))
	for k := range h.config.Configs {
		cfg := h.config.Configs[k]
		if cfg.Disabled {
			log.Infof("skipping disabled config %s", k)
			continue
		}
		go func(c *Config) {
			exited <- c.run(ctx)
		}(cfg)
	}

	select {
	case err := <-exited:
		return err
	case <-ctx.Done():
		log.Debug("exiting fdsnexec...")
		cancel()
		return nil
	}
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
