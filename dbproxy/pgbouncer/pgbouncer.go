package pgbouncer

import (
	"errors"
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

const templateText = `
[databases]
{{.LocalDbName}} = host={{.RemoteHost}} port={{.RemotePort}} dbname={{.LocalDbName}}

[pgbouncer]
listen_port = {{.LocalPort}}
listen_addr = {{.LocalHost}}
auth_type = trust
auth_file = userlist.txt
logfile = pgbouncer.log
pidfile = pgbouncer.pid
admin_users = {{.UserName}}
remote_user_override = {{.UserName}}
remote_db_override = {{.LocalDbName}}
ignore_startup_parameters = extra_float_digits
client_tls_sslmode = require
client_tls_key_file=dbproxy-client.key
client_tls_cert_file=dbproxy-client.crt
server_tls_sslmode = require
#server_tls_key_file=dbproxy-server.key
#server_tls_cert_file=dbproxy-server.crt
`

func WritePGBouncerConfig(path string, config *PGBouncerConfig) error {
	t, err := template.New("pgbouncer").Parse(templateText)
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

func ReloadConfiguration(scriptPath string) (err error) {
	log.Println("Reloading PG Bouncer config")
	_, err = run(scriptPath)
	return err
}

func Start(scriptPath string) error {
	log.Println("Starting PG Bouncer:", scriptPath)
	_, err := run(scriptPath)
	return err
}

func run(scriptPath string) (string, error) {

	cmd := exec.Command(scriptPath)
	stdoutStderr, err := cmd.CombinedOutput()
	return string(stdoutStderr), err
}
