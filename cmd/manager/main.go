/*


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
	"encoding/json"
	"flag"
	"fmt"
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
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
	"github.com/infobloxopen/db-controller/controllers"
	"github.com/infobloxopen/db-controller/pkg/config"
	"github.com/infobloxopen/db-controller/pkg/rdsauth"
	dbwebhook "github.com/infobloxopen/db-controller/webhook"

	//+kubebuilder:scaffold:imports

	crossplanerdsv1alpha1 "github.com/crossplane-contrib/provider-aws/apis/rds/v1alpha1"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup").V(controllers.InfoLevel)
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(persistancev1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme

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
	var metricsPort int
	var enableLeaderElection bool
	var configFile string
	var dBProxySidecarConfigPath string
	var dsnExecSidecarConfigPath string
	var probeAddr string
	var probePort int
	var enableDBProxyWebhook bool
	var enableDsnExecWebhook bool
	var dbIdentifierPrefix string
	var class string
	var metricsDepYamlPath string
	var metricsConfigYamlPath string
	var verbosity int

	flag.StringVar(&class, "class", "default", "The class of claims this db-controller instance needs to address.")
	flag.StringVar(&dbIdentifierPrefix, "db-identifier-prefix", "", "The prefix to be added to the DbHost. Ideally this is the env name.")
	flag.StringVar(&metricsAddr, "metrics-addr", "0.0.0.0", "The address the metric endpoint binds to.")
	flag.IntVar(&metricsPort, "metrics-port", 8080, "The port the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-address", "", "The address the probe endpoint binds to.")
	flag.IntVar(&probePort, "health-probe-port", 8081, "The port the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(&configFile, "config-file", "/etc/config/config.yaml", "Database connection string to with root credentials.")
	flag.StringVar(&dBProxySidecarConfigPath, "db-proxy-sidecar-config-path", "/etc/config/sidecar.yaml", "Mutating webhook sidecar configuration.")
	flag.StringVar(&dsnExecSidecarConfigPath, "dsnexec-sidecar-config-path", "/etc/config/sidecar.yaml", "Mutating webhook sidecar configuration.")
	flag.StringVar(&metricsDepYamlPath, "metrics-dep-yaml", "/config/postgres-exporter/deployment.yaml", "path to the metrics deployment yaml")
	flag.StringVar(&metricsConfigYamlPath, "metrics-config-yaml", "/config/postgres-exporter/config.yaml", "path to the metrics config yaml")
	flag.BoolVar(&enableDBProxyWebhook, "enable-db-proxy", false,
		"Enable DB Proxy webhook. "+
			"Enabling this option will cause the db-controller to inject db proxy pod into pods "+
			"with the infoblox.com/db-secret-path annotation set.")
	flag.BoolVar(&enableDsnExecWebhook, "enable-dsnexec", false,
		"Enable Dsnexec webhook. "+
			"Enabling this option will cause the db-controller to inject dsnexec container into pods "+
			"with the infoblox.com/remote-db-dsn-secret and infoblox.com/dsnexec-config-secret annotations set.")
	flag.IntVar(&verbosity, "verbosity", 0, "Configures the verbosity of the logging. "+
		"By default it's set to 0, set to 1 to enable debug logs.")
	opts := zap.Options{
		Development: false,
		TimeEncoder: zapcore.RFC3339NanoTimeEncoder,
		Level:       zapcore.Level(-1 * verbosity),
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	logger := zap.New(zap.UseFlagOptions(&opts))
	ctlConfig := config.NewConfig(logger, configFile)
	ctrl.SetLogger(logger)

	webhookOptions := webhook.Options{
		Port:    7443,
		CertDir: "./certs",
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		//MetricsBindAddress:     fmt.Sprintf("%s:%d", metricsAddr, metricsPort),
		HealthProbeBindAddress: fmt.Sprintf("%s:%d", probeAddr, probePort),
		WebhookServer:          webhook.NewServer(webhookOptions),
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "32151587.atlas.infoblox.com",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err = (&controllers.DatabaseClaimReconciler{
		Class:                 class,
		Client:                mgr.GetClient(),
		Config:                ctlConfig,
		DbIdentifierPrefix:    dbIdentifierPrefix,
		Log:                   ctrl.Log.WithName("controllers").WithName("DatabaseClaim").V(controllers.InfoLevel),
		MasterAuth:            rdsauth.NewMasterAuth(),
		MetricsDepYamlPath:    metricsDepYamlPath,
		MetricsConfigYamlPath: metricsConfigYamlPath,
		Scheme:                mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DatabaseClaim")
		os.Exit(1)
	}
	if err = (&controllers.DbRoleClaimReconciler{
		Class:    class,
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("dbRoleClaim-controller"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DbRoleClaim")
		os.Exit(1)
	}
	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	if enableDBProxyWebhook || enableDsnExecWebhook {
		webHookServer := mgr.GetWebhookServer()
		if enableDBProxyWebhook {

			cfg, err := dbwebhook.ParseConfig(dBProxySidecarConfigPath)

			if err != nil {
				setupLog.Error(err, "could not parse db proxy sidecar configuration")
				os.Exit(1)
			}
			setupLog.V(controllers.DebugLevel).Info("Parsed db proxy conig:", "dbproxysidecarconfig", cfg)

			setupLog.Info("registering with webhook server for DbProxy")
			decoder := admission.NewDecoder(mgr.GetScheme())

			webHookServer.Register("/mutate", &webhook.Admission{
				Handler: &dbwebhook.DBProxyInjector{
					Name:                 "DB Proxy",
					Client:               mgr.GetClient(),
					DBProxySidecarConfig: cfg,
					Decoder:              &decoder,
				},
			})
		}
		if enableDsnExecWebhook {

			cfg, err := dbwebhook.ParseConfig(dsnExecSidecarConfigPath)

			if err != nil {
				setupLog.Error(err, "could not parse dsnexec  sidecar configuration")
				os.Exit(1)
			}
			setupLog.V(controllers.DebugLevel).Info("Parsed dsnexec conig:", "dsnexecsidecarconfig", cfg)

			setupLog.Info("registering with webhook server for DsnExec")
			decoder := admission.NewDecoder(mgr.GetScheme())
			webHookServer.Register("/mutate-dsnexec", &webhook.Admission{
				Handler: &dbwebhook.DsnExecInjector{
					Name:                 "Dsnexec",
					Client:               mgr.GetClient(),
					DsnExecSidecarConfig: cfg,
					Decoder:              &decoder,
				},
			})
		}
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
