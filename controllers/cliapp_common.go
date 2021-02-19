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
		if len(app.Spec.Image) == 0 {
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
	if len(app.Spec.Image) == 0 && len(app.Spec.Dockerfile) == 0 {
		return xerrors.Errorf("specify either image or dockerfile for the app")
	}

	if len(app.Spec.Command) == 0 {
		return xerrors.Errorf("specify command will be executed")
	}

	if app.Spec.TargetPhase == "" {
		return xerrors.Errorf("TargetPhase is not set")
	}

	if len(app.Spec.Distrio) > 0 {
		if app.Spec.Distrio != appcorev1.CliAppDistrioAlpine && app.Spec.Distrio != appcorev1.CliAppDistrioUbuntu {
			return xerrors.Errorf("Spec.Distrio must be either alpine or ubuntu")
		}
	}

	if len(app.Spec.Shell) > 0 {
		if app.Spec.Shell != appcorev1.CliAppShellZsh && app.Spec.Shell != appcorev1.CliAppShellBash {
			return xerrors.Errorf("Spec.Shell must be either bash or zsh")
		}
	}

	return nil
}
