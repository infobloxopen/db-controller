package pgbouncer

import (
	"bytes"
	"crypto/md5"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
)

// PGBouncerConfig represents a pgbouncer configuration.
type PGBouncerConfig struct {
	tmpl       *template.Template
	configPath string
	authPath   string
	dsnPath    string
	// hash is used to track changes to the config
	hash []byte

	LocalDbName string
	LocalHost   string
	LocalPort   string
	RemoteHost  string
	RemotePort  int16
	UserName    string
	Password    string
	SSLMode     string
	RoleName    string
}

// String returns the DSN for the pgbouncer config.
func (pgb *PGBouncerConfig) String() string {
	pass := pgb.Password
	// Redact all but last 4 characters of password
	if len(pass) > 4 {
		pass = "****" + pass[len(pass)-4:]
	}

	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s", pgb.UserName, pass, pgb.RemoteHost, pgb.RemotePort, pgb.LocalDbName)
}

// Params configures how to create a config for pgbouncer
type Params struct {
	LocalAddr string
	DSNPath   string
	OutPath   string
}

// NewConfig generates a pgbouncer config from a dsn
// The Port is the local port to bind to
// passwordPath is used with old style DSN as db-controller
// tends not to update the password in it
func NewConfig(p Params) (*PGBouncerConfig, error) {
	if p.DSNPath == "" {
		return nil, errors.New("dsn path is required")
	}
	cfg := PGBouncerConfig{
		dsnPath: p.DSNPath,
	}

	t, err := template.New("pgbouncer").Parse(templateText)
	if err != nil {
		return nil, err
	}
	cfg.tmpl = t

	host, port, err := net.SplitHostPort(p.LocalAddr)
	if err != nil {
		return nil, err
	}

	cfg.LocalHost = host
	cfg.LocalPort = port

	cfg.configPath = p.OutPath
	cfg.authPath = filepath.Join(filepath.Dir(p.OutPath), "userlist.txt")

	return &cfg, nil
}

func parseURI(c *PGBouncerConfig, dsn string) error {

	u, err := url.Parse(dsn)
	if err != nil {
		return err
	}

	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return fmt.Errorf("invalid_scheme: %s", u.Scheme)
	}

	// Ensure this maps to dbuser.SuffixA dbuser.SuffixB
	c.RoleName = strings.TrimSuffix(strings.TrimSuffix(u.User.Username(), "_a"), "_b")
	c.RemoteHost = u.Hostname()
	c.UserName = u.User.Username()
	remotePort, err := strconv.Atoi(u.Port())
	if err != nil {
		return err
	}
	c.RemotePort = int16(remotePort)
	c.Password, _ = u.User.Password()
	if u.Path != "" {
		c.LocalDbName = u.Path[1:]
	}

	q := u.Query()
	c.SSLMode = q.Get("sslmode")

	return nil
}

// As this is a local node connection, we will support non-SSL connections by using
// the default client SSLMode of prefer. This will allow the client to connect to
// the server using SSL if the server supports it, otherwise it will connect without SSL.
// https://www.postgresql.org/docs/current/libpq-ssl.html#LIBPQ-SSL-SSLMODE-STATEMENTS

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
client_tls_sslmode = prefer
client_tls_key_file=dbproxy-client.key
client_tls_cert_file=dbproxy-client.crt
server_tls_sslmode = {{.SSLMode}}
server_reset_query = SET ROLE {{.RoleName}}
#server_tls_key_file=dbproxy-server.key
#server_tls_cert_file=dbproxy-server.crt
`

// Write writes the pgbouncer config to the filesystem
func (config *PGBouncerConfig) Write() error {

	bs, err := ioutil.ReadFile(config.dsnPath)
	if err != nil {
		return fmt.Errorf("failed to read database dsn file: %w", err)
	}

	if err := parseURI(config, string(bs)); err != nil {
		return err
	}

	configFile, err := os.OpenFile(config.configPath, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer configFile.Close()

	authFile, err := os.OpenFile(config.authPath, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer authFile.Close()

	m := md5.New()
	if err := config.tmpl.Execute(m, *config); err != nil {
		return err
	}

	userBS := []byte(strconv.Quote(config.UserName) + " \"" + strings.Replace(config.Password, "\"", "\"\"", -1) + "\"")

	sum := m.Sum(userBS)

	if len(sum) == 0 {
		return errors.New("empty config")
	}

	// Catch file events that result in the same config
	if bytes.Compare(sum, config.hash) == 0 {
		return ErrDuplicateWrite
	}
	config.hash = sum

	err = config.tmpl.Execute(configFile, *config)
	if err != nil {
		return err
	}

	_, err = authFile.Write(userBS)
	return err
}

var ErrDuplicateWrite = errors.New("duplicate write")
