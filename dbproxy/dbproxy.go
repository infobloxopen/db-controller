package dbproxy

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
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
	DBPasswordPath   string
	PGCredentialPath string
	PGBStartScript   string
	PGBReloadScript  string
	Port             int
}

type cachedCredentials struct {
	user, pass string
}

func (c *cachedCredentials) verifyIfNewCreds(newUser, newPass string) bool {
	if c.pass != newPass || c.user != newUser {
		c.pass = newPass
		c.user = newUser
		return true
	}
	return false
}

// FIXME: remove password path, uri_dsn has the password in it
func generatePGBouncerConfiguration(dbCredentialPath, dbPasswordPath string, port int, pbCredentialPath string) (string, string) {

	bs, err := ioutil.ReadFile(dbCredentialPath)
	if err != nil {
		logger.Error(err, "failed to read database dsn file")
		os.Exit(1)
	}

	cfg, err := pgbouncer.GenerateConfig(string(bs), dbPasswordPath, int16(port))
	if err != nil {
		logger.Error(err, "failed to generate config")
		os.Exit(1)
	}

	err = pgbouncer.WritePGBouncerConfig(pbCredentialPath, &cfg)
	if err != nil {
		logger.Error(err, "failed to write pgbouncerconfig")
		os.Exit(1)
	}
	return cfg.UserName, cfg.Password
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

	if cfg.Port < 1 || cfg.Port > 65535 {
		return nil, fmt.Errorf("invalid_port_number must be between 1 and 65535: %d", cfg.Port)
	}

	if cfg.StartUpTime == 0 {
		cfg.StartUpTime = 30 * time.Second
	}

	waitCtx, cancel := context.WithTimeout(ctx, cfg.StartUpTime)
	if err := waitForFiles(waitCtx, cfg.DBCredentialPath, cfg.DBPasswordPath); err != nil {
		return nil, err
	}
	cancel()
	return &mgr{cfg: cfg}, nil
}

func (m *mgr) Start(ctx context.Context) error {

	cfg := m.cfg
	// First time pgbouncer config generation and start
	user, pass := generatePGBouncerConfiguration(cfg.DBCredentialPath, cfg.DBPasswordPath, cfg.Port, cfg.PGCredentialPath)

	if err := pgbouncer.Start(context.TODO(), cfg.PGBStartScript); err != nil {
		return err
	}

	cachedCreds := cachedCredentials{user: user, pass: pass}

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
				log.Printf("%s %s\n", event.Name, event.Op)
				err := watcher.Remove(cfg.DBCredentialPath)
				if err != nil {
					if !errors.Is(err, fsnotify.ErrNonExistentWatch) {
						log.Fatal("Remove failed:", err)
					}
				}

				waitCtx, cancel := context.WithTimeout(ctx, cfg.StartUpTime)
				if err := waitForFiles(waitCtx, cfg.DBCredentialPath, cfg.DBPasswordPath); err != nil {
					log.Println("waitForFiles failed:", err)
				}
				cancel()

				err = watcher.Add(cfg.DBCredentialPath)
				if err != nil {
					log.Fatal("Add failed:", err)
				}
				// Regenerate pgbouncer configuration and signal pgbouncer to reload cconfiguration
				newUser, newPass := generatePGBouncerConfiguration(cfg.DBCredentialPath, cfg.DBPasswordPath, cfg.Port, cfg.PGCredentialPath)
				if err != nil {
					log.Println("parseWritePGBConfig failed:", err)
					continue
				}

				if cachedCreds.verifyIfNewCreds(newUser, newPass) {

					if err := pgbouncer.ReloadConfiguration(context.TODO(), cfg.PGBReloadScript); err != nil {
						panic(err)
					}
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Println("watcher_error:", err)
			}
		}

	}()

	err = watcher.Add(cfg.DBCredentialPath)
	if err != nil {
		log.Fatal("Add failed:", err)
	}
	return <-done
}
