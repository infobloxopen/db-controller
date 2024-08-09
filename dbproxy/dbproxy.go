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
	"go.uber.org/zap/zapcore"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// logger is used since some times we run from testing.M and testing.T is not available
var logger logr.Logger

func init() {
	// Use zap logger
	opts := zap.Options{
		Development: true,
		// Enable this to debug this code
		//Level: zapcore.DebugLevel,
		Level: zapcore.InfoLevel,
	}

	logger = zap.New(zap.UseFlagOptions(&opts))

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

func parseWritePGBConfig(dbCredentialPath, dbPasswordPath string, port int, pbCredentialPath string) (string, string, error) {
	dbc, err := pgbouncer.ParseDBCredentials(dbCredentialPath, dbPasswordPath)
	if err != nil {
		return "", "", err
	}
	localHost := "127.0.0.1"
	localPort := port
	err = pgbouncer.WritePGBouncerConfig(pbCredentialPath, &pgbouncer.PGBouncerConfig{
		LocalDbName: dbc.DBName,
		LocalHost:   &localHost,
		LocalPort:   int16(localPort),
		RemoteHost:  dbc.Host,
		RemotePort:  int16(dbc.Port),
		UserName:    dbc.User,
		Password:    dbc.Password,
	})
	if err != nil {
		return "", "", err
	}
	return *dbc.User, *dbc.Password, nil
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
				log.Println("missing_db_credentials:", path, ", error:", err)
				continue
			}

			stat, err := file.Stat()
			if err != nil {
				log.Println("failed stat file:", err)
				continue
			}

			if !stat.Mode().IsRegular() {
				log.Println("not a regular file")
				continue
			}
			return nil
		}
	}
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

func Start(ctx context.Context, cfg Config) error {

	if cfg.StartUpTime == 0 {
		cfg.StartUpTime = 30 * time.Second
	}

	waitCtx, cancel := context.WithTimeout(ctx, cfg.StartUpTime)
	if err := waitForFiles(waitCtx, cfg.DBCredentialPath, cfg.DBPasswordPath); err != nil {
		return err
	}
	cancel()

	// First time pgbouncer config generation and start
	user, pass, err := parseWritePGBConfig(cfg.DBCredentialPath, cfg.DBPasswordPath, cfg.Port, cfg.PGCredentialPath)
	if err != nil {
		return err
	}

	if err := pgbouncer.Start(cfg.PGBStartScript); err != nil {
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
				newUser, newPass, err := parseWritePGBConfig(cfg.DBCredentialPath, cfg.DBPasswordPath, cfg.Port, cfg.PGCredentialPath)
				if err != nil {
					log.Println("parseWritePGBConfig failed:", err)
					continue
				}

				if cachedCreds.verifyIfNewCreds(newUser, newPass) {

					if err := pgbouncer.ReloadConfiguration(cfg.PGBReloadScript); err != nil {
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
