package pgctl

import (
	"bufio"
	"bytes"
	"database/sql"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"time"

	"github.com/go-logr/logr"
)

// IsAdminUser checks if the user is a superuser or has replication role
func IsAdminUser(db *sql.DB) (bool, error) {
	return isAdminUser(db)
}

func isAdminUser(db *sql.DB) (bool, error) {
	var (
		exists bool
		user   string
		count  int
	)

	err := db.QueryRow("SELECT EXISTS(select  rolname from pg_roles where rolsuper = 't' and rolreplication ='t' and rolname = (select session_user))").Scan(&exists)

	if err != nil {
		// pc.log.Error(err, "could not query for Subscription name")
		return false, err
	}
	if exists {
		return true, nil
	}
	//AWS uses a different mechanism to grant admin/replication rules
	_ = db.QueryRow("SELECT session_user").Scan(&user)

	awsAdminQuery := fmt.Sprintf(`
	SELECT count(1)
	FROM pg_roles
	WHERE pg_has_role( '%s', oid, 'member')
  	AND rolname in ( 'alloydbsuperuser', 'alloydbadmin', 'alloydbreplica', 'rds_superuser', 'rds_replication');`, user)

	err = db.QueryRow(awsAdminQuery).Scan(&count)
	if err != nil {
		return false, err
	}
	if count < 2 {
		return false, fmt.Errorf("db user does not have required super_user and/or replication role")
	}

	return true, nil
}

func isLogical(db *sql.DB) (bool, error) {
	var exists bool

	err := db.QueryRow("SELECT EXISTS(select  name from pg_settings where name ='wal_level' and setting = 'logical')").Scan(&exists)

	if err != nil {
		// pc.log.Error(err, "could not query for Subscription name")
		return false, err
	}
	if !exists {
		// pc.log.Info("creating Subscription:", "with name", subName)
		return false, fmt.Errorf("db wal_level not set to logical")
	}

	return true, nil
}

type ExecOptions struct {
	StreamPrint bool
}

func streamExecOutput(out io.ReadCloser, options ExecOptions) string {
	output := ""
	reader := bufio.NewReader(out)
	line, err := reader.ReadString('\n')
	output += line
	for err == nil {
		if options.StreamPrint {
			fmt.Print(line)
		}
		line, err = reader.ReadString('\n')
		output += line
	}
	return output
}

func getDB(dsn string, db *sql.DB) (*sql.DB, error) {

	var err error
	if db == nil {
		if db, err = sql.Open("postgres", dsn); err != nil {
			return nil, err
		}
		db.SetConnMaxIdleTime(time.Minute)
	}
	if err = db.Ping(); err != nil {
		return nil, err
	}

	return db, nil
}

func Exec(name string, arg ...string) (string, error) {
	cmd := exec.Command(name, arg...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		outStr, errStr := stdout.String(), stderr.String()

		return outStr, fmt.Errorf("command_failed %s:\n%s %s", cmd.String(), outStr, errStr)
	}
	return stdout.String(), nil
}

func closeDB(log logr.Logger, db *sql.DB) error {
	if err := db.Close(); err != nil {
		log.Error(err, "db close failed")
		return err
	}
	return nil
}

/*
extract the parent role from db user name by removing the last two characters.
The last two characters are expected to be _a or _b
if rolename name is sample_user_a, the role inherited is sample_user
remove "_a" to get role name.
*/
func getParentRole(dbUser string) string {
	if len(dbUser) > 2 {
		if string(dbUser[len(dbUser)-2]) == "_" {
			return dbUser[:len(dbUser)-2]
		}
	}
	return dbUser
}

func SanitizeDSN(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return ""
	}
	return u.Redacted()
}
