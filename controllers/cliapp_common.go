package controllers

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	appcorev1 "github.com/warmmetal/cliapp/api/v1"
	"github.com/warmmetal/cliapp/utils"
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
	ctx context.Context, log logr.Logger, app *appcorev1.CliApp, phase appcorev1.CliAppPhase, podName ...string,
) error {
	app.Status.Phase = phase
	app.Status.LastPhaseTransition = metav1.Now()

	if phase == appcorev1.CliAppPhaseLive {
		if len(podName) == 0 || len(podName[0]) == 0 {
			panic("set Pod name along with phase Live")
		}

		app.Status.PodName = podName[0]
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
