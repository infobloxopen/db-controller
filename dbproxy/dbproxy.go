package main

import (
	"flag"
	"log"
	"os"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/infobloxopen/db-controller/dbproxy/pgbouncer"
)

func generatePGBouncerConfiguration(dbCredentialPath, pbCredentialPath string) {
	dbc, err := pgbouncer.ParseDBCredentials(dbCredentialPath)
	if err != nil {
		log.Println(err)
		panic(err)
	}
	err = pgbouncer.WritePGBouncerConfig(pbCredentialPath,
		&pgbouncer.PGBouncerConfig{
			LocalDbName: dbc.DBName,
			LocalHost:   "127.0.0.1",
			LocalPort:   5432,
			RemoteHost:  dbc.Host,
			RemotePort:  int16(dbc.Port),
			UserName:    dbc.User,
			Password:    dbc.Password})
	if err != nil {
		log.Println(err)
		panic(err)
	}
}

func startPGBouncer() {
	if err := pgbouncer.Start(); err != nil {
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
	pbCredentialPath string
)

func init() {
	flag.StringVar(&dbCredentialPath, "dbc", "./db-credential", "Location of the DB Credentials")
	flag.StringVar(&pbCredentialPath, "pbc", "./pgbouncer.ini", "Location of the PGBouncer config file")
}

func main() {

	flag.Parse()

	waitForDbCredentialFile(dbCredentialPath)

	// First time pgbouncer config generation and start
	generatePGBouncerConfiguration(dbCredentialPath, pbCredentialPath)
	startPGBouncer()

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
					log.Fatal("Remove failed:", err)
				}
				err = watcher.Add(dbCredentialPath)
				if err != nil {
					log.Fatal("Add failed:", err)
				}
				// Regenerate pgbouncer configuration and signal pgbouncer to reload cconfiguration
				generatePGBouncerConfiguration(dbCredentialPath, pbCredentialPath)
				reloadPGBouncerConfiguration()
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
