package controllers

import (
	"context"
	"github.com/go-logr/logr"
	appcorev1 "github.com/warm-metal/cliapp/api/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (r *CliAppReconciler) makeAppRest(ctx context.Context, log logr.Logger, app *appcorev1.CliApp) (result ctrl.Result, err error) {
	if app.Spec.TargetPhase != appcorev1.CliAppPhaseRest {
		panic(app.Name)
	}

	log.V(1).Info("app status transits", "from", app.Status.Phase, "to", app.Spec.TargetPhase)

	switch app.Status.Phase {
	case appcorev1.CliAppPhaseLive:
		if err = r.transitPhaseTo(ctx, log, app, appcorev1.CliAppPhaseWaitingForSessions); err != nil {
			return
		}

		result.RequeueAfter = r.DurationIdleLiveLasts
		return
	case appcorev1.CliAppPhaseWaitingForSessions:
		now := metav1.Now()
		elapse := now.Sub(app.Status.LastPhaseTransition.Time)
		if elapse < r.DurationIdleLiveLasts {
			result.RequeueAfter = r.DurationIdleLiveLasts - elapse
			return
		}

		fallthrough
	case "", appcorev1.CliAppPhaseRecovering:
		if err = r.transitPhaseTo(ctx, log, app, appcorev1.CliAppPhaseShuttingDown); err != nil {
			return
		}

		fallthrough
	case appcorev1.CliAppPhaseShuttingDown:
		podList := corev1.PodList{}
		err = r.List(ctx, &podList, client.InNamespace(app.Namespace), client.MatchingLabels{appLabel: app.Name})
		if err != nil {
			log.Error(err, "unable to list Pods of app")
			return
		}

		if len(podList.Items) == 0 {
			if err = r.transitPhaseTo(ctx, log, app, appcorev1.CliAppPhaseRest); err != nil {
				return
			}

			return
		}

		result.Requeue = true
		terminating, ready, starting, terminatingDesc, readyDesc, startingDesc := groupPods(&podList)
		log.Info("Pods of app", "terminating", terminatingDesc, "ready", readyDesc, "starting", startingDesc)

		if len(terminating) > 0 {
			return
		}

		for _, pod := range append(ready, starting...) {
			if err = r.Delete(ctx, pod); err != nil {
				log.Error(err, "unable to delete pod", "pod", pod.Name)
			}
		}

		return

	case appcorev1.CliAppPhaseBuilding:
		// FIXME cancel building and go to CliAppPhaseShuttingDown
	default:
		panic(app.Status.Phase)
	}

	return
}
