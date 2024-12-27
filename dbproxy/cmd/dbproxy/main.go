package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/go-logr/logr"
	"github.com/infobloxopen/db-controller/dbproxy"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	dbCredentialPath   = os.Getenv("DBPROXY_CREDENTIAL")
	pbCredentialPath   string
	pgbStartScriptPath string
	pgbReloadScript    string
	addr               string
)

func init() {
	flag.StringVar(&dbCredentialPath, "dbc", dbCredentialPath, "Location of the DB Credentials")
	flag.StringVar(&pbCredentialPath, "pbc", "./pgbouncer.ini", "Location of the PGBouncer config file")

	flag.StringVar(&pgbStartScriptPath, "pgbouncer-start-script", "/var/run/dbproxy/start-pgbouncer.sh", "Location of the PGBouncer start script")
	flag.StringVar(&pgbReloadScript, "pgbouncer-reload-script", "/var/run/dbproxy/reload-pgbouncer.sh", "Location of the PGBouncer reload script")
	flag.StringVar(&addr, "addr", "0.0.0.0:5432", "address to listen to clients on")
}

var logger logr.Logger

func main() {

	opts := zap.Options{
		Development: false,
	}
	opts.BindFlags(flag.CommandLine)

	flag.Parse()

	logger = zap.New(zap.UseFlagOptions(&opts))

	mgr, err := dbproxy.New(context.TODO(), logger, dbproxy.Config{
		DBCredentialPath: dbCredentialPath,
		PGCredentialPath: pbCredentialPath,
		PGBStartScript:   pgbStartScriptPath,
		PGBReloadScript:  pgbReloadScript,
		LocalAddr:        addr,
	})

	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	// Catch signals so helm test can exit cleanly
	catch(cancel)

	// Blocking call
	if err := mgr.Start(ctx); err != nil {
		logger.Error(err, "failed to start dbproxy")
	}
}

func catch(cancel func()) {

	sigs := make(chan os.Signal, 1)

	// Notify the channel on SIGINT (Ctrl+C) or SIGTERM (kill command)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL)

	// Goroutine to handle the signal
	go func() {
		sig := <-sigs
		cancel()
		logger.Info("Received signal", "signal", sig)
		// Path set in ini file
		bs, err := ioutil.ReadFile(filepath.Join("pgbouncer.pid"))
		if err != nil {
			logger.Error(err, "failed to read pgbouncer pid file")
			os.Exit(1)
		}
		pid, err := strconv.Atoi(string(bytes.TrimSpace(bs)))
		if err != nil {
			logger.Error(err, "failed to convert pid to int")
			os.Exit(1)
		}

		// Terminate pgbouncer
		logger.Info("terminating pgbouncer pid", "pid", pid)
		cmd := exec.Command("sh", "-c", fmt.Sprintf("kill -s 9 %d", pid))

		stdoutStderr, err := cmd.CombinedOutput()
		if err != nil {
			logger.Error(err, "failed to kill pgbouncer")
		}
		logger.Info("pgbouncer stop executed", "output", stdoutStderr)

		// Capture log pgbouncer.log and write to stdout
		logger.Info("capturing pgbouncer.log")
		cmd = exec.Command("sh", "-c", fmt.Sprintf("cat %s", "pgbouncer.log"))

		stdoutStderr, err = cmd.CombinedOutput()
		if err != nil {
			logger.Error(err, "failed to cat log", "output", string(stdoutStderr))
		} else {
			logger.Info("pgbouncer.log", "output", string(stdoutStderr))
		}

		os.Exit(0)
	}()
}
