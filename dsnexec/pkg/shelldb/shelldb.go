package shelldb

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"os/exec"
)

type d struct {
}

func init() {
	sql.Register("shell", &d{})
}

func (d *d) Open(name string) (driver.Conn, error) {

	executable, err := exec.LookPath(name)
	if err != nil {
		return nil, err
	}
	return &conn{
		command: executable,
	}, nil
}

type conn struct {
	command string
}

type ExecArgs struct {
	Query string
	Args  []driver.Value
}

func (c *conn) Exec(query string, args []driver.Value) (driver.Result, error) {
	return c.ExecContext(context.Background(), query, args)
}

func (c *conn) ExecContext(ctx context.Context, query string, args []driver.Value) (driver.Result, error) {

	var cmd *exec.Cmd
	if len(args) == 0 {
		cmd = exec.CommandContext(ctx, c.command, query)
	} else {
		var argsStr []string
		for _, v := range args {
			argsStr = append(argsStr, fmt.Sprintf("%s", v))
		}
		cmd = exec.Command(c.command, argsStr...)
	}

	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return &result{}, nil
}

type result struct{}

func (r *result) LastInsertId() (int64, error) {
	return 0, fmt.Errorf("unsupported LastInsertId in shell driver")
}

func (r *result) RowsAffected() (int64, error) {
	return 0, fmt.Errorf("unsupported RowsAffected in shell driver")
}

func (c *conn) Prepare(query string) (driver.Stmt, error) {
	return nil, fmt.Errorf("unsupported Prepare in shell driver")
}

func (c *conn) Begin() (driver.Tx, error) {
	return nil, fmt.Errorf("unsupported Begin in shell driver")
}

func (c *conn) Close() error {
	return nil
}
