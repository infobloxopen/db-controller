package fprintf

import (
	"fmt"
	"io"
)

var (
	templaters map[string]Templater = make(map[string]Templater)
)

type Templater func(io.Writer, string, ...interface{}) (int, error)

func init() {
	templaters["fprintf"] = fmt.Fprintf
	templaters[""] = fmt.Fprintf
}

// RegisterTemplater a new templater. This will panic if the name is already registered.
func RegisterTemplater(name string, f Templater) {
	if _, ok := templaters[name]; ok {
		panic(fmt.Sprintf("fprintf: %s already registered", name))
	}
	templaters[name] = f
}
