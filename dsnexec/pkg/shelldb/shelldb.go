package shelldb

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"os/exec"

	log "github.com/sirupsen/logrus"
)

type d struct {
}

func init() {
	sql.Register("shelldb", &d{})
}

// Open a new connection to the shell driver.
func (d *d) Open(name string) (driver.Conn, error) {
	return &conn{
		name: name,
	}, nil
}

type conn struct {
	name string
}

// Exec a query against the shell driver. The query represents the command to
// run. The args are passed to the command as arguments.
func (c *conn) Exec(query string, args []driver.Value) (driver.Result, error) {
	return c.ExecContext(context.Background(), query, args)
}

type logWriter struct {
	logger *log.Entry
}

func (lw logWriter) Write(p []byte) (n int, err error) {
	lw.logger.Infof("%s", p)
	return len(p), nil
}

// ExecContext a query against the shell driver. The query represents the command to
// run. The args are passed to the command as arguments. The context is used to
// cancel the command if the context is canceled.
func (c *conn) ExecContext(ctx context.Context, query string, args []driver.Value) (driver.Result, error) {

	var cmd *exec.Cmd
	if len(args) == 0 {
		cmd = exec.CommandContext(ctx, query)
	} else {
		var argsStr []string
		for _, v := range args {
			argsStr = append(argsStr, fmt.Sprintf("%s", v))
		}
		cmd = exec.Command(query, argsStr...)
	}

	log := log.WithFields(log.Fields{
		"shelldb_destination": c.name,
	})
	cmd.Stdout = logWriter{logger: log}
	cmd.Stderr = logWriter{logger: log}
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return &result{}, nil
}

type result struct{}

// LastInsertId is not supported by the shell driver.
func (r *result) LastInsertId() (int64, error) {
	return 0, fmt.Errorf("unsupported LastInsertId in shell driver")
}

// RowsAffected is not supported by the shell driver.
func (r *result) RowsAffected() (int64, error) {
	return 0, fmt.Errorf("unsupported RowsAffected in shell driver")
}

// Prepare is not supported by the shell driver.
func (c *conn) Prepare(query string) (driver.Stmt, error) {
	return nil, fmt.Errorf("unsupported Prepare in shell driver")
}

// Begin is not supported by the shell driver.
func (c *conn) Begin() (driver.Tx, error) {
	return nil, fmt.Errorf("unsupported Begin in shell driver")
}

// Close implements the driver.Conn interface. This is a no-op for the shell.
func (c *conn) Close() error {
	return nil
}
