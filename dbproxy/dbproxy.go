package main

import (
	"flag"
	"log"
	"os"
	"time"

	"github.com/fsnotify/fsnotify"
	"infoblox.com/dbproxy/pgbouncer" // go.mod has a replace directive to make this relative path work.
)

func generatePGBouncerConfiguration(dbCredentialPath, dbPasswordPath, pbCredentialPath *string) {
	dbc, err := pgbouncer.ParseDBCredentials(dbCredentialPath, dbPasswordPath)
	if err != nil {
		log.Println(err)
		panic(err)
	}
	localHost := "127.0.0.1"
	localPort := 5432
	err = pgbouncer.WritePGBouncerConfig(pbCredentialPath, &pgbouncer.PGBouncerConfig{
		LocalDbName: dbc.DBName, LocalHost: &localHost, LocalPort: int16(localPort),
		RemoteHost: dbc.Host, RemotePort: int16(dbc.Port), UserName: dbc.User, Password: dbc.Password})
	if err != nil {
		log.Println(err)
		panic(err)
	}
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

func waitForDbCredentialFile(path *string) {
	for {
		time.Sleep(time.Second)

		file, err := os.Open(*path)
		if err != nil {
			log.Println("Waiting for file to appear:", *path, ", error:", err)
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

func main() {
	dbCredentialPath := flag.String("dbc", "./db-credential", "Location of the DB Credentials")
	dbPasswordPath := flag.String("dbp", "./db-password", "Location of the unescaped DB Password")
	pbCredentialPath := flag.String("pbc", "./pgbouncer.ini", "Location of the PGBouncer config file")

	flag.Parse()

	waitForDbCredentialFile(dbCredentialPath)

	waitForDbCredentialFile(dbPasswordPath)

	// First time pgbouncer config generation and start
	generatePGBouncerConfiguration(dbCredentialPath, dbPasswordPath, pbCredentialPath)
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
				err := watcher.Remove(*dbCredentialPath)
				if err != nil {
					log.Fatal("Remove failed:", err)
				}
				err = watcher.Add(*dbCredentialPath)
				if err != nil {
					log.Fatal("Add failed:", err)
				}
				// Regenerate pgbouncer configuration and signal pgbouncer to reload cconfiguration
				generatePGBouncerConfiguration(dbCredentialPath, dbPasswordPath, pbCredentialPath)
				reloadPGBouncerConfiguration()
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Println("error:", err)
			}
		}

	}()

	err = watcher.Add(*dbCredentialPath)
	if err != nil {
		log.Fatal("Add failed:", err)
	}
	<-done
}
