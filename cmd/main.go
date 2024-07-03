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

package main

import (
	"crypto/tls"
	"encoding/json"
	"flag"
	"os"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	"go.uber.org/zap/zapcore"
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
	"github.com/infobloxopen/db-controller/internal/controller"
	"github.com/infobloxopen/db-controller/pkg/config"
	"github.com/infobloxopen/db-controller/pkg/databaseclaim"
	"github.com/infobloxopen/db-controller/pkg/rdsauth"

	// +kubebuilder:scaffold:imports

	crossplanerdsv1alpha1 "github.com/crossplane-contrib/provider-aws/apis/rds/v1alpha1"
	dbwebhook "github.com/infobloxopen/db-controller/webhook"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(persistancev1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme

	// Infrastructure provisioning using crossplane
	utilruntime.Must(crossplanerdsv1alpha1.SchemeBuilder.AddToScheme(scheme))
}

func parseDBPoxySidecarConfig(configFile string) (*dbwebhook.Config, error) {
	data, err := os.ReadFile(configFile)
	if err != nil {
		return nil, err
	}

	var cfg dbwebhook.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool
	var verbosity int

	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metric endpoint binds to. "+
		"Use the port :8080. If not set, it will be 0 in order to disable the metrics server")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", false,
		"If set the metrics endpoint is served securely")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")

	flag.IntVar(&verbosity, "verbosity", 0, "Configures the verbosity of the logging. "+
		"By default it's set to 0, set to 1 to enable debug logs.")

	opts := zap.Options{
		Development: false,
		TimeEncoder: zapcore.RFC3339NanoTimeEncoder,
		Level:       zapcore.Level(-1 * verbosity),
	}

	var class string
	var dbIdentifierPrefix string
	var configFile string
	var dBProxySidecarConfigPath string
	var dsnExecSidecarConfigPath string
	var metricsDepYamlPath string
	var metricsConfigYamlPath string
	var enableDBProxyWebhook bool
	var enableDSNExecWebhook bool

	flag.StringVar(&class, "class", "default", "The class of claims this db-controller instance needs to address.")
	flag.StringVar(&dbIdentifierPrefix, "db-identifier-prefix", "", "The prefix to be added to the DbHost. Ideally this is the env name.")

	flag.StringVar(&configFile, "config-file", "/etc/config/config.yaml", "Database connection string to with root credentials.")
	flag.StringVar(&dBProxySidecarConfigPath, "db-proxy-sidecar-config-path", "/etc/config/sidecar.yaml", "Mutating webhook sidecar configuration.")
	flag.StringVar(&dsnExecSidecarConfigPath, "dsnexec-sidecar-config-path", "/etc/config/sidecar.yaml", "Mutating webhook sidecar configuration.")
	flag.StringVar(&metricsDepYamlPath, "metrics-dep-yaml", "/config/postgres-exporter/deployment.yaml", "path to the metrics deployment yaml")
	flag.StringVar(&metricsConfigYamlPath, "metrics-config-yaml", "/config/postgres-exporter/config.yaml", "path to the metrics config yaml")
	flag.BoolVar(&enableDBProxyWebhook, "enable-db-proxy", false,
		"Enable DB Proxy webhook. "+
			"Enabling this option will cause the db-controller to inject db proxy pod into pods "+
			"with the infoblox.com/db-secret-path annotation set.")
	flag.BoolVar(&enableDSNExecWebhook, "enable-dsnexec", false,
		"Enable Dsnexec webhook. "+
			"Enabling this option will cause the db-controller to inject dsnexec container into pods "+
			"with the infoblox.com/remote-db-dsn-secret and infoblox.com/dsnexec-config-secret annotations set.")

	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	logger := zap.New(zap.UseFlagOptions(&opts))
	ctlConfig := config.NewConfig(logger, configFile)
	ctrl.SetLogger(logger)

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	tlsOpts := []func(*tls.Config){}
	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	webhookServer := webhook.NewServer(webhook.Options{
		TLSOpts: tlsOpts,
	})

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress:   metricsAddr,
			SecureServing: secureMetrics,
			TLSOpts:       tlsOpts,
		},
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "32151587.atlas.infoblox.com",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	cfg := &databaseclaim.DatabaseClaimConfig{
		Viper: ctlConfig,

		Class:              class,
		DbIdentifierPrefix: dbIdentifierPrefix,
		// Log:                   ctrl.Log.WithName("controllers").WithName("DatabaseClaim").V(controllers.InfoLevel),
		MasterAuth:            rdsauth.NewMasterAuth(),
		MetricsDepYamlPath:    metricsDepYamlPath,
		MetricsConfigYamlPath: metricsConfigYamlPath,
	}

	if err = (&controller.DatabaseClaimReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Config: cfg,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DatabaseClaim")
		os.Exit(1)
	}
	if err = (&controller.DbRoleClaimReconciler{
		Class:    class,
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("dbRoleClaim-controller"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DbRoleClaim")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
