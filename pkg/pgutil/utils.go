package pgutil

import (
	"bufio"
	"database/sql"
	"fmt"
	"io"
)

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

func TestDBConnection(dsn string, db *sql.DB) (*sql.DB, error) {

	var err error
	if db != nil {
		if err = db.Ping(); err == nil {
			return db, nil
		}
	}
	if db, err = sql.Open("postgres", dsn); err != nil {
		return nil, err
	}

	return db, nil
}
