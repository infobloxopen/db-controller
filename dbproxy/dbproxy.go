package main

import (
	"errors"
	"flag"
	"log"
	"os"
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
	dbc, err := pgbouncer.ParseDBCredentials(dbCredentialPath, dbPasswordPath)
	if err != nil {
		log.Println(err)
		panic(err)
	}
	localHost := "127.0.0.1"
	localPort := port
	err = pgbouncer.WritePGBouncerConfig(pbCredentialPath, &pgbouncer.PGBouncerConfig{
		LocalDbName: dbc.DBName, LocalHost: &localHost, LocalPort: int16(localPort),
		RemoteHost: dbc.Host, RemotePort: int16(dbc.Port), UserName: dbc.User, Password: dbc.Password})
	if err != nil {
		log.Println(err)
		panic(err)
	}
	return *dbc.User, *dbc.Password
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
	dbCredentialPath string
	dbPasswordPath   string
	pbCredentialPath string
	port             int
)

func init() {
	flag.StringVar(&dbCredentialPath, "dbc", "./db-credential", "Location of the DB Credentials")
	flag.StringVar(&dbPasswordPath, "dbp", "./db-password", "Location of the unescaped DB Password")
	flag.StringVar(&pbCredentialPath, "pbc", "./pgbouncer.ini", "Location of the PGBouncer config file")
	flag.IntVar(&port, "port", 5432, "Port to listen on")
}

func main() {

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
