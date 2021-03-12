package controllers

import (
	"context"
	"github.com/go-logr/logr"
	appcorev1 "github.com/warm-metal/cliapp/pkg/apis/cliapp/v1"
	"golang.org/x/xerrors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (r *CliAppReconciler) makeAppLive(
	ctx context.Context, log logr.Logger, app *appcorev1.CliApp,
) (result ctrl.Result, err error) {
	if app.Spec.TargetPhase != appcorev1.CliAppPhaseLive {
		panic(app.Name)
	}

	log.V(1).Info("app status", "current", app.Status.Phase, "target", app.Spec.TargetPhase)

	if app.Spec.Fork != nil && app.Status.Phase != appcorev1.CliAppPhaseBuilding && app.Spec.Image == "" {
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

		result.RequeueAfter = DefaultRequeueDuration
		return
	}

	switch app.Status.Phase {
	case "", appcorev1.CliAppPhaseShuttingDown, appcorev1.CliAppPhaseWaitingForSessions, appcorev1.CliAppPhaseRest:
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
				if len(starting) > 0 {
					err = xerrors.Errorf("app has %d Pods are still booting", len(starting))
					return
				}

				if len(ready) > 1 {
					log.Info("app has more than 1 ready Pods", "num", len(ready))
					for i := 1; i < len(ready); i++ {
						log.Info("destroy pod", "pod", ready[i].Name)
						if err = r.Delete(ctx, ready[i]); err != nil {
							log.Error(err, "unable to destroy pod", "pod", ready[i].Name)
						}
					}
				}

				app.Status.PodName = ready[0].Name
				if err = r.transitPhaseTo(ctx, log, app, appcorev1.CliAppPhaseLive); err != nil {
					return
				}
				return
			}

			if len(starting) > 0 {
				if len(starting) > 1 {
					err = xerrors.Errorf("app has %d Pods are still booting", len(starting))
				}

				result.RequeueAfter = DefaultRequeueDuration
				return
			}
		} else {
			log.Info("no pod found")
		}

		err = r.startApp(ctx, app, log)

	case appcorev1.CliAppPhaseBuilding:
		if len(app.Spec.Image) == 0 {
			log.Info("build image")
			image, err := r.testImage(log, app)
			if err != nil {
				result.RequeueAfter = DefaultRequeueDuration
				return result, nil
			}

			err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
				app.Spec.Image = image
				return r.Update(ctx, app)
			})

			if err != nil {
				result.RequeueAfter = DefaultRequeueDuration
				return result, err
			}
		}

		if err = r.transitPhaseTo(ctx, log, app, appcorev1.CliAppPhaseRecovering); err != nil {
			return
		}
		result.Requeue = true
		return

	default:
		panic(app.Status.Phase)
	}

	return
}
