/*
Copyright 2021.

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
	"github.com/warm-metal/cliapp/pkg/utils"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"os"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	k8sScheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/warm-metal/cliapp/controllers"
	appcorev1 "github.com/warm-metal/cliapp/pkg/apis/cliapp/v1"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(k8sScheme.AddToScheme(scheme))
	utilruntime.Must(appcorev1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")

	var idleLiveLasts time.Duration
	flag.DurationVar(&idleLiveLasts, "idle-live", 10*time.Minute, "Duration in that the background pod "+
		"would be still alive even no active session opened.")

	var builderSvc string
	flag.StringVar(&builderSvc, "builder-svc", "", "buildkitd endpoint used to build image for app")

	var defaultAppContextImage, defaultShell, defaultDistro string
	flag.StringVar(&defaultAppContextImage, "app-context", "", "The context image to start an app")
	flag.StringVar(&defaultShell, "default-shell", "",
		"The shell cliapp used as default. The default value is bash. You can also use zsh instead.")
	flag.StringVar(&defaultDistro, "default-distro", "",
		"Linux distro on that the app works as default. The default value is alpine. Another supported distro is ubuntu.")

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "337df6b6.cliapp.warm-metal.tech",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if len(defaultDistro) > 0 {
		if controllers.ValidateDistro(appcorev1.CliAppDistro(defaultDistro)) != nil {
			setupLog.Error(err, "invalid distro")
			os.Exit(1)
		}
	} else {
		setupLog.Info("alpine is used as the default Linux distro")
		defaultDistro = string(appcorev1.CliAppDistroAlpine)
	}

	if len(defaultShell) > 0 {
		if controllers.ValidateShell(appcorev1.CliAppShell(defaultShell)) != nil {
			setupLog.Error(err, "invalid shell")
			os.Exit(1)
		}
	} else {
		setupLog.Info("bash is used as the default shell")
		defaultShell = string(appcorev1.CliAppShellBash)
	}

	if err = (&controllers.CliAppReconciler{
		RestClient:             clientGetter(mgr),
		Client:                 mgr.GetClient(),
		Log:                    ctrl.Log.WithName("controllers").WithName("CliApp"),
		Scheme:                 mgr.GetScheme(),
		DurationIdleLiveLasts:  idleLiveLasts,
		BuilderEndpoint:        builderSvc,
		ControllerNamespace:    utils.GetCurrentNamespace(),
		ImageBuilder:           controllers.InitImageBuilderOrDie(builderSvc),
		DefaultAppContextImage: defaultAppContextImage,
		DefaultDistro:          appcorev1.CliAppDistro(defaultDistro),
		DefaultShell:           appcorev1.CliAppShell(defaultShell),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "CliApp")
		os.Exit(1)
	}
	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("health", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("check", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func clientGetter(mgr manager.Manager) resource.RESTClientGetter {
	discoveryCli := memory.NewMemCacheClient(discovery.NewDiscoveryClientForConfigOrDie(mgr.GetConfig()))
	return &mgrClientGetter{
		config:    mgr.GetConfig(),
		mapper:    restmapper.NewShortcutExpander(mgr.GetRESTMapper(), discoveryCli),
		discovery: discoveryCli,
	}
}

type mgrClientGetter struct {
	config    *rest.Config
	mapper    meta.RESTMapper
	discovery discovery.CachedDiscoveryInterface
}

func (m mgrClientGetter) ToRESTConfig() (*rest.Config, error) {
	return m.config, nil
}

func (m mgrClientGetter) ToDiscoveryClient() (discovery.CachedDiscoveryInterface, error) {
	return m.discovery, nil
}

func (m mgrClientGetter) ToRESTMapper() (meta.RESTMapper, error) {
	return m.mapper, nil
}
