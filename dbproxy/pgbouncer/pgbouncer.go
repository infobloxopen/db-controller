package pgbouncer

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"text/template"
)

type DBCredential struct {
	Host     *string
	Port     int
	DBName   *string
	User     *string
	Password *string
}

type PGBouncerConfig struct {
	LocalDbName *string
	LocalHost   *string
	LocalPort   int16
	RemoteHost  *string
	RemotePort  int16
	UserName    *string
	Password    *string
}

func ParseDBCredentials(path string, passwordPath string) (*DBCredential, error) {
	content, err := ioutil.ReadFile(path)

	if err != nil {
		return nil, err
	}

	fields := strings.Split(string(content), " ")

	f := func(c rune) bool {
		return (c == '=')
	}

	m := make(map[string]*string)

	for _, field := range fields {
		fieldPair := strings.FieldsFunc(field, f)
		fieldPair = strings.SplitN(field, "=", 2)
		if len(fieldPair) == 2 {
			unquotedString := strings.Trim(fieldPair[1], `'`)
			m[fieldPair[0]] = &unquotedString
		} else if len(fieldPair) == 1 {
			m[fieldPair[0]] = nil
		}
	}

	passwordContent, err := ioutil.ReadFile(passwordPath)
	if err != nil {
		return nil, err
	}

	// override password from db credential file with unescaped password
	password := string(passwordContent)
	m["password"] = &password

	if m["host"] == nil {
		return nil, errors.New("host value not found in db credential")
	}

	if m["port"] == nil {
		return nil, errors.New("port value not found in db credential")
	}

	if m["dbname"] == nil {
		return nil, errors.New("dbname value not found in db credential")
	}

	if m["user"] == nil {
		return nil, errors.New("user value not found in db credential")
	}

	if m["password"] == nil {
		return nil, errors.New("password value not found in db credential")
	}

	dbc := DBCredential{}
	dbc.Host = m["host"]
	dbc.Port, _ = strconv.Atoi(*m["port"])
	dbc.DBName = m["dbname"]
	dbc.User = m["user"]
	dbc.Password = m["password"]

	// fmt.Println(*dbc.Host, dbc.Port, *dbc.DBName, *dbc.User, *dbc.Password)

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

	userLine := strconv.Quote(*config.UserName) + " \"" + strings.Replace(*config.Password, "\"", "\"\"", -1) + "\""

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
