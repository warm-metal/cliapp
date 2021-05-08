package controllers

import (
	"context"
	"fmt"
	"github.com/davecgh/go-spew/spew"
	"github.com/go-logr/logr"
	appcorev1 "github.com/warm-metal/cliapp/pkg/apis/cliapp/v1"
	"github.com/warm-metal/cliapp/pkg/utils"
	"golang.org/x/xerrors"
	"hash"
	"hash/fnv"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
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
	case appcorev1.CliAppPhaseLive:
		specHash := computeHash(&app.Spec)
		specDump := encodeSpec(&app.Spec)
		var newPod *corev1.Pod
		newPod, err = r.claimPods(ctx, log, app, specDump, specHash)
		if err != nil {
			return
		}

		if newPod != nil && utils.IsPodReady(newPod) {
			if app.Status.PodName != newPod.Name {
				app.Status.PodName = newPod.Name
				if err = r.Status().Update(ctx, app); err != nil {
					log.Error(err, "unable to update app")
					return
				}
			}

			return
		}

		app.Status.PodName = ""
		if err = r.transitPhaseTo(ctx, log, app, appcorev1.CliAppPhaseRecovering); err != nil {
			return
		}

		return

	case appcorev1.CliAppPhaseRecovering:
		specHash := computeHash(&app.Spec)
		specDump := encodeSpec(&app.Spec)
		var newPod *corev1.Pod
		newPod, err = r.claimPods(ctx, log, app, specDump, specHash)
		if err != nil {
			return
		}

		if newPod != nil {
			if utils.IsPodReady(newPod) {
				app.Status.PodName = newPod.Name
				if err = r.transitPhaseTo(ctx, log, app, appcorev1.CliAppPhaseLive); err != nil {
					return
				}
				return
			}

			log.Info("wait for pod to be ready", "pod", newPod.Name)
			return
		}

		log.Info("create pod")
		_, err = r.startApp(ctx, app, log, specDump, specHash)
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

func computeHash(spec *appcorev1.CliAppSpec) string {
	podTemplateSpecHasher := fnv.New32a()
	deepHashObject(podTemplateSpecHasher, *spec)
	return rand.SafeEncodeString(fmt.Sprint(podTemplateSpecHasher.Sum32()))
}

func encodeSpec(spec *appcorev1.CliAppSpec) string {
	bytes, err := json.Marshal(spec)
	if err != nil {
		panic(err)
	}

	return string(bytes)
}

func deepHashObject(hasher hash.Hash, objectToWrite interface{}) {
	hasher.Reset()
	printer := spew.ConfigState{
		Indent:         " ",
		SortKeys:       true,
		DisableMethods: true,
		SpewKeys:       true,
	}
	printer.Fprintf(hasher, "%#v", objectToWrite)
}

func (r *CliAppReconciler) claimPods(
	ctx context.Context, log logr.Logger, app *appcorev1.CliApp, specDump, specHash string,
) (pod *corev1.Pod, err error) {
	config, err := r.RestClient.ToRESTConfig()
	if err != nil {
		panic(err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return
	}

	podList, err := clientset.CoreV1().Pods(app.Namespace).List(
		ctx, metav1.ListOptions{LabelSelector: fmt.Sprintf("%s=%s", appLabel, app.Name)},
	)
	if err != nil {
		err = xerrors.Errorf(`unable to fetch pods: %s`, err)
		return
	}

	oldPods := make([]*corev1.Pod, 0, len(podList.Items))
	newPods := make([]*corev1.Pod, 0, len(podList.Items))
	for i := range podList.Items {
		pod := &podList.Items[i]
		if pod.DeletionTimestamp != nil {
			continue
		}

		if len(pod.Annotations) == 0 ||
			pod.Annotations[annoKeySpecHash] != specHash ||
			pod.Annotations[annoKeySpecDump] == "" {

			oldPods = append(oldPods, pod)
		}

		if pod.Annotations[annoKeySpecDump] != specDump {
			oldPods = append(oldPods, pod)
		} else {
			newPods = append(newPods, pod)
		}
	}

	if len(newPods) > 1 {
		log.Info("more than 1 new pods are found and will be terminated except the first one",
			"numPods", len(newPods))
		oldPods = append(oldPods, newPods[1:]...)
	}

	for _, pod := range oldPods {
		log.Info("recycle old pod", "pod", pod.Name)
		if err := clientset.CoreV1().Pods(app.Namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{}); err != nil {
			log.Error(err, "unable to delete old pod", "pod", pod.Name)
		}
	}

	if len(newPods) > 0 {
		pod = newPods[0]
	}

	return
}
