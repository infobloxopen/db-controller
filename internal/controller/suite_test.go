/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/go-logr/logr"
	uberzap "go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	crossplanerdsv1alpha1 "github.com/crossplane-contrib/provider-aws/apis/rds/v1alpha1"
	crossplanegcpv1beta2 "github.com/upbound/provider-gcp/apis/alloydb/v1beta2"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
	"github.com/infobloxopen/db-controller/internal/dockerdb"
	"github.com/infobloxopen/db-controller/pkg/config"
	"github.com/infobloxopen/db-controller/pkg/databaseclaim"
	// +kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var cfg *rest.Config
var k8sClient client.Client
var testEnv *envtest.Environment
var controllerReconciler *DatabaseClaimReconciler
var namespace string
var logger logr.Logger
var env = "testenv"
var class = "testenv"

// Stand up postgres in a container
var (
	testdb        *sql.DB
	testDSN       string
	cleanupTestDB func()
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Controller Suite", Label("FailFast"))
}

func NewGinkgoLogger() logr.Logger {
	// Create a new Zap logger, routing output to GinkgoWriter
	return zap.New(zap.WriteTo(GinkgoWriter), zap.UseFlagOptions(&zap.Options{
		Encoder:     zapcore.NewConsoleEncoder(uberzap.NewDevelopmentEncoderConfig()),
		Development: true,
		TimeEncoder: zapcore.RFC3339TimeEncoder,
	}))
}

var _ = BeforeSuite(func() {

	logger = NewGinkgoLogger()
	log.SetLogger(logger)
	By("bootstrapping test environment")
	namespace = "default"
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "config", "crd", "bases"),
			filepath.Join("..", "..", "test", "crd"),
		},
		ErrorIfCRDPathMissing: true,
		WebhookInstallOptions: envtest.WebhookInstallOptions{
			// Paths: []string{filepath.Join("..", "..", "config", "webhook")},
		},
		// The BinaryAssetsDirectory is only required if you want to run the tests directly
		// without call the makefile target test. If not informed it will look for the
		// default path defined in controller-runtime which is /usr/local/kubebuilder/.
		// Note that you must have the required binaries setup under the bin directory to perform
		// the tests directly. When we run make test it will be setup and used automatically.
		BinaryAssetsDirectory: filepath.Join("..", "..", "bin", "k8s",
			fmt.Sprintf("1.30.0-%s-%s", runtime.GOOS, runtime.GOARCH)),
		AttachControlPlaneOutput: false,
	}

	var err error
	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = scheme.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = persistancev1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = crossplanerdsv1alpha1.SchemeBuilder.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = crossplanegcpv1beta2.SchemeBuilder.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	// +kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	now := time.Now()
	testdb, testDSN, cleanupTestDB = dockerdb.Run(dockerdb.Config{
		Database:  "postgres",
		Username:  "postgres",
		Password:  "postgres",
		DockerTag: "15",
	})
	Expect(testdb.Ping()).To(Succeed(), "Database connection failed")
	logger.Info("postgres_setup_took", "duration", time.Since(now))

	// Mock table for testing migrations
	_, err = testdb.Exec(`CREATE TABLE IF NOT EXISTS users (
		id SERIAL PRIMARY KEY,
		name TEXT NOT NULL,
		email TEXT NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`)
	Expect(err).NotTo(HaveOccurred())

	// Setup controller
	By("setting up the database controller")
	configPath, err := filepath.Abs(filepath.Join("..", "..", "cmd", "config", "config.yaml"))
	Expect(err).NotTo(HaveOccurred())
	_, err = os.Stat(configPath)
	Expect(err).NotTo(HaveOccurred(), "Configuration file not found at path: %s", configPath)

	viperCfg := config.NewConfig(configPath)
	Expect(viperCfg).NotTo(BeNil(), "Failed to initialize Viper configuration")
	// Used by kctlutils
	viperCfg.Set("SERVICE_NAMESPACE", "default")
	controllerReconciler = &DatabaseClaimReconciler{
		Config: &databaseclaim.DatabaseClaimConfig{
			Viper:     viperCfg,
			Namespace: "default",
		},
		Client: k8sClient,
		Scheme: k8sClient.Scheme(),
	}
	controllerReconciler.Setup()

	// FIXME: make these actual properties on the reconciler struct
	controllerReconciler.Config.Viper.Set("defaultMasterusername", "postgres")
	controllerReconciler.Config.Viper.Set("defaultSslMode", "disable")

})

var _ = AfterSuite(func() {
	cleanupTestDB()
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})
