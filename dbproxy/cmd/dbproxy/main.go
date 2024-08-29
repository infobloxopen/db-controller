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

func main() {

	opts := zap.Options{
		Development: false,
	}
	opts.BindFlags(flag.CommandLine)

	flag.Parse()

	dbproxy.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	mgr, err := dbproxy.New(context.TODO(), dbproxy.Config{
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
	log.Fatal(mgr.Start(ctx))
}

func catch(cancel func()) {

	sigs := make(chan os.Signal, 1)

	// Notify the channel on SIGINT (Ctrl+C) or SIGTERM (kill command)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL)

	// Goroutine to handle the signal
	go func() {
		sig := <-sigs
		cancel()
		fmt.Println("Received signal:", sig)
		// Path set in ini file
		bs, err := ioutil.ReadFile(filepath.Join("pgbouncer.pid"))
		if err != nil {
			log.Fatal(err)
		}
		pid, err := strconv.Atoi(string(bytes.TrimSpace(bs)))
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println("terminating pgbouncer pid:", pid)
		// Terminate pgbouncer
		cmd := exec.Command("sh", "-c", fmt.Sprintf("kill -s 9 %d", pid))
		stdoutStderr, err := cmd.CombinedOutput()
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(stdoutStderr)

		// Capture log pgbouncer.log and write to stdout
		cmd = exec.Command("sh", "-c", fmt.Sprintf("cat %s", "pgbouncer.log"))
		stdoutStderr, err = cmd.CombinedOutput()
		if err != nil {
			log.Fatalf("failed to cat log: %s", err)
		}
		log.Println("pgbouncer.log")
		fmt.Println(string(stdoutStderr))

		os.Exit(0)
	}()
}
