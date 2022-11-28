package exporter

import (
	"text/template"
	"strings"

	"github.com/Masterminds/sprig"
	"gopkg.in/yaml.v2"
)

func funcMap() template.FuncMap {
	f := sprig.TxtFuncMap()
	delete(f, "env")
	delete(f, "expandenv")

	// Add some extra functionality
	extra := template.FuncMap{
		"toYaml": toYAML,
	}

	for k, v := range extra {
		f[k] = v
	}

	return f
}

// implements toYaml in func
func toYAML(v interface{}) string {
	data, err := yaml.Marshal(v)
	if err != nil {
		// Swallow errors inside of a template.
		return ""
	}
	return strings.TrimSuffix(string(data), "\n")
}
