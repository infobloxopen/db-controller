package nulldb

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
)

var (
	defaultDriver *d
)

type d struct {
}

func init() {
	defaultDriver = &d{}
	sql.Register("nulldb", defaultDriver)
}

func (d *d) Open(name string) (driver.Conn, error) {
	return nil, fmt.Errorf("unsupported Open in nulldb")
}
