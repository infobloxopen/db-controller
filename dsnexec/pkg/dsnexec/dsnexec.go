package dsnexec

import (
	"bytes"
	"database/sql"
	"fmt"
	"math/bits"
	"strconv"
	"strings"
	"sync"
	"text/template"

	"github.com/Masterminds/sprig/v3"
	_ "github.com/infobloxopen/db-controller/dsnexec/pkg/shelldb"
	_ "github.com/lib/pq"
)

// Hanlder is an instance of dsnexec.
type Handler struct {
	config Config
	l      sync.RWMutex
}

// HandlerOption is an option for the dsnexec handler.
type HandlerOption func(*Handler) error

// WithConfig sets the config for the dnsexec handler.
func WithConfig(c Config) HandlerOption {
	return func(w *Handler) error {
		w.config = c
		return nil
	}
}

// NewHanlder creates a new dsnexec handler.
func NewHanlder(options ...HandlerOption) (*Handler, error) {
	w := &Handler{}
	for _, option := range options {
		if err := option(w); err != nil {
			return nil, err
		}
	}
	if err := w.config.Validate(); err != nil {
		return nil, err
	}
	return w, nil
}

// Exec executes the commands in the config given the current state of the sources.
func (w *Handler) Exec() error {
	w.l.RLock()
	defer w.l.RUnlock()
	return w.exec()
}

// UpdateDSN updates the dsn for a source.
func (w *Handler) UpdateDSN(path, content string) error {
	w.l.RLock()
	defer w.l.RUnlock()

	if err := w.exec(); err != nil {
		return fmt.Errorf("failed initial execute: %v", err)
	}
	return nil
}

func (w *Handler) exec() error {

	// build the arg context
	argContext := make(map[string]interface{})

	for name, source := range w.config.Sources {
		parse, found := parsers[source.Driver]
		if !found {
			return fmt.Errorf("unsupported source driver: %s", source.Driver)
		}

		parsedOpts, err := parse(source.DSN)
		if err != nil {
			return fmt.Errorf("failed to parse dsn: %v", err)
		}
		parsedOpts["raw_dsn"] = source.DSN
		argContext[name] = parsedOpts
	}
	db, err := sql.Open(w.config.Destination.Driver, w.config.Destination.DSN)
	if err != nil {
		return fmt.Errorf("failed to open destination database: %v", err)
	}
	defer db.Close()

	for i, v := range w.config.Commands {
		if len(v.Args) == 0 {
			if _, err := db.Exec(v.Command); err != nil {
				return fmt.Errorf("failed to execute sql: %v", err)
			}
			continue
		}
		var args []interface{}
		for j, arg := range v.Args {
			t, err := template.New(fmt.Sprintf("arg(%d, %d)", i, j)).Funcs(sprig.FuncMap()).Parse(arg)
			if err != nil {
				return fmt.Errorf("failed to parse argument template %v: %v", arg, err)
			}
			bs := bytes.NewBuffer(nil)
			if err := t.Execute(bs, argContext); err != nil {
				return fmt.Errorf("failed to render argument template: %v: %v", arg, err)
			}
			val, err := cast(bs.String())
			if err != nil {
				return fmt.Errorf("failt to cast argument: %v: %v", bs.String(), err)
			}
			args = append(args, val)
		}
		if _, err := db.Exec(v.Command, args...); err != nil {
			return fmt.Errorf("failed to execute command: command %s %v", v.Command, err)
		}
	}
	return nil
}

// cast converts the given string to the prefixed type. A intXX: prefix
// will be converted to the given int type. If no prefix is given, the
// string is returned as is.
func cast(s string) (interface{}, error) {
	switch {
	case strings.HasPrefix(s, "int64:"):
		i, err := strconv.ParseInt(s[6:], 10, 64)
		if err != nil {
			return nil, err
		}
		return int64(i), nil
	case strings.HasPrefix(s, "int32:"):
		i, err := strconv.ParseInt(s[6:], 10, 32)
		if err != nil {
			return nil, err
		}
		return int32(i), nil
	case strings.HasPrefix(s, "int16:"):
		i, err := strconv.ParseInt(s[6:], 10, 16)
		if err != nil {
			return nil, err
		}
		return int16(i), nil
	case strings.HasPrefix(s, "int8:"):
		i, err := strconv.ParseInt(s[5:], 10, 8)
		if err != nil {
			return nil, err
		}
		return int8(i), nil

	case strings.HasPrefix(s, "int:"):
		i, err := strconv.ParseInt(s[4:], 10, bits.UintSize)
		if err != nil {
			return nil, err
		}
		return int(i), nil

	}
	return s, nil
}
