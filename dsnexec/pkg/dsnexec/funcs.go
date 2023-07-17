package dsnexec

import (
	"strings"

	"github.com/infobloxopen/dsnutil/pg"
)

// parseDSN parses a dsn string into a map of options. The DSN can be
// in a URI or key=value format.
func parseDSN(dsn string) (map[string]string, error) {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		var err error
		dsn, err = pg.ParseURL(dsn)
		if err != nil {
			return nil, err
		}
	}
	pgOptions, err := pg.ParseOpts(dsn)
	if err != nil {
		return nil, err
	}
	return pgOptions, nil
}
