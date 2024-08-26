package dbproxy

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"github.com/infobloxopen/db-controller/dbproxy/pgbouncer"
	"go.uber.org/zap"
)

var logger logr.Logger

func init() {
	zapLog, _ := zap.NewDevelopment()
	logger = zapr.NewLogger(zapLog)
}

type Config struct {
	StartUpTime      time.Duration
	DBCredentialPath string
	PGCredentialPath string
	PGBStartScript   string
	PGBReloadScript  string
	LocalAddr        string
}

func waitForFiles(ctx context.Context, paths ...string) error {
	var errs []error
	wg := &sync.WaitGroup{}
	for _, path := range paths {
		wg.Add(1)
		go func(path string) {
			defer wg.Done()
			err := waitForFile(ctx, path)
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

func waitForFile(ctx context.Context, path string) error {
	logr := logger.WithValues("path", path)
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

type mgr struct {
	cfg Config
}

func New(ctx context.Context, cfg Config) (*mgr, error) {

	flag.Parse()

	if cfg.StartUpTime == 0 {
		cfg.StartUpTime = 30 * time.Second
	}

	waitCtx, cancel := context.WithTimeout(ctx, cfg.StartUpTime)
	if err := waitForFiles(waitCtx, cfg.DBCredentialPath); err != nil {
		return nil, err
	}
	cancel()
	return &mgr{cfg: cfg}, nil
}

func (m *mgr) Start(ctx context.Context) error {

	cfg := m.cfg

	pgbCfg, err := pgbouncer.NewConfig(pgbouncer.Params{
		DSNPath:   cfg.DBCredentialPath,
		LocalAddr: cfg.LocalAddr,
		OutPath:   cfg.PGCredentialPath,
	})
	if err != nil {
		return err
	}

	if err := pgbCfg.Write(); err != nil {
		return err
	}

	if err := pgbouncer.Start(context.TODO(), cfg.PGBStartScript); err != nil {
		return err
	}

	// Watch for ongoing changes and regenerate pgbouncer config
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()

	done := make(chan error)
	go func() {
		defer close(done)

		for {
			select {
			case <-ctx.Done():
				done <- nil
				return
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				logger.V(1).Info("received_update", "event", event)
				// FIXME: check if file change is one we actually care about
				err := watcher.Remove(cfg.DBCredentialPath)
				if err != nil {
					if !errors.Is(err, fsnotify.ErrNonExistentWatch) {
						log.Fatal("Remove failed:", err)
					}
				}

				waitCtx, cancel := context.WithTimeout(ctx, cfg.StartUpTime)
				if err := waitForFiles(waitCtx, cfg.DBCredentialPath); err != nil {
					logger.Error(err, "waitForFiles failed")
				}
				cancel()

				err = watcher.Add(cfg.DBCredentialPath)
				if err != nil {
					logger.Error(err, "add failed")
					os.Exit(1)
				}

				err = pgbCfg.Write()
				if errors.Is(err, pgbouncer.ErrDuplicateWrite) {
					logger.V(1).Info("ignoring duplicate write")
					continue
				} else if err != nil {
					log.Println("parseWritePGBConfig failed:", err)
					continue
				}

				logger.V(1).Info("reload_pgbouncer")
				if err := pgbouncer.Reload(context.TODO(), cfg.PGBReloadScript); err != nil {
					logger.Error(err, "reload_pgbouncer_error")
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				logger.Error(err, "watcher_err")
			}
		}

	}()

	err = watcher.Add(cfg.DBCredentialPath)
	if err != nil {
		log.Fatal("Add failed:", err)
	}
	return <-done
}
