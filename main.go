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
	configv1 "github.com/warm-metal/cliapp/pkg/apis/config/v1"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(k8sScheme.AddToScheme(scheme))
	utilruntime.Must(appcorev1.AddToScheme(scheme))
	utilruntime.Must(configv1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	var configFile string
	flag.StringVar(&configFile, "config", "",
		"The controller will load its initial configuration from this file. "+
			"Omit this flag to use the default configuration values. "+
			"Command-line flags override configuration from this file.")

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	var err error
	ctrlConfig := configv1.CliAppDefault{}
	options := ctrl.Options{Scheme: scheme}
	if configFile != "" {
		options, err = options.AndFrom(ctrl.ConfigFile().AtPath(configFile).OfKind(&ctrlConfig))
		if err != nil {
			setupLog.Error(err, "unable to load the config file")
			os.Exit(1)
		}
	}

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	setupLog.Info("static config", "default", ctrlConfig)

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), options)
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if len(ctrlConfig.DefaultDistro) > 0 {
		if controllers.ValidateDistro(appcorev1.CliAppDistro(ctrlConfig.DefaultDistro)) != nil {
			setupLog.Error(err, "invalid distro")
			os.Exit(1)
		}
	} else {
		setupLog.Info("alpine is used as the default Linux distro")
		ctrlConfig.DefaultDistro = string(appcorev1.CliAppDistroAlpine)
	}

	if len(ctrlConfig.DefaultShell) > 0 {
		if controllers.ValidateShell(appcorev1.CliAppShell(ctrlConfig.DefaultShell)) != nil {
			setupLog.Error(err, "invalid shell")
			os.Exit(1)
		}
	} else {
		setupLog.Info("bash is used as the default shell")
		ctrlConfig.DefaultShell = string(appcorev1.CliAppShellBash)
	}

	if err = (&controllers.CliAppReconciler{
		RestClient:             clientGetter(mgr),
		Client:                 mgr.GetClient(),
		Log:                    ctrl.Log.WithName("controllers").WithName("CliApp"),
		Scheme:                 mgr.GetScheme(),
		DurationIdleLiveLasts:  ctrlConfig.DurationIdleLivesLast.Duration,
		BuilderEndpoint:        ctrlConfig.BuilderService,
		ControllerNamespace:    utils.GetCurrentNamespace(),
		ImageBuilder:           controllers.InitImageBuilderOrDie(ctrlConfig.BuilderService),
		DefaultAppContextImage: ctrlConfig.DefaultAppContextImage,
		DefaultDistro:          appcorev1.CliAppDistro(ctrlConfig.DefaultDistro),
		DefaultShell:           appcorev1.CliAppShell(ctrlConfig.DefaultShell),
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
