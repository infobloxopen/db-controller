package exporter

import (
	"bytes"
	"context"
	"io/ioutil"
	"os"
	"path"
	"runtime"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
)

func TestPrepare(t *testing.T) {
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

func TestRender(t *testing.T) {

	tests := []struct {
		path  string
		cfg   *Config
		eErr  error
		ePath string
	}{
		{
			path:  "deployment.yaml",
			ePath: "output.yaml",
			cfg: &Config{
				Name:                 "testA",
				Namespace:            "nsA",
				Release:              "ReleaseA",
				DBClaimOwnerRef:      "owner-abcd",
				ConfigCheckSum:       "e1134fb",
				ServiceAccountName:   "robot",
				DatasourceUser:       "data",
				DatasourceSecretName: "secret/data",
				Values: map[string]interface{}{
					"podLabels": map[string]string{
						"keyA": "valueA",
					},
					"annotations": map[string]string{
						"annkey": "annvalue",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		f := openFile(t, path.Join(dir, tt.path))
		s, err := render(context.TODO(), tt.cfg, f)
		if err != tt.eErr {
			t.Errorf("got: %s wanted: %s", err, tt.eErr)
		}

		e := openFile(t, path.Join(dir, tt.ePath))

		if s != e {
			t.Errorf("\n   got: %q\nwanted: %q", s, e)
		}

		var dep appsv1.Deployment
		if err := unmarshal([]byte(s), &dep); err != nil {
			t.Fatal(err)
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
