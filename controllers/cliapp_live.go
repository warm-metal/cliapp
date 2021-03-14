package controllers

import (
	"context"
	"github.com/go-logr/logr"
	appcorev1 "github.com/warm-metal/cliapp/pkg/apis/cliapp/v1"
	"github.com/warm-metal/cliapp/pkg/utils"
	"golang.org/x/xerrors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"time"
)

func (r *CliAppReconciler) makeAppLive(
	ctx context.Context, log logr.Logger, app *appcorev1.CliApp,
) (result ctrl.Result, err error) {
	if app.Spec.TargetPhase != appcorev1.CliAppPhaseLive {
		panic(app.Name)
	}

	log.V(1).Info("app status", "current", app.Status.Phase, "target", app.Spec.TargetPhase)

	if app.Spec.Fork == nil && app.Status.Phase != appcorev1.CliAppPhaseBuilding && app.Spec.Image == "" {
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
		err = r.transitPhaseTo(ctx, log, app, appcorev1.CliAppPhaseRecovering)
		result.Requeue = true
		return
	case appcorev1.CliAppPhaseRecovering:
		if len(app.Status.PodName) > 0 {
			pod := corev1.Pod{}
			err = r.Get(ctx, types.NamespacedName{Name: app.Status.PodName, Namespace: app.Namespace}, &pod)
			if err != nil && !errors.IsNotFound(err) {
				log.Error(err, "unable to get app Pod")
				return
			}

			if errors.IsNotFound(err) {
				elapse := metav1.Now().Sub(app.Status.LastPhaseTransition.Time)
				if elapse < 10*time.Second {
					log.Info("pod not found. will wait at most 10 seconds before recreating")
					result.RequeueAfter = 10*time.Second - elapse
					return result, xerrors.Errorf("pod not found. will create %s later", result.RequeueAfter)
				}
			}

			if err == nil {
				if utils.IsPodReady(&pod) {
					if err = r.transitPhaseTo(ctx, log, app, appcorev1.CliAppPhaseLive); err != nil {
						return
					}
					return
				}

				if pod.DeletionTimestamp == nil {
					log.Info("wait for pod to be ready", "pod", app.Status.PodName)
					return
				}

				log.Info("pod is terminating. will create a new one.", "pod", app.Status.PodName)
			}
		}

		log.Info("create pod")
		pod, err := r.startApp(ctx, app, log)
		if err == nil {
			app.Status.PodName = pod.Name
			if err := r.Status().Update(ctx, app); err != nil {
				log.Error(err, "unable to save pod name to app. delete pod", "pod", pod.Name)
				if err := r.Delete(ctx, pod); err != nil {
					log.Error(err, "unable to delete pod", "pod", pod.Name)
				}

				return result, err
			}
		}

		return result, err

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
}
