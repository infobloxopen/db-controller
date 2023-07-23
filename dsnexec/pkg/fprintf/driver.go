package fprintf

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"net/url"

	log "github.com/sirupsen/logrus"
)

type d struct {
}

func init() {
	sql.Register("fprintf", &d{})
}

func (d *d) Open(name string) (driver.Conn, error) {
	uri, err := url.Parse(name)
	if err != nil {
		return nil, err
	}
	fileHandler, ok := fileHandlers[uri.Scheme]
	if !ok {
		return nil, fmt.Errorf("fprintf: unsupported file handler %s", uri.Scheme)
	}
	templater, ok := templaters[uri.Host]
	if !ok {
		return nil, fmt.Errorf("fprintf: unsupported templater %s", uri.Scheme)
	}

	return &conn{
		filename:    uri.Path,
		fileHandler: fileHandler,
		templater:   templater,
	}, nil
}

type conn struct {
	filename    string
	fileHandler FileHandler
	templater   Templater
}

func (c *conn) Exec(query string, args []driver.Value) (driver.Result, error) {
	return c.ExecContext(context.Background(), query, args)
}

func (c *conn) ExecContext(ctx context.Context, query string, args []driver.Value) (driver.Result, error) {

	log := log.WithFields(log.Fields{
		"fprintf_destination": c.filename,
	})

	log.Debug("executing query")

	ctx, abort := context.WithCancel(ctx)
	defer abort()

	w, err := newFileHandler(ctx, c.filename)
	if err != nil {
		return nil, err
	}
	var vargs []interface{}
	for i := range args {
		vargs = append(vargs, fmt.Sprintf("%s", args[i]))
	}
	if len(vargs) == 0 {
		if _, err := c.templater(w, query); err != nil {
			return nil, err
		}
	} else {
		if _, err := c.templater(w, query, vargs...); err != nil {
			return nil, err
		}
	}
	if err := w.Close(); err != nil {
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
