package exporter

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"strings"
	"text/template"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

// Config is used to render out the deployment yaml
type Config struct {
	// Default Values
	Name      string
	Namespace string
	ImageRepo string
	ImageTag  string

	// Override Values
	Release      string
	TemplatePath string

	// DBClaimOwnerRef ensures deployment is cleaned up when db claim is deleted
	DBClaimOwnerRef      string
	DBClaimUID           string
	ConfigCheckSum       string
	ServiceAccountName   string
	ImagePullSecrets     []string
	constantLabels       map[string]string
	AppendConstantLabels map[string]string

	DatasourceUser       string
	DatasourceSecretName string
	DatasourceFileName   string

	Resources map[string]map[string]string

	DepYamlPath    string
	ConfigYamlPath string
	Values         Values
}

// Values represents a collection of chart values.
type Values map[string]interface{}

// YAML encodes the Values into a YAML string.
func (v Values) YAML() (string, error) {
	b, err := yaml.Marshal(v)
	return string(b), err
}

// Encode writes serialized Values information to the given io.Writer.
func (v Values) Encode(w io.Writer) error {
	out, err := yaml.Marshal(v)
	if err != nil {
		return err
	}
	_, err = w.Write(out)
	return err
}

func ReadValues(data []byte) (vals Values, err error) {
	err = yaml.Unmarshal(data, &vals)
	if len(vals) == 0 {
		vals = Values{}
	}
	return vals, err
}

func MustReadValues(data []byte) Values {
	vals, err := ReadValues(data)
	if err != nil {
		panic(err)
	}
	return vals
}

var DefaultConfig = Config{

	Release:            "dbclaim-exporter",
	ImageRepo:          "quay.io/prometheuscommunity/postgres-exporter",
	ImageTag:           "v0.10.1",
	DatasourceFileName: "dsn.txt",

	Resources: map[string]map[string]string{
		"requests": {
			"cpu":    "100m",
			"memory": "128Mi",
		},
		"limits": {
			"cpu":    "500m",
			"memory": "512Mi",
		},
	},
	Values: MustReadValues([]byte(`annotations:
  "prometheus.io/scrape": "true"
  "prometheus.io/path": "/metrics"
  "prometheus.io/port": "9187"
`)),
}

// NewConfig returns a config with default values populated
func NewConfig() *Config {
	cfg := DefaultConfig
	return &cfg
}

func prepare(cfg *Config) error {
	cfg.constantLabels = map[string]string{
		"dbclaim": cfg.Name,
	}
	for k, v := range cfg.AppendConstantLabels {
		cfg.constantLabels[k] = v
	}

	return nil
}

// Apply provided config to cluster
func Apply(ctx context.Context, cli client.Client, cfg *Config) error {

	var dep appsv1.Deployment
	var cm corev1.ConfigMap
	err := apply(ctx, cfg, &dep, &cm)
	if err != nil {
		return err
	}

	req := client.ObjectKeyFromObject(&dep)
	var existing appsv1.Deployment
	if err := cli.Get(ctx, req, &existing); err != nil {

	}

	if err := client.IgnoreAlreadyExists(cli.Create(ctx, &dep)); err != nil {
		return err
	}
	return client.IgnoreAlreadyExists(cli.Create(ctx, &cm))
}

// Apply provided config to cluster
func apply(ctx context.Context, cfg *Config, dep *appsv1.Deployment, cm *corev1.ConfigMap) error {

	depYaml, cfgYaml, err := Render(ctx, cfg)
	if err != nil {
		return err
	}

	if err := unmarshal([]byte(depYaml), dep); err != nil {
		return err
	}
	return unmarshal([]byte(cfgYaml), cm)
}

func unmarshal(bs []byte, inf interface{}) error {
	return yaml.Unmarshal(bs, inf)
}

// Render template based on config available
func Render(ctx context.Context, cfg *Config) (string, string, error) {

	if err := prepare(cfg); err != nil {
		return "", "", err
	}

	var (
		bs  []byte
		err error
	)

	bs, err = ioutil.ReadFile(cfg.DepYamlPath)
	if err != nil {
		return "", "", fmt.Errorf("unable load depYamlPath: %w", err)
	}
	depYaml, err := render(ctx, cfg, string(bs))
	if err != nil {
		return "", "", err
	}

	bs, err = ioutil.ReadFile(cfg.ConfigYamlPath)
	if err != nil {
		return "", "", err
	}
	cfgYaml, err := render(ctx, cfg, string(bs))

	return depYaml, cfgYaml, err
}

func render(ctx context.Context, cfg *Config, yaml string) (string, error) {

	cfgTpl, err := template.New("base").Funcs(funcMap()).Parse(yaml)
	if err != nil {
		return "", err
	}

	// cfg.rawValues, err = cfg.Values.YAML()
	// if err != nil {
	// 	return "", err
	// }
	// fmt.Println("wtf", cfg.rawValues)

	buf := bytes.Buffer{}
	err = cfgTpl.Execute(&buf, cfg)
	return strings.TrimSpace(buf.String()), err
}
