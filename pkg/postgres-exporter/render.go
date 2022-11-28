package exporter

import (
	"bytes"
	"context"
	"text/template"
	"io/ioutil"
	"strings"

	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
	ConfigCheckSum       string
	ServiceAccountName   string
	ImagePullSecrets     []string
	constantLabels       map[string]string
	AppendConstantLabels map[string]string

	DatasourceUser       string
	DatasourceSecretName string

	Resources map[string]map[string]string

	DepYamlPath    string
	ConfigYamlPath string

	Values map[string]interface{}
}

var DefaultConfig = Config{
	Release:   "dbclaim-exporter",
	ImageRepo: "quay.io/prometheuscommunity/postgres-exporter",
	ImageTag:  "v0.10.1",

	Resources: map[string]map[string]string{
		"requests": {
			"cpu":    "100m",
			"memory": "128Mi",
		},
		"limits": {
			"cpu":    "100m",
			"memory": "128Mi",
		},
	},

	Values: map[string]interface{}{},
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
func Apply(ctx context.Context, cli client.Writer, cfg *Config) error {

	yaml, err := Render(ctx, cfg)
	if err != nil {
		return err
	}

	var dep appsv1.Deployment
	if err := unmarshal([]byte(yaml), &dep); err != nil {
		return err
	}

	return cli.Create(ctx, &dep)
}

func unmarshal(bs []byte, dep *appsv1.Deployment) error {
	return yaml.Unmarshal(bs, dep)
}

// Render template based on config available
func Render(ctx context.Context, cfg *Config) (string, error) {

	if err := prepare(cfg); err != nil {
		return "", err
	}

	var (
		bs  []byte
		err error
	)

	bs, err = ioutil.ReadFile(cfg.DepYamlPath)
	if err != nil {
		return "", err
	}
	rendered, err := render(ctx, cfg, string(bs))
	if err != nil {
		return "", err
	}

	bs, err = ioutil.ReadFile(cfg.ConfigYamlPath)
	if err != nil {
		return "", err
	}
	rendered, err = render(ctx, cfg, string(bs))

	_ = rendered
	return "", err
}

func render(ctx context.Context, cfg *Config, yaml string) (string, error) {

	cfgTpl, err := template.New("base").Funcs(funcMap()).Parse(yaml)
	if err != nil {
		return "", err
	}

	buf := bytes.Buffer{}
	err = cfgTpl.Execute(&buf, cfg)
	return strings.TrimSpace(buf.String()), err
}
