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

type PGBouncerConfig struct {
	LocalDbName string
	LocalHost   string
	LocalPort   int16
	RemoteHost  string
	RemotePort  int16
	UserName    string
	Password    string
	SSLMode     string
}

func (pgb PGBouncerConfig) String() string {
	pass := pgb.Password
	// Redact all but last 4 characters of password
	if len(pass) > 4 {
		pass = "****" + pass[len(pass)-4:]
	}

	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s", pgb.UserName, pass, pgb.RemoteHost, pgb.RemotePort, pgb.LocalDbName)
}

func GenerateConfig(dsn string, passwordPath string, port int16) (PGBouncerConfig, error) {
	cfg, err := parseURI(dsn)
	if err != nil {
		// Fall back to old style dsn
		cfg, err = parseOldDSN(dsn, passwordPath)
		if err != nil {
			return cfg, err
		}
	}
	cfg.LocalHost = "0.0.0.0"
	cfg.LocalPort = port
	return cfg, nil
}

func parseOldDSN(content string, passwordPath string) (PGBouncerConfig, error) {
	var cfg PGBouncerConfig
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
		return cfg, err
	}

	// FIXME: password in dsn never gets updated, read from password key for rotated password
	password := string(passwordContent)
	m["password"] = &password

	if m["host"] == nil {
		return cfg, errors.New("host value not found in db credential")
	}

	if m["port"] == nil {
		return cfg, errors.New("port value not found in db credential")
	}

	if m["dbname"] == nil {
		return cfg, errors.New("dbname value not found in db credential")
	}

	if m["user"] == nil {
		return cfg, errors.New("user value not found in db credential")
	}

	if m["password"] == nil {
		return cfg, errors.New("password value not found in db credential")
	}

	cfg.RemoteHost = *m["host"]
	cfg.UserName = *m["user"]
	remotePort, _ := strconv.Atoi(*m["port"])
	cfg.RemotePort = int16(remotePort)
	cfg.Password = *m["password"]
	cfg.LocalDbName = *m["dbname"]
	cfg.SSLMode = *m["sslmode"]

	return cfg, nil
}

func parseURI(dsn string) (PGBouncerConfig, error) {
	c := PGBouncerConfig{}

	u, err := url.Parse(dsn)
	if err != nil {
		return c, err
	}

	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return c, fmt.Errorf("invalid_scheme: %s", u.Scheme)
	}

	c.RemoteHost = u.Hostname()
	c.UserName = u.User.Username()
	remotePort, err := strconv.Atoi(u.Port())
	if err != nil {
		return c, err
	}
	c.RemotePort = int16(remotePort)
	c.Password, _ = u.User.Password()
	if u.Path != "" {
		c.LocalDbName = u.Path[1:]
	}

	q := u.Query()
	c.SSLMode = q.Get("sslmode")

	return c, nil
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
