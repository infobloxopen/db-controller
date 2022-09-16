package pgctl

import (
	"bufio"
	"bytes"
	"database/sql"
	"fmt"
	"io"
	"os/exec"

	"github.com/go-logr/logr"
)

func isAdminUser(db *sql.DB) (bool, error) {
	var exists bool

	err := db.QueryRow("SELECT EXISTS(select  rolname from pg_roles where rolsuper = 't' and rolreplication ='t' and rolname = (select session_user))").Scan(&exists)

	if err != nil {
		// pc.log.Error(err, "could not query for Subscription name")
		return false, err
	}
	if !exists {
		// pc.log.Info("creating Subscription:", "with name", subName)
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
		fmt.Println(err)
		outStr, errStr := stdout.String(), stderr.String()

		if exitError, ok := err.(*exec.ExitError); ok {
			fmt.Println(exitError.ExitCode(), errStr)
			return outStr, fmt.Errorf("command %s\nfailed with\n%s", cmd.String(), errStr)
		}
		return outStr, err
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
