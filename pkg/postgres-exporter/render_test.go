package exporter

import (
	"bytes"
	"context"
	"io/ioutil"
	"os"
	"path"
	"runtime"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

var testenv = &envtest.Environment{}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig

	s, err := render(context.TODO(), &cfg, `
annotations:
  {{- toYaml .Values.annotations | nindent 2 }}
resources:
  {{- toYaml .Resources | nindent 2 }}
`)
	if err != nil {
		t.Fatal(err)
	}

	e := `annotations:
  prometheus.io/path: /metrics
  prometheus.io/port: "9187"
  prometheus.io/scrape: "true"
resources:
  limits:
    cpu: 100m
    memory: 128Mi
  requests:
    cpu: 100m
    memory: 128Mi`

	if !strings.EqualFold(s, e) {
		t.Logf("got:   %q\nwanted: %q", s, e)
	}
}

func testReadValues(t *testing.T, data []byte) Values {
	t.Helper()
	vals, err := ReadValues(data)
	if err != nil {
		t.Fatal(err)
	}
	return vals
}

func TestMarshal(t *testing.T) {
	t.Skip("skip marshal")
	tests := []struct {
		path string
	}{
		{
			path: "deployment.yaml",
		},
	}

	for _, tt := range tests {
		var dep appsv1.Deployment
		f := openFile(t, path.Join(dir, tt.path))
		if err := unmarshal([]byte(f), &dep); err != nil {
			t.Fatal(err)
		}
		_ = dep
	}
}

func Test_render(t *testing.T) {

	tests := []struct {
		path  string
		cfg   *Config
		eErr  error
		ePath string
	}{
		{
			path:  "deployment.yaml",
			ePath: "tests/testA.yaml",
			cfg: &Config{
				Name:                 "testA",
				Namespace:            "nsA",
				Release:              "ReleaseA",
				DBClaimOwnerRef:      "888888b8-d7c5-432d-8807-033f89aba33c",
				ConfigCheckSum:       "e1134fb",
				ServiceAccountName:   "robot",
				DatasourceUser:       "data",
				DatasourceSecretName: "secret/data",
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
				Values: testReadValues(t, []byte(`
podLabels:
  "keyA": valueA
annotations:
  annkey: annvalue
  port: "80"
`)),
			},
		},

		{
			path:  "config.yaml",
			ePath: "tests/testB.yaml",
			cfg: &Config{
				Name:                 "testB",
				Namespace:            "nsA",
				Release:              "ReleaseA",
				DBClaimOwnerRef:      "owner-abcd",
				ConfigCheckSum:       "e1134fb",
				ServiceAccountName:   "robot",
				DatasourceUser:       "data",
				DatasourceSecretName: "secret/data",
				Values: testReadValues(t, []byte(`
podLabels:
  keyA: valueA
annotations:
  annkey: annvalue`)),
			},
		},
	}

	for _, tt := range tests {
		f := openFile(t, path.Join(dir, tt.path))
		s, err := render(context.TODO(), tt.cfg, f)
		if err != tt.eErr {
			t.Fatalf("got: %s wanted: %s", err, tt.eErr)
		}

		e := openFile(t, path.Join(dir, tt.ePath))

		if s != e {
			ioutil.WriteFile(tt.cfg.Name, []byte(s), 0600)
			t.Errorf("got:\n%q\nwanted:\n%q", s, e)
		}

		var dep appsv1.Deployment
		if err := unmarshal([]byte(s), &dep); err != nil {
			t.Log(err)
			t.Log(s)
		}
	}
}

func TestRender(t *testing.T) {

	tests := []struct {
		cfg               *Config
		eErr              error
		applyErr          error
		eDepPath, eCMPath string
	}{
		{
			eDepPath: "tests/RenderA-dep.yaml",
			eCMPath:  "tests/RenderA-cm.yaml",
			cfg: &Config{
				Name:                 "RenderA",
				Namespace:            "nsA",
				Release:              "ReleaseA",
				DBClaimOwnerRef:      "owner-abcd",
				ConfigCheckSum:       "e1134fb",
				ServiceAccountName:   "robot",
				DatasourceUser:       "data",
				DatasourceSecretName: "secret/data",
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
				DepYamlPath:    "deployment.yaml",
				ConfigYamlPath: "config.yaml",
				Values: testReadValues(t, []byte(`
podLabels:
  keyA: valueA
annotations:
  annkey: annvalue
  port: "80"
`)),
			},
		},
	}

	for _, tt := range tests {
		d, cm, err := Render(context.TODO(), tt.cfg)
		if err != tt.eErr {
			t.Errorf("got: %s wanted: %s", err, tt.eErr)
		}

		e := openFile(t, path.Join(dir, tt.eDepPath))
		if !strings.EqualFold(d, e) {
			t.Errorf("got:\n%s\nwanted:\n%s", d, e)
			ioutil.WriteFile(tt.cfg.Name+"-dep.yaml", []byte(d), 0600)
			continue
		}

		e = openFile(t, path.Join(dir, tt.eCMPath))
		if !strings.EqualFold(cm, e) {
			t.Errorf("got:\n%q\nwanted:\n%q", cm, e)
			t.Log(len(cm), len(e))
			ioutil.WriteFile(tt.cfg.Name+"-cm.yaml", []byte(cm), 0600)
			continue
		}

		var (
			dep    appsv1.Deployment
			cfgMap corev1.ConfigMap
		)
		err = apply(context.TODO(), tt.cfg, &dep, &cfgMap)
		if err != tt.applyErr {
			t.Errorf("got: %s wanted: %s", err, tt.applyErr)
			continue
		}
	}
}

func openFile(t *testing.T, path string) string {
	bs, err := ioutil.ReadFile(path)
	if err != nil {
		t.Fatal(err.Error())
	}
	return string(bytes.TrimSpace(bs))
}

var dir string

func init() {
	_, filename, _, _ := runtime.Caller(0)
	dir = path.Dir(filename)
	err := os.Chdir(dir)
	if err != nil {
		panic(err)
	}
}
