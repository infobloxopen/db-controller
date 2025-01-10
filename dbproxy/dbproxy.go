package dbproxy

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/go-logr/logr"
	"github.com/infobloxopen/db-controller/dbproxy/pgbouncer"
)

// DebugLevel is used to set V level to 1 as suggested by official docs
// https://github.com/kubernetes-sigs/controller-runtime/blob/main/TMP-LOGGING.md
const debugLevel = 1

// Config is the configuration for the dbproxy manager.
type Config struct {
	StartUpTime      time.Duration
	DBCredentialPath string
	PGCredentialPath string
	PGBStartScript   string
	PGBReloadScript  string
	LocalAddr        string
}

// DBProxy is a manager for the dbproxy.
type DBProxy struct {
	logger logr.Logger
	cfg    Config
}

// New creates a new dbproxy manager.
func New(ctx context.Context, logger logr.Logger, cfg Config) (*DBProxy, error) {
	if cfg.StartUpTime == 0 {
		cfg.StartUpTime = 30 * time.Second
	}

	pgbouncer.SetLogger(logger)

	waitCtx, cancel := context.WithTimeout(ctx, cfg.StartUpTime)
	defer cancel()

	if err := waitForFiles(waitCtx, logger, cfg.DBCredentialPath); err != nil {
		return nil, err
	}

	dbp := DBProxy{
		logger: logger,
		cfg:    cfg,
	}

	return &dbp, nil
}

// Start starts the dbproxy manager.
func (dbp *DBProxy) Start(ctx context.Context) error {

	cfg := dbp.cfg

	pgbCfg, err := pgbouncer.NewConfig(pgbouncer.Params{
		DSNPath:   cfg.DBCredentialPath,
		LocalAddr: cfg.LocalAddr,
		OutPath:   cfg.PGCredentialPath,
	})
	if err != nil {
		return err
	}

	if err := pgbCfg.Write(); err != nil {
		return fmt.Errorf("pgbCfg.Write failed: %w", err)
	}

	if err := pgbouncer.Start(context.TODO(), cfg.PGBStartScript); err != nil {
		log.Fatalf("FATAL: Unable to start pgbouncer: %v", err)
		panic(fmt.Sprintf("Unable to start pgbouncer: %v", err))
	}

	// Watch for ongoing changes and regenerate pgbouncer config
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("fsnotify.NewWatcher failed: %w", err)
	}
	defer watcher.Close()

	done := make(chan error)
	go func() {
		defer close(done)

		for {
			select {
			case <-ctx.Done():
				done <- nil
				dbp.logger.Info("context done")
				return
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}

				dbp.logger.Info("received_update", "event", event)

				// FIXME: check if file change is one we actually care about
				err := watcher.Remove(cfg.DBCredentialPath)
				if err != nil {
					dbp.logger.Error(err, "remove_failed", "credential_path", cfg.DBCredentialPath)
					if !errors.Is(err, fsnotify.ErrNonExistentWatch) {
						log.Fatal("Remove failed:", err)
					}
				}

				waitCtx, cancel := context.WithTimeout(ctx, cfg.StartUpTime)
				if err := waitForFiles(waitCtx, dbp.logger, cfg.DBCredentialPath); err != nil {
					dbp.logger.Error(err, "waitForFiles failed", "credential_path", cfg.DBCredentialPath)
				}
				cancel()

				err = watcher.Add(cfg.DBCredentialPath)
				if err != nil {
					dbp.logger.Error(err, "add_failed", "credential_path", cfg.DBCredentialPath)
					os.Exit(1)
				}

				err = pgbCfg.Write()
				if errors.Is(err, pgbouncer.ErrDuplicateWrite) {
					dbp.logger.Error(err, "ignoring_duplicate_write")
					continue
				} else if err != nil {
					dbp.logger.Error(err, "parseWritePGBConfig failed")
					continue
				}

				dbp.logger.Info("reloading_pgbouncer")
				if err := pgbouncer.Reload(context.TODO(), cfg.PGBReloadScript); err != nil {
					dbp.logger.Error(err, "pgbouncer.Reload failed")
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				dbp.logger.Error(err, "watcher_error")
			}
		}

	}()

	err = watcher.Add(cfg.DBCredentialPath)
	if err != nil {
		log.Fatal("Add failed:", err)
	}
	return <-done
}

func waitForFiles(ctx context.Context, logger logr.Logger, paths ...string) error {
	var errs []error
	wg := &sync.WaitGroup{}
	for _, path := range paths {
		wg.Add(1)
		go func(path string) {
			defer wg.Done()
			err := waitForFile(ctx, logger, path)
			if err != nil {
				errs = append(errs, err)
			}
		}(path)
	}
	wg.Wait()
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func waitForFile(ctx context.Context, logger logr.Logger, path string) error {
	logr := logger.WithValues("method", "waitForFile", "path", path)
	var sleep time.Duration
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout_wait_for_file: %s", path)
		case <-time.After(sleep):
			sleep = 5 * time.Second
			time.Sleep(time.Second)

			file, err := os.Open(path)
			if err != nil {
				logr.Error(err, "file_not_found")
				continue
			}

			stat, err := file.Stat()
			if err != nil {
				logr.Error(err, "failed to stat file")
				continue
			}

			if !stat.Mode().IsRegular() {
				logr.Info("irregular file")
				continue
			}
			return nil
		}
	}
}
