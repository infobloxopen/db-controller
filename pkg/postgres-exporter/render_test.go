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
)

var testDataDir string

func init() {
	// Set up the test directory based on the current file's location
	_, filename, _, _ := runtime.Caller(0)
	testDataDir = path.Dir(filename)
	err := os.Chdir(testDataDir)
	if err != nil {
		panic(err)
	}
}

func TestRenderDefaultConfigAnnotations(t *testing.T) {
	template := `annotations:
  {{- toYaml .Values.annotations | nindent 2 }}`

	expected := `annotations:
  prometheus.io/path: /metrics
  prometheus.io/port: "9187"
  prometheus.io/scrape: "true"`

	rendered, err := render(context.Background(), &DefaultConfig, template)
	if err != nil {
		t.Fatalf("Failed to render template: %v", err)
	}

	rendered = strings.TrimSpace(rendered)
	expected = strings.TrimSpace(expected)

	if rendered != expected {
		t.Errorf("Rendered annotations don't match expected output.\nGot:\n%s\n\nWant:\n%s", rendered, expected)
	}
}

func loadTestValues(t *testing.T, data []byte) Values {
	t.Helper()
	vals, err := ReadValues(data)
	if err != nil {
		t.Fatal(err)
	}
	return vals
}

func TestRenderTemplateWithConfig(t *testing.T) {
	tests := []struct {
		name            string
		templatePath    string
		config          *Config
		expectedOutPath string
		wantErr         bool
	}{
		{
			name:            "deployment template with config A",
			templatePath:    "deployment.yaml",
			expectedOutPath: "tests/testA.yaml",
			config: &Config{
				Name:                 "testA",
				Namespace:            "nsA",
				Class:                "testA",
				Release:              "ReleaseA",
				DBClaimOwnerRef:      "888888b8-d7c5-432d-8807-033f89aba33c",
				ConfigCheckSum:       "e1134fb",
				ServiceAccountName:   "robot",
				DatasourceUser:       "data",
				DatasourceSecretName: "secret/data",
				DatasourceFileName:   DefaultDBFileName,
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
				Values: loadTestValues(t, []byte(`
podLabels:
  "keyA": valueA
annotations:
  annkey: annvalue
  port: "80"
`)),
			},
		},
		{
			name:            "config template with config B",
			templatePath:    "config.yaml",
			expectedOutPath: "tests/testB.yaml",
			config: &Config{
				Name:                 "testB",
				Namespace:            "nsA",
				Release:              "ReleaseA",
				DBClaimOwnerRef:      "owner-abcd",
				ConfigCheckSum:       "e1134fb",
				ServiceAccountName:   "robot",
				DatasourceUser:       "data",
				DatasourceSecretName: "secret/data",
				DatasourceFileName:   DefaultDBFileName,
				Values: loadTestValues(t, []byte(`
podLabels:
  keyA: valueA
annotations:
  annkey: annvalue`)),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			templateContent := readTestFile(t, path.Join(testDataDir, tt.templatePath))
			got, err := render(context.TODO(), tt.config, templateContent)

			if (err != nil) != tt.wantErr {
				t.Fatalf("render() error = %v, wantErr %v", err, tt.wantErr)
			}

			expected := readTestFile(t, path.Join(testDataDir, tt.expectedOutPath))

			if got != expected {
				t.Errorf("rendered output mismatch:\ngot: %s\nwant: %s", got, expected)
			}

			// Validate that the rendered template is valid YAML
			var dep appsv1.Deployment
			if err := unmarshal([]byte(got), &dep); err != nil {
				t.Logf("Failed to unmarshal rendered template: %v", err)
				t.Logf("Rendered template: %s", got)
			}
		})
	}
}

func TestRenderFullResources(t *testing.T) {
	tests := []struct {
		name           string
		config         *Config
		wantErrRender  bool
		wantErrApply   bool
		wantDepPath    string
		wantConfigPath string
	}{
		{
			name:           "render deployment and config map A",
			wantDepPath:    "tests/RenderA-dep.yaml",
			wantConfigPath: "tests/RenderA-cm.yaml",
			config: &Config{
				Name:                 "RenderA",
				Namespace:            "nsA",
				Release:              "ReleaseA",
				Class:                "RenderA",
				DBClaimOwnerRef:      "owner-abcd",
				ConfigCheckSum:       "e1134fb",
				ServiceAccountName:   "robot",
				DatasourceUser:       "data",
				DatasourceSecretName: "secret/data",
				DatasourceFileName:   DefaultDBFileName,
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
				Values: loadTestValues(t, []byte(`
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
		t.Run(tt.name, func(t *testing.T) {
			deploymentYAML, configMapYAML, err := Render(context.TODO(), tt.config)
			if (err != nil) != tt.wantErrRender {
				t.Errorf("Render() error = %v, wantErr %v", err, tt.wantErrRender)
				return
			}

			// Check deployment YAML
			expectedDep := readTestFile(t, path.Join(testDataDir, tt.wantDepPath))
			if !strings.EqualFold(deploymentYAML, expectedDep) {
				t.Errorf("Deployment YAML mismatch")
			}

			// Check config map YAML
			expectedCM := readTestFile(t, path.Join(testDataDir, tt.wantConfigPath))
			if !strings.EqualFold(configMapYAML, expectedCM) {
				t.Errorf("ConfigMap YAML mismatch")
			}

			// Test applying the rendered templates
			var deployment appsv1.Deployment
			var configMap corev1.ConfigMap
			err = apply(context.TODO(), tt.config, &deployment, &configMap)
			if (err != nil) != tt.wantErrApply {
				t.Errorf("apply() error = %v, wantErr %v", err, tt.wantErrApply)
			}
		})
	}
}

// readTestFile reads a file and returns its content as a trimmed string
func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := ioutil.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read test file %s: %v", path, err)
	}
	return string(bytes.TrimSpace(data))
}
