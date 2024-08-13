package main

import (
	"bytes"
	"errors"
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
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/infobloxopen/db-controller/dbproxy/pgbouncer"
)

type cachedCredentials struct {
	currUser string
	currPass string
}

func (cached *cachedCredentials) verifyIfNewCreds(newUser, newPass string) bool {
	if cached.currPass != newPass || cached.currUser != newUser {
		return true
	}
	return false
}

func (cached *cachedCredentials) setNewCreds(newUser, newPass string) {
	cached.currUser = newUser
	cached.currPass = newPass
}

func generatePGBouncerConfiguration(dbCredentialPath, dbPasswordPath string, port int, pbCredentialPath string) (string, string) {

	bs, err := ioutil.ReadFile(dbCredentialPath)
	if err != nil {
		panic(err)
	}

	cfg, err := pgbouncer.GenerateConfig(string(bs), dbPasswordPath, int16(port))
	if err != nil {
		panic(err)
	}

	err = pgbouncer.WritePGBouncerConfig(pbCredentialPath, &cfg)
	log.Printf("Setup pgbouncerconfig: %s\n", cfg)
	if err != nil {
		log.Println(err)
		panic(err)
	}
	return cfg.UserName, cfg.Password
}

func startPGBouncer() {
	ok, err := pgbouncer.Start()
	if !ok {
		log.Println(err)
		panic(err)
	}
}

func reloadPGBouncerConfiguration() {
	ok, err := pgbouncer.ReloadConfiguration()
	if !ok {
		log.Println(err)
		panic(err)
	}
}

func waitForDbCredentialFile(path string) {
	for {
		time.Sleep(time.Second)

		file, err := os.Open(path)
		if err != nil {
			log.Println("Waiting for file to appear:", path, ", error:", err)
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
		} else {
			break
		}
	}
}

var (
	dbCredentialPath = os.Getenv("DBPROXY_CREDENTIAL")
	dbPasswordPath   = os.Getenv("DBPROXY_PASSWORD")
	pbCredentialPath string
	port             int
)

func init() {
	flag.StringVar(&dbCredentialPath, "dbc", dbCredentialPath, "Location of the DB Credentials")
	flag.StringVar(&dbPasswordPath, "dbp", dbPasswordPath, "Location of the unescaped DB Password")
	flag.StringVar(&pbCredentialPath, "pbc", "./pgbouncer.ini", "Location of the PGBouncer config file")
	flag.IntVar(&port, "port", 5432, "Port to listen on")
}

func main() {

	// Catch signals so helm test can exit cleanly
	catch()

	flag.Parse()

	if port < 1 || port > 65535 {
		log.Fatal("Invalid port number")
	}

	cachedCreds := cachedCredentials{}

	waitForDbCredentialFile(dbCredentialPath)

	waitForDbCredentialFile(dbPasswordPath)

	// First time pgbouncer config generation and start
	user, pass := generatePGBouncerConfiguration(dbCredentialPath, dbPasswordPath, port, pbCredentialPath)
	startPGBouncer()
	cachedCreds.setNewCreds(user, pass)

	// Watch for ongoing changes and regenerate pgbouncer config
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal("NewWatcher failed: ", err)
	}
	defer watcher.Close()

	done := make(chan bool)
	go func() {
		defer close(done)

		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				log.Printf("%s %s\n", event.Name, event.Op)
				err := watcher.Remove(dbCredentialPath)
				if err != nil {
					if !errors.Is(err, fsnotify.ErrNonExistentWatch) {
						log.Fatal("Remove failed:", err)
					}
				}

				waitForDbCredentialFile(dbCredentialPath)
				waitForDbCredentialFile(dbPasswordPath)

				err = watcher.Add(dbCredentialPath)
				if err != nil {
					log.Fatal("Add failed:", err)
				}
				// Regenerate pgbouncer configuration and signal pgbouncer to reload cconfiguration
				newUser, newPass := generatePGBouncerConfiguration(dbCredentialPath, dbPasswordPath, port, pbCredentialPath)
				if cachedCreds.verifyIfNewCreds(newUser, newPass) {
					reloadPGBouncerConfiguration()
					cachedCreds.setNewCreds(newUser, newPass)
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Println("error:", err)
			}
		}

	}()

	err = watcher.Add(dbCredentialPath)
	if err != nil {
		log.Fatal("Add failed:", err)
	}
	<-done
}

func catch() {

	sigs := make(chan os.Signal, 1)

	// Notify the channel on SIGINT (Ctrl+C) or SIGTERM (kill command)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL)

	// Goroutine to handle the signal
	go func() {
		sig := <-sigs
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
		fmt.Println("terminating pgbouncer pid: %d", pid)
		// Terminate pgbouncer
		cmd := exec.Command("sh", "-c", fmt.Sprintf("kill -s 9 %d", pid))
		stdoutStderr, err := cmd.CombinedOutput()
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(stdoutStderr)
		os.Exit(0)
	}()
}
