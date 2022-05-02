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

	persistancev1 "github.com/infobloxopen/db-controller/api/v1"
	"github.com/infobloxopen/db-controller/controllers"
	//+kubebuilder:scaffold:imports
	"github.com/infobloxopen/db-controller/pkg/config"
	"github.com/infobloxopen/db-controller/pkg/rdsauth"
	// +kubebuilder:scaffold:imports
	crossplanedbv1beta1 "github.com/crossplane/provider-aws/apis/database/v1beta1"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(persistancev1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme

	// Infrastructure provisioning using crossplane
	utilruntime.Must(crossplanedbv1beta1.SchemeBuilder.AddToScheme(scheme))

}

func main() {
	var metricsAddr string
	var metricsPort int
	var enableLeaderElection bool
	var configFile string
	var probeAddr string
	var probePort int
	flag.StringVar(&metricsAddr, "metrics-addr", "0.0.0.0", "The address the metric endpoint binds to.")
	flag.IntVar(&metricsPort, "metrics-port", 8080, "The port the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-address", "", "The address the probe endpoint binds to.")
	flag.IntVar(&probePort, "health-probe-port", 8081, "The port the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(&configFile, "config-file", "/etc/config/config.yaml",
		"Database connection string to with root credentials.")
	opts := zap.Options{
		Development: false,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	logger := zap.New(zap.UseFlagOptions(&opts))
	ctlConfig := config.NewConfig(logger, configFile)
	ctrl.SetLogger(logger)

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     fmt.Sprintf("%s:%d", metricsAddr, metricsPort),
		HealthProbeBindAddress: fmt.Sprintf("%s:%d", probeAddr, probePort),
		Port:                   9443,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "32151587.atlas.infoblox.com",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err = (&controllers.DatabaseClaimReconciler{
		Client:     mgr.GetClient(),
		Log:        ctrl.Log.WithName("controllers").WithName("DatabaseClaim"),
		Scheme:     mgr.GetScheme(),
		Config:     ctlConfig,
		MasterAuth: rdsauth.NewMasterAuth(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DatabaseClaim")
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

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
