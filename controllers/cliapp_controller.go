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
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1 "github.com/warm-metal/cliapp/api/v1"
)

// CliAppReconciler reconciles a CliApp object
type CliAppReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme

	DurationIdleLiveLasts time.Duration
	BuilderEndpoint       string
	ControllerNamespace   string
}

//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core.cliapp.warmmetal.tech,resources=cliapps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core.cliapp.warmmetal.tech,resources=cliapps/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=core.cliapp.warmmetal.tech,resources=cliapps/finalizers,verbs=update

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

	app := corev1.CliApp{}
	if err = r.Get(ctx, req.NamespacedName, &app); err != nil {
		log.Error(err, "unable to fetch app", "app", req.NamespacedName.String())
		err = client.IgnoreNotFound(err)
		return
	}

	if app.Spec.TargetPhase == "" {
		err = xerrors.Errorf("TargetPhase is not set")
		return
	}

	if app.Spec.TargetPhase == app.Status.Phase {
		return
	}

	switch app.Spec.TargetPhase {
	case corev1.CliAppPhaseRest:
		return r.makeAppRest(ctx, log, &app)
	case corev1.CliAppPhaseLive:
		return r.makeAppLive(ctx, log, &app)
	default:
		err = xerrors.Errorf("TargetPhase can only be either Rest or Live")
		return
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *CliAppReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.CliApp{}).
		Complete(r)
}
