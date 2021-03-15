package controllers

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	appcorev1 "github.com/warm-metal/cliapp/pkg/apis/cliapp/v1"
	"github.com/warm-metal/cliapp/pkg/utils"
	"golang.org/x/xerrors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"time"
)

var (
	DefaultRequeueDuration = 5 * time.Second
)

func groupPods(podList *corev1.PodList) (
	terminating, ready, starting []*corev1.Pod, terminatingDesc, readyDesc, startingDesc []string,
) {
	for i := range podList.Items {
		pod := &podList.Items[i]
		desc := fmt.Sprintf("%s|%s", pod.Name, pod.Status.Phase)
		if pod.DeletionTimestamp != nil {
			terminating = append(terminating, pod)
			terminatingDesc = append(terminatingDesc, desc)
			continue
		}

		if utils.IsPodReady(pod) {
			ready = append(ready, pod)
			readyDesc = append(readyDesc, desc)
			continue
		}

		starting = append(starting, pod)
		startingDesc = append(startingDesc, desc)
	}

	return
}

func (r *CliAppReconciler) transitPhaseTo(
	ctx context.Context, log logr.Logger, app *appcorev1.CliApp, phase appcorev1.CliAppPhase,
) error {
	app.Status.Phase = phase
	app.Status.LastPhaseTransition = metav1.Now()

	if phase == appcorev1.CliAppPhaseLive {
		if len(app.Status.PodName) == 0 {
			panic("set Pod name along with phase Live")
		}
		if len(app.Spec.Image) == 0 && app.Spec.Fork == nil {
			panic("set Image along with phase Live")
		}
	}

	if phase == appcorev1.CliAppPhaseRest {
		app.Status.PodName = ""
	}

	if err := r.Status().Update(ctx, app); err != nil {
		log.Error(err, "unable to update app")
		return err
	}

	return nil
}

func validateApp(app *appcorev1.CliApp) error {
	if len(app.Spec.Image) == 0 && len(app.Spec.Dockerfile) == 0 && app.Spec.Fork == nil {
		return xerrors.Errorf("specify either image, dockerfile, or Fork for the app")
	}

	if app.Spec.TargetPhase == "" {
		return xerrors.Errorf("TargetPhase is not set")
	}

	if app.Spec.TargetPhase != appcorev1.CliAppPhaseRest && app.Spec.TargetPhase != appcorev1.CliAppPhaseLive {
		return xerrors.Errorf("TargetPhase must be %q or %q", appcorev1.CliAppPhaseRest, appcorev1.CliAppPhaseLive)
	}

	if len(app.Spec.Distro) > 0 {
		if err := ValidateDistro(app.Spec.Distro); err != nil {
			return err
		}

	}

	if len(app.Spec.Shell) > 0 {
		if err := ValidateShell(app.Spec.Shell); err != nil {
			return err
		}
	}

	return nil
}

func ValidateDistro(d appcorev1.CliAppDistro) error {
	switch d {
	case appcorev1.CliAppDistroAlpine, appcorev1.CliAppDistroUbuntu:
		return nil
	default:
		return xerrors.Errorf("Spec.Distro must be either %q or %q",
			appcorev1.CliAppDistroAlpine, appcorev1.CliAppDistroUbuntu)
	}
}

func ValidateShell(s appcorev1.CliAppShell) error {
	switch s {
	case appcorev1.CliAppShellZsh, appcorev1.CliAppShellBash:
		return nil
	default:
		return xerrors.Errorf("Spec.Shell must be either %q or %q",
			appcorev1.CliAppShellZsh, appcorev1.CliAppShellBash)
	}
}
