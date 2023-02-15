package pgbouncer

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"text/template"
)

// DBCredential represents the information parsed from a connection string.
type DBCredential struct {
	Host     string
	Port     int
	DBName   string
	User     string
	Password string
}

// PGBouncerConfig represents the required configuration for pgbouncer.
type PGBouncerConfig struct {
	LocalDbName string
	LocalHost   string
	LocalPort   int16
	RemoteHost  string
	RemotePort  int16
	UserName    string
	Password    string
}

var (
	errHostNotFound     = errors.New("host not found in db credential")
	errPortNotFound     = errors.New("port value not found in db credential")
	errDbNameNotFound   = errors.New("dbname value not found in db credential")
	errUserNotFound     = errors.New("user value not found in db credential")
	errPasswordNotFound = errors.New("password value not found in db credential")
)

// ParseDBCredentials will open the filename and parse the DBCredential info out of it.
func ParseDBCredentials(filename string) (*DBCredential, error) {
	content, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	return parseDBCredentials(string(content))
}

// parseDBCredentials removes the file reading from ParseDBCredentials to make
// it easier to unit test.
func parseDBCredentials(content string) (*DBCredential, error) {

	var dbc DBCredential
	if strings.Contains(content, "://") {
		u, err := url.Parse(content)
		if err != nil {
			return nil, fmt.Errorf("error parsing URL DNS: %s", err)
		}
		dbc.Host = u.Hostname()
		portStr := u.Port()
		if portStr == "" {
			dbc.Port = 5432
		} else {
			var err error
			dbc.Port, err = strconv.Atoi(portStr)
			if err != nil {
				return nil, fmt.Errorf("invalid port number: %s", portStr)
			}
		}
		if u.User != nil {
			dbc.User = u.User.String()
			dbc.Password, _ = u.User.Password()
		}
		dbc.DBName = strings.TrimPrefix(u.Path, "/")
	} else {

		o := make(values)
		o["host"] = "localhost"
		o["port"] = "5432"
		if err := parseOpts(content, o); err != nil {
			return nil, fmt.Errorf("could not parse %s from connection string", err)
		}

		dbc.Host = o["host"]
		if o["port"] != "" {
			var err error
			dbc.Port, err = strconv.Atoi(o["port"])
			if err != nil {
				return nil, fmt.Errorf("invalid port number: %s", o["port"])
			}
		}
		// tolerate older spec connection string
		dbc.DBName = o["dbname"]
		if dbName, found := o["database"]; found {
			dbc.DBName = dbName
		}
		dbc.User = o["user"]
		dbc.Password = o["password"]
	}

	if dbc.Host == "" {
		return nil, errHostNotFound
	}

	if dbc.Port == 0 {
		return nil, errPortNotFound
	}

	if dbc.DBName == "" {
		return nil, errDbNameNotFound
	}

	if dbc.User == "" {
		return nil, errUserNotFound
	}

	if dbc.Password == "" {
		return nil, errPasswordNotFound
	}

	return &dbc, nil
}

// WritePGBouncerConfig will write out the config at the given path.
func WritePGBouncerConfig(path string, config *PGBouncerConfig) error {
	t, err := template.ParseFiles("./pgbouncer.template")
	if err != nil {
		return err
	}
	if t == nil {
		os.Stderr.WriteString("could not parse pgbouncer config template")
	}

	configFile, err := os.OpenFile(path, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatal(err)
	}

	defer configFile.Close()

	userFile, err := os.OpenFile("userlist.txt", os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatal(err)
	}

	defer userFile.Close()

	err = t.Execute(configFile, *config)
	if err != nil {
		return err
	}

	userLine := strconv.Quote(config.UserName) + " \"" + strings.Replace(config.Password, "\"", "\"\"", -1) + "\""

	userFile.Write([]byte(userLine))

	return (nil)
}

// ReloadConfiguration will send a signal to pgbouncer to make it re-read its configuration.
func ReloadConfiguration() (ok bool, err error) {
	fmt.Println("Reloading PG Bouncer config")

	ok = true

	cmd := exec.Command("/var/run/dbproxy/reload-pgbouncer.sh")

	var stdOut, stdErr bytes.Buffer

	cmd.Stdout = &stdOut
	cmd.Stderr = &stdErr

	err = cmd.Run()
	if err != nil {
		ok = false
	}

	log.Println(stdOut.String())
	log.Println(stdErr.String())

	return ok, err
}

// Start will start pgbouncer.
func Start() error {
	fmt.Println("Starting PG Bouncer ...")

	cmd := exec.Command("/var/run/dbproxy/start-pgbouncer.sh")

	var stdOut, stdErr bytes.Buffer

	cmd.Stdout = &stdOut
	cmd.Stderr = &stdErr

	err := cmd.Run()
	log.Println(stdOut.String())
	log.Println(stdErr.String())

	return err
}
