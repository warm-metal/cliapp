package controllers

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	appcorev1 "github.com/warm-metal/cliapp/pkg/apis/cliapp/v1"
	"golang.org/x/xerrors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"path/filepath"
	"strings"
)

const (
	appLabel = "cliapp.warm-metal.tech"
)

func (r *CliAppReconciler) startApp(ctx context.Context, app *appcorev1.CliApp, log logr.Logger) (err error) {
	var manifest *corev1.Pod
	targetContainerID := 0
	if len(app.Spec.ForkObject) > 0 {
		manifest, targetContainerID, err = r.fetchForkTargetPod(app.Namespace, app.Spec.ForkObject, app.Spec.ForkContainer)
	} else {
		manifest, err = r.convertToManifest(app)
		if err != nil {
			log.Error(err, "unable to generate manifest", "spec", app.Spec)
			return err
		}
	}

	err = r.applyAppConfig(manifest, targetContainerID, app)
	if err != nil {
		return err
	}

	// FIXME watch all these pods and repair them on fail
	if err = r.Create(ctx, manifest); err != nil {
		log.Error(err, "unable to create pod")
	}

	return err
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
				{},
			},
		},
	}, nil
}

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

	ctxImage := r.AppContextImage
	if len(ctxImage) == 0 {
		sh := appcorev1.CliAppShellBash
		distro := appcorev1.CliAppDistroAlpine
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

	targetContainer := pod.Spec.Containers[targetContainerID]
	targetContainer.Name = appContainer
	targetContainer.Env = append(targetContainer.Env, corev1.EnvVar{
		Name:  "APP_ROOT",
		Value: appRoot,
	})
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

	if len(ctxImage) > 0 {
		targetContainer.Image = ctxImage
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
						"image": app.Spec.Image,
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
