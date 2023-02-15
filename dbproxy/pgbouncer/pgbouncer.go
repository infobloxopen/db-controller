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

type DBCredential struct {
	Host     string
	Port     int
	DBName   string
	User     string
	Password string
}

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
	errHostNotFound     = errors.New("value not found in db credential")
	errPortNotFound     = errors.New("port value not found in db credential")
	errDbNameNotFound   = errors.New("dbname value not found in db credential")
	errUserNotFound     = errors.New("user value not found in db credential")
	errPasswordNotFound = errors.New("password value not found in db credential")
)

func ParseDBCredentials(path string) (*DBCredential, error) {
	var dbc DBCredential
	content, err := ioutil.ReadFile(path)

	if err != nil {
		return nil, err
	}

	if strings.Contains(string(content), "://") {
		u, err := url.Parse(string(content))
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

		fields := strings.Split(string(content), "' ")

		f := func(c rune) bool {
			return (c == '=')
		}

		m := make(map[string]string)

		for _, field := range fields {
			fieldPair := strings.FieldsFunc(field, f)
			fieldPair = strings.SplitN(field, "=", 2)
			if len(fieldPair) == 2 {
				m[fieldPair[0]] = strings.Trim(fieldPair[1], `'`)
			} else if len(fieldPair) == 1 {
				m[fieldPair[0]] = ""
			}
		}
		dbc.Host = m["host"]
		if m["port"] != "" {
			dbc.Port, err = strconv.Atoi(m["port"])
			if err != nil {
				return nil, fmt.Errorf("invalid port number: %s", m["port"])
			}
		}
		dbc.DBName = m["dbmane"]
		dbc.User = m["user"]
		dbc.Password = m["password"]
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

func Start() (ok bool, err error) {
	fmt.Println("Starting PG Bouncer ...")

	ok = true

	cmd := exec.Command("/var/run/dbproxy/start-pgbouncer.sh")

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
