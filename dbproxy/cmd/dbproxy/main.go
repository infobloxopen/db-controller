package main

import (
	"context"
	"flag"
	"log"

	"github.com/infobloxopen/db-controller/dbproxy"
)

var (
	dbCredentialPath   string
	dbPasswordPath     string
	pbCredentialPath   string
	pgbStartScriptPath string
	pgbReloadScript    string
	port               int
)

func init() {
	flag.StringVar(&dbCredentialPath, "dbc", "./db-credential", "Location of the DB Credentials")
	flag.StringVar(&dbPasswordPath, "dbp", "./db-password", "Location of the unescaped DB Password")
	flag.StringVar(&pbCredentialPath, "pbc", "./pgbouncer.ini", "Location of the PGBouncer config file")
	flag.StringVar(&pgbStartScriptPath, "pgbouncer-start-script", "/var/run/dbproxy/start-pgbouncer.sh", "Location of the PGBouncer start script")
	flag.StringVar(&pgbReloadScript, "pgbouncer-reload-script", "/var/run/dbproxy/reload-pgbouncer.sh", "Location of the PGBouncer reload script")
	flag.IntVar(&port, "port", 5432, "Port to listen on")
}

func main() {
	flag.Parse()

	if port < 1 || port > 65535 {
		log.Fatal("Invalid port number")
	}

	err := dbproxy.Start(context.TODO(), dbproxy.Config{
		DBCredentialPath: dbCredentialPath,
		DBPasswordPath:   dbPasswordPath,
		PGCredentialPath: pbCredentialPath,
		PGBStartScript:   pgbStartScriptPath,
		PGBReloadScript:  pgbReloadScript,
		Port:             port,
	})

	if err != nil {
		log.Fatal(err)
	}
}
