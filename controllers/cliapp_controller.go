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

package controllers

import (
	"context"
	"golang.org/x/xerrors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/cli-runtime/pkg/resource"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appcorev1 "github.com/warm-metal/cliapp/pkg/apis/cliapp/v1"
)

// CliAppReconciler reconciles a CliApp object
type CliAppReconciler struct {
	RestClient resource.RESTClientGetter
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme

	BuilderEndpoint string
	ImageBuilder

	DurationIdleLiveLasts time.Duration
	ControllerNamespace   string

	DefaultAppContextImage string
	DefaultShell           appcorev1.CliAppShell
	DefaultDistro          appcorev1.CliAppDistro
}

//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete;deletecollection
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch
//+kubebuilder:rbac:groups="extensions",resources=deployments;daemonsets;replicasets,verbs=get
//+kubebuilder:rbac:groups="apps",resources=replicasets;daemonsets;statefulsets;deployments,verbs=get
//+kubebuilder:rbac:groups="batch",resources=cronjobs;jobs,verbs=get
//+kubebuilder:rbac:groups=core.cliapp.warm-metal.tech,resources=cliapps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core.cliapp.warm-metal.tech,resources=cliapps/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=core.cliapp.warm-metal.tech,resources=cliapps/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the CliApp object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.7.0/pkg/reconcile
func (r *CliAppReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	log := r.Log.WithValues("cliapp", req.NamespacedName)

	app := &appcorev1.CliApp{}
	if err = r.Get(ctx, req.NamespacedName, app); err != nil {
		return result, client.IgnoreNotFound(err)
	}

	app.Status.Error = ""
	defer func() {
		if err != nil {
			app.Status.Error = err.Error()
			if err := r.Status().Update(ctx, app); err != nil {
				log.Error(err, "unable to update error state")
			}

			err = nil
		}
	}()

	if err = validateApp(app); err != nil {
		return
	}

	switch app.Spec.TargetPhase {
	case appcorev1.CliAppPhaseRest:
		if app.Spec.TargetPhase == app.Status.Phase {
			return
		}

		result, err = r.makeAppRest(ctx, log, app)
	case appcorev1.CliAppPhaseLive:
		result, err = r.makeAppLive(ctx, log, app)
	default:
		err = xerrors.Errorf("TargetPhase can only be either Rest or Live")
	}

	return
}

// SetupWithManager sets up the controller with the Manager.
func (r *CliAppReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appcorev1.CliApp{}).
		Owns(&corev1.Pod{}).
		Complete(r)
}
