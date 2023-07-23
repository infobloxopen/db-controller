package dsnexec

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/infobloxopen/dsnutil/pg"
	"gopkg.in/yaml.v2"
)

var (
	parsers map[string]func(string) (map[string]string, error)
)

func init() {
	parsers = make(map[string]func(string) (map[string]string, error))
	parsers["postgres"] = parsePostgresDSN
	parsers["json"] = parseJSON
	parsers["yaml"] = parseYAML
	parsers["none"] = parseNone
	parsers[""] = parseNone
}

func parseNone(dsn string) (map[string]string, error) {
	value := make(map[string]string)
	return value, nil
}

func parseJSON(dsn string) (map[string]string, error) {
	var imap map[string]interface{}

	if err := json.Unmarshal([]byte(dsn), &imap); err != nil {
		return nil, err
	}
	value := make(map[string]string)
	for k, v := range imap {
		value[k] = fmt.Sprintf("%s", v)
	}
	return value, nil
}

func parseYAML(dsn string) (map[string]string, error) {
	var imap map[string]interface{}

	if err := yaml.Unmarshal([]byte(dsn), &imap); err != nil {
		return nil, err
	}
	value := make(map[string]string)
	for k, v := range imap {
		value[k] = fmt.Sprintf("%s", v)
	}
	return value, nil
}

// parsePostgresDSN parses a dsn string into a map of options. The DSN can be
// in a URI or key=value format.
func parsePostgresDSN(dsn string) (map[string]string, error) {
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
