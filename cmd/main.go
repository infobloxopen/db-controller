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
	"flag"
	"fmt"
	"os"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.

	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
	"github.com/infobloxopen/db-controller/internal/controller"
	mutating "github.com/infobloxopen/db-controller/internal/webhook"
	"github.com/infobloxopen/db-controller/pkg/config"
	"github.com/infobloxopen/db-controller/pkg/databaseclaim"
	"github.com/infobloxopen/db-controller/pkg/rdsauth"
	"github.com/infobloxopen/db-controller/pkg/roleclaim"
	dbwebhook "github.com/infobloxopen/db-controller/webhook"

	persistanceinfobloxcomv1alpha1 "github.com/infobloxopen/db-controller/api/persistance.infoblox.com/v1alpha1"
	// +kubebuilder:scaffold:imports

	crossplanerdsv1alpha1 "github.com/crossplane-contrib/provider-aws/apis/rds/v1alpha1"
	crossplanegcpv1beta2 "github.com/upbound/provider-gcp/apis/alloydb/v1beta2"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(persistancev1.AddToScheme(scheme))
	utilruntime.Must(persistanceinfobloxcomv1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme

	// Infrastructure provisioning using crossplane
	utilruntime.Must(crossplanerdsv1alpha1.SchemeBuilder.AddToScheme(scheme))

	utilruntime.Must(crossplanegcpv1beta2.SchemeBuilder.AddToScheme(scheme))
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool
	var tlsOpts []func(*tls.Config)
	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")

	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", false,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")

	var class string
	var configFile string
	var dsnExecSidecarConfigPath string
	var metricsDepYamlPath string
	var metricsConfigYamlPath string
	var enableDBProxyWebhook bool
	var enableDSNExecWebhook bool

	flag.StringVar(&class, "class", "default", "The class of claims this db-controller instance needs to address.")

	flag.StringVar(&configFile, "config-file", "/etc/config/config.yaml", "Database connection string to with root credentials.")
	flag.StringVar(&dsnExecSidecarConfigPath, "dsnexec-sidecar-config-path", "/etc/config/dsnexec/dsnexecsidecar.json", "Mutating webhook sidecar configuration.")
	flag.StringVar(&metricsDepYamlPath, "metrics-dep-yaml", "/config/postgres-exporter/deployment.yaml", "path to the metrics deployment yaml")
	flag.StringVar(&metricsConfigYamlPath, "metrics-config-yaml", "/config/postgres-exporter/config.yaml", "path to the metrics config yaml")
	flag.BoolVar(&enableDBProxyWebhook, "enable-db-proxy", false, "Enable DB Proxy webhook. Enabling this option will cause the db-controller to inject db proxy pod into pods with the infoblox.com/db-secret-path annotation set.")
	flag.BoolVar(&enableDSNExecWebhook, "enable-dsnexec", false, "Enable Dsnexec webhook. Enabling this option will cause the db-controller to inject dsnexec container into pods with the infoblox.com/remote-db-dsn-secret and infoblox.com/dsnexec-config-secret annotations set.")

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	ctlConfig := config.NewConfig(configFile)

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

	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	webhookServer := webhook.NewServer(webhook.Options{
		TLSOpts: tlsOpts,
		// 9443 is the default port
		Port: 9443,
	})

	// Metrics endpoint is enabled in 'config/default/kustomization.yaml'. The Metrics options configure the server.
	// More info:
	// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.18.4/pkg/metrics/server
	// - https://book.kubebuilder.io/reference/metrics.html
	metricsServerOptions := metricsserver.Options{
		BindAddress:   metricsAddr,
		SecureServing: secureMetrics,
		// TODO(user): TLSOpts is used to allow configuring the TLS config used for the server. If certificates are
		// not provided, self-signed certificates will be generated by default. This option is not recommended for
		// production environments as self-signed certificates do not offer the same level of trust and security
		// as certificates issued by a trusted Certificate Authority (CA). The primary risk is potentially allowing
		// unauthorized access to sensitive metrics data. Consider replacing with CertDir, CertName, and KeyName
		// to provide certificates, ensuring the server communicates using trusted and secure certificates.
		TLSOpts: tlsOpts,
	}

	// TODO: we do not secure metrics and this introduces build problems with opentracing
	// if secureMetrics {
	// 	// FilterProvider is used to protect the metrics endpoint with authn/authz.
	// 	// These configurations ensure that only authorized users and service accounts
	// 	// can access the metrics endpoint. The RBAC are configured in 'config/rbac/kustomization.yaml'. More info:
	// 	// https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.18.4/pkg/metrics/filters#WithAuthenticationAndAuthorization
	// 	metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	// }

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsServerOptions,
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

	namespace := os.Getenv("SERVICE_NAMESPACE")
	if len(namespace) == 0 {
		setupLog.Error(fmt.Errorf("empty_namespace"), "namespace must be set")
		os.Exit(1)
	}

	dbClaimConfig := &databaseclaim.DatabaseClaimConfig{
		Viper:     ctlConfig,
		Namespace: namespace,
		Class:     class,
		// Log:                   ctrl.Log.WithName("controllers").WithName("DatabaseClaim").V(controllers.InfoLevel),
		MasterAuth:            rdsauth.NewMasterAuth(),
		MetricsEnabled:        true,
		MetricsDepYamlPath:    metricsDepYamlPath,
		MetricsConfigYamlPath: metricsConfigYamlPath,
	}

	setupLog.Info("setup_ctrl", "dbClaimConfig", dbClaimConfig)
	if err = (&controller.DatabaseClaimReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Config: dbClaimConfig,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DatabaseClaim")
		os.Exit(1)
	}
	dbRoleClaimConfig := &roleclaim.RoleConfig{
		Viper:     ctlConfig,
		Namespace: namespace,
		Class:     class,
		// Log:                   ctrl.Log.WithName("controllers").WithName("DatabaseClaim").V(controllers.InfoLevel),
		MasterAuth: rdsauth.NewMasterAuth(),
	}

	if err = (&controller.DbRoleClaimReconciler{
		Class:  class,
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Config: dbRoleClaimConfig,
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

	if enableDBProxyWebhook {

		if err := mutating.SetupWebhookWithManager(mgr, mutating.SetupConfig{
			Namespace:  namespace,
			Class:      class,
			DBProxyImg: os.Getenv("DBPROXY_IMAGE"),
		}); err != nil {
			setupLog.Error(err, "failed to setup webhooks")
			os.Exit(1)
		}
	}
	if enableDSNExecWebhook {

		cfg, err := dbwebhook.ParseConfig(dsnExecSidecarConfigPath)

		if err != nil {
			setupLog.Error(err, "could not parse dsnexec  sidecar configuration")
			os.Exit(1)
		}
		setupLog.Info("dnsexec-controller", "config", cfg)

		mgr.GetWebhookServer().Register("/mutate-dsnexec", &webhook.Admission{
			Handler: &dbwebhook.DsnExecInjector{
				Name:                 "Dsnexec",
				Client:               mgr.GetClient(),
				DsnExecSidecarConfig: cfg,
				Decoder:              admission.NewDecoder(mgr.GetScheme()),
			},
		})
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
