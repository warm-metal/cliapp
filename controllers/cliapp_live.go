package controllers

import (
	"context"
	"github.com/go-logr/logr"
	appcorev1 "github.com/warmmetal/cliapp/api/v1"
	"golang.org/x/xerrors"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (r *CliAppReconciler) makeAppLive(
	ctx context.Context, log logr.Logger, app *appcorev1.CliApp,
) (result ctrl.Result, err error) {
	if app.Spec.TargetPhase != appcorev1.CliAppPhaseLive {
		panic(app.Name)
	}

	log.V(1).Info("app status transits", "from", app.Status.Phase, "to", app.Spec.TargetPhase)

	switch app.Status.Phase {
	case "":
		if app.Spec.Image == "" {
			log.V(1).Info("build image")
			if app.Spec.Dockerfile == "" {
				err = xerrors.Errorf("specify either image or dockerfile for the app")
				return
			}

			if r.BuilderEndpoint == "" {
				err = xerrors.Errorf("unable to build image since no image builder installed")
				log.Error(err, "")
				return
			}

			if err = r.transitPhaseTo(ctx, log, app, appcorev1.CliAppPhaseBuilding); err != nil {
				return
			}

			result.Requeue = true
			return
		}

		fallthrough
	case appcorev1.CliAppPhaseShuttingDown:
		fallthrough
	case appcorev1.CliAppPhaseRest:
		if err = r.transitPhaseTo(ctx, log, app, appcorev1.CliAppPhaseRecovering); err != nil {
			return
		}

		fallthrough
	case appcorev1.CliAppPhaseRecovering:
		podList := corev1.PodList{}
		err = r.List(ctx, &podList, client.InNamespace(app.Namespace), client.MatchingLabels{appLabel: app.Name})
		if err != nil {
			log.Error(err, "unable to list app Pods")
			return
		}

		if len(podList.Items) > 0 {
			_, ready, starting, terminatingDesc, readyDesc, startingDesc := groupPods(&podList)
			log.Info("Pods of app", "terminating", terminatingDesc, "ready", readyDesc, "starting", startingDesc)

			if len(ready) > 0 {
				if len(ready) > 1 {
					err = xerrors.Errorf("app %s has %d ready Pods", app.ObjectMeta, len(ready))
					return
				}

				if len(starting) > 0 {
					err = xerrors.Errorf("app %s has %d Pods are still booting", app.ObjectMeta, len(starting))
					return
				}

				if err = r.transitPhaseTo(ctx, log, app, appcorev1.CliAppPhaseLive, ready[0].Name); err != nil {
					return
				}
				return
			}

			if len(starting) > 0 {
				if len(starting) > 1 {
					err = xerrors.Errorf("app %s has %d Pods are still booting", app.ObjectMeta, len(starting))
				}

				result.Requeue = true
				return
			}
		}

		err = r.startApp(ctx, app, log)
		result.Requeue = true

	case appcorev1.CliAppPhaseBuilding:
		// FIXME check the builder state and transit to CliAppPhaseRecovering
		panic("not implemented")

	case appcorev1.CliAppPhaseWaitingForSessions:
		if err = r.transitPhaseTo(ctx, log, app, appcorev1.CliAppPhaseLive); err != nil {
			return
		}
	default:
		panic(app.Status.Phase)
	}

	return
}
