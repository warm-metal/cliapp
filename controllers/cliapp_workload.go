package controllers

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	appcorev1 "github.com/warm-metal/cliapp/pkg/apis/cliapp/v1"
	"golang.org/x/xerrors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"path/filepath"
	"strings"
)

const (
	appLabel = "cliapp.warm-metal.tech"
)

func (r *CliAppReconciler) startApp(
	ctx context.Context, app *appcorev1.CliApp, log logr.Logger,
) (pod *corev1.Pod, err error) {
	targetContainerID := 0
	if app.Spec.Fork != nil {
		pod, targetContainerID, err = r.fetchForkTargetPod(app.Namespace, app.Spec.Fork)
	} else {
		pod, err = r.convertToManifest(app)
	}

	if err != nil {
		log.Error(err, "unable to generate pod manifest", "spec", app.Spec)
		return
	}

	if err = r.applyAppConfig(pod, targetContainerID, app); err != nil {
		return
	}

	log.Info("create pod", "pod", pod.Name, "namespace", pod.Namespace, "labels", pod.Labels)
	if err = r.Create(ctx, pod); err != nil {
		log.Error(err, "unable to create pod")
	}

	return pod, err
}

const (
	shellContextSyncImage = "docker.io/warmmetal/f2cm:v0.1.0"
	appContextImage       = "docker.io/warmmetal/app-context-%s-%s:v0.1.0"
	shellContextSidecar   = "shell-context-sync"
	appContainer          = "workspace"
	appRoot               = "/app-root"
	appImageVolume        = "app"
	csiDriverName         = "csi-image.warm-metal.tech"
)

func (r *CliAppReconciler) convertToManifest(app *appcorev1.CliApp) (*corev1.Pod, error) {
	return &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Image: app.Spec.Image,
				},
			},
		},
	}, nil
}

var enabled = true

func (r *CliAppReconciler) applyAppConfig(pod *corev1.Pod, targetContainerID int, app *appcorev1.CliApp) error {
	var hostVolumes []corev1.Volume
	var hostMounts []corev1.VolumeMount

	for i, path := range app.Spec.HostPath {
		mountPair := strings.Split(strings.TrimSpace(path), ":")
		if len(mountPair) == 0 {
			return xerrors.Errorf("invalid hostpath spec")
		}

		hostpath := strings.TrimSpace(mountPair[0])
		if !filepath.IsAbs(hostpath) {
			return xerrors.Errorf("hostpath can't be empty")
		}

		mountpoint := hostpath
		if len(mountPair) > 1 {
			mountpoint = strings.TrimSpace(mountPair[1])
			if !filepath.IsAbs(mountpoint) {
				return xerrors.Errorf("mountpoint must be an absolute path")
			}
		}

		volume := fmt.Sprintf("hostpath-%d", i)
		hostVolumes = append(hostVolumes, corev1.Volume{
			Name: volume,
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: hostpath,
				},
			},
		})
		hostMounts = append(hostMounts, corev1.VolumeMount{
			Name:      volume,
			MountPath: mountpoint,
		})
	}

	sharedCtx := corev1.VolumeMount{
		Name:      "shell-context",
		MountPath: "/root",
	}

	var envs []corev1.EnvVar
	for _, kv := range app.Spec.Env {
		envPair := strings.Split(kv, "=")
		if len(envPair) != 2 {
			return xerrors.Errorf(`environment variable must be in the form of "key=value"`)
		}

		env := corev1.EnvVar{
			Name:  strings.TrimSpace(envPair[0]),
			Value: strings.TrimSpace(envPair[1]),
		}

		if len(env.Name) == 0 {
			return xerrors.Errorf(`the key of environment variable must be not empty`)
		}

		envs = append(envs, env)
	}

	sh := r.DefaultShell
	distro := r.DefaultDistro
	ctxImage := r.DefaultAppContextImage
	if len(ctxImage) == 0 {
		if len(app.Spec.Shell) > 0 {
			sh = app.Spec.Shell
		}

		if len(app.Spec.Distro) > 0 {
			distro = app.Spec.Distro
		}

		ctxImage = fmt.Sprintf(appContextImage, strings.ToLower(string(sh)), strings.ToLower(string(distro)))
	}

	pod.ObjectMeta.Name = fmt.Sprintf("%s-%s", app.Name, rand.String(5))
	pod.ObjectMeta.Namespace = app.Namespace
	pod.ObjectMeta.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion:         app.APIVersion,
			Kind:               app.Kind,
			Name:               app.Name,
			UID:                app.UID,
			Controller:         &enabled,
			BlockOwnerDeletion: &enabled,
		},
	}
	if pod.ObjectMeta.Labels == nil {
		pod.ObjectMeta.Labels = map[string]string{appLabel: app.Name}
	} else {
		pod.ObjectMeta.Labels[appLabel] = app.Name
	}

	pod.Spec.InitContainers = append(pod.Spec.InitContainers, corev1.Container{
		Name:         "shell-context-initializer",
		Image:        shellContextSyncImage,
		Args:         []string{fmt.Sprintf("%s/%s=>/root", r.ControllerNamespace, ShellContextConfigMap)},
		VolumeMounts: []corev1.VolumeMount{sharedCtx},
	})

	targetContainer := &pod.Spec.Containers[targetContainerID]

	// exchange the target image
	targetImage := targetContainer.Image
	targetContainer.Image = ctxImage

	// update the target container name
	targetContainer.Name = appContainer

	// append envs
	targetContainer.Env = append(targetContainer.Env, corev1.EnvVar{
		Name:  "APP_ROOT",
		Value: appRoot,
	}, corev1.EnvVar{
		Name:  "DISTRO",
		Value: string(distro),
	}, corev1.EnvVar{
		Name:  "SHELL",
		Value: string(sh),
	})

	targetContainer.Env = append(targetContainer.Env, envs...)

	targetContainer.Stdin = true
	targetContainer.VolumeMounts = append(targetContainer.VolumeMounts, hostMounts...)
	targetContainer.VolumeMounts = append(targetContainer.VolumeMounts, sharedCtx,
		corev1.VolumeMount{
			Name:      appImageVolume,
			MountPath: appRoot,
		})
	if targetContainer.SecurityContext == nil {
		targetContainer.SecurityContext = &corev1.SecurityContext{
			Capabilities: &corev1.Capabilities{
				Add: []corev1.Capability{"SYS_ADMIN"},
			},
		}
	} else {
		if targetContainer.SecurityContext.Capabilities == nil {
			targetContainer.SecurityContext.Capabilities = &corev1.Capabilities{
				Add: []corev1.Capability{"SYS_ADMIN"},
			}
		} else {
			targetContainer.SecurityContext.Capabilities.Add = append(targetContainer.SecurityContext.Capabilities.Add,
				"SYS_ADMIN")
		}
	}

	pod.Spec.Containers = append(pod.Spec.Containers, corev1.Container{
		Name:  shellContextSidecar,
		Image: shellContextSyncImage,
		Args: []string{
			"-w",
			fmt.Sprintf("/root=>%s/%s", r.ControllerNamespace, ShellContextConfigMap),
		},
		VolumeMounts: []corev1.VolumeMount{sharedCtx},
	})

	pod.Spec.Volumes = append(pod.Spec.Volumes, hostVolumes...)
	pod.Spec.Volumes = append(pod.Spec.Volumes,
		corev1.Volume{
			Name: appImageVolume,
			VolumeSource: corev1.VolumeSource{
				CSI: &corev1.CSIVolumeSource{
					Driver: csiDriverName,
					VolumeAttributes: map[string]string{
						"image": targetImage,
					},
				},
			},
		},
		corev1.Volume{
			Name: "shell-context",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
	return nil
}
