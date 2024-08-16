package pgbouncer

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"

	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"go.uber.org/zap"
)

var logger logr.Logger

func init() {
	cfg := zap.NewProductionConfig()
	// Disable stack traces in this package
	cfg.EncoderConfig.StacktraceKey = ""

	zapLog, _ := cfg.Build()
	logger = zapr.NewLogger(zapLog)
}

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

// GenerateConfig generates a pgbouncer config from a dsn
// The Port is the local port to bind to
// passwordPath is used with old style DSN as db-controller
// tends not to update the password in it
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
client_tls_sslmode = disable
client_tls_key_file=dbproxy-client.key
client_tls_cert_file=dbproxy-client.crt
server_tls_sslmode = {{.SSLMode}}
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

	authPath := filepath.Join(filepath.Dir(path), "userlist.txt")
	userFile, err := os.OpenFile(authPath, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0644)
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

func ReloadConfiguration(ctx context.Context, scriptPath string) error {
	log.Println("Reloading PG Bouncer config")
	err := run(ctx, scriptPath, logger)
	return err
}

func Start(ctx context.Context, scriptPath string) error {
	log.Println("Starting PG Bouncer:", scriptPath)
	err := run(ctx, scriptPath, logger)
	return err
}

func run(ctx context.Context, scriptPath string, logger logr.Logger) error {
	cmd := exec.CommandContext(ctx, scriptPath)

	// Create pipes for stdout and stderr
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		return err
	}

	// Function to stream and log output
	streamAndLog := func(pipe io.ReadCloser, logFunc func(string, ...interface{})) {
		scanner := bufio.NewScanner(pipe)
		for scanner.Scan() {
			logFunc(scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			logger.Error(err, "Error reading output")
		}
	}

	// Stream stdout and stderr concurrently
	go streamAndLog(stdoutPipe, func(msg string, args ...interface{}) {
		logger.Info(msg, args...)
	})
	go streamAndLog(stderrPipe, func(msg string, args ...interface{}) {
		logger.Error(nil, msg, args...)
	})

	// Wait for the command to finish
	err = cmd.Wait()

	return err
}
