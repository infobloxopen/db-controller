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
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/go-logr/logr/funcr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
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
var namespace string
var logger logr.Logger

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite", Label("FailFast"))

	logger = funcr.New(func(prefix, args string) {
		t.Log(prefix, args)
	}, funcr.Options{
		//Verbosity: 1,
	})
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	namespace = "default"
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,

		// The BinaryAssetsDirectory is only required if you want to run the tests directly
		// without call the makefile target test. If not informed it will look for the
		// default path defined in controller-runtime which is /usr/local/kubebuilder/.
		// Note that you must have the required binaries setup under the bin directory to perform
		// the tests directly. When we run make test it will be setup and used automatically.
		BinaryAssetsDirectory: filepath.Join("..", "..", "bin", "k8s",
			fmt.Sprintf("1.30.0-%s-%s", runtime.GOOS, runtime.GOARCH)),
	}

	var err error
	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = persistancev1.AddToScheme(scheme.Scheme)
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
	logger.Info("postgres_setup_took", "duration", time.Since(now))

	// Setup controller
	By("setting up the database controller")
	configPath, err := filepath.Abs(filepath.Join("..", "..", "cmd", "config", "config.yaml"))
	Expect(err).NotTo(HaveOccurred())

	controllerReconciler = &DatabaseClaimReconciler{
		Config: &databaseclaim.DatabaseClaimConfig{
			Viper:     config.NewConfig(logger, configPath),
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

// Stand up postgres in a container
var (
	testdb               *sql.DB
	testDSN              string
	cleanupTestDB        func()
	controllerReconciler *DatabaseClaimReconciler
)

var _ = AfterSuite(func() {
	cleanupTestDB()
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})
