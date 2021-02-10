package controllers

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	appcorev1 "github.com/warm-metal/cliapp/api/v1"
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

func (r *CliAppReconciler) startApp(ctx context.Context, app *appcorev1.CliApp, log logr.Logger) error {
	// FIXME watch all these pods and repair them on fail
	manifest, err := r.convertToManifest(app)
	if err != nil {
		log.Error(err, "unable to generate manifest", "spec", app.Spec)
		return err
	}

	if err = r.Create(ctx, manifest); err != nil {
		log.Error(err, "unable to create pod")
	}

	return err
}

const (
	// FIXME make images below configurable
	shellContextSyncImage = "docker.io/warmmetal/f2cm:v0.1.0"
	appContextImage       = "docker.io/warmmetal/app-context:v0.1.0"
	shellContextSidecar   = "shell-context-sync"
	appContainer          = "workspace"
	appRoot               = "/app-root"
	appImageVolume        = "app"
	csiDriverName         = "csi-image.warm-metal.tech"
)

func (r *CliAppReconciler) convertToManifest(app *appcorev1.CliApp) (*corev1.Pod, error) {
	var hostVolumes []corev1.Volume
	var hostMounts []corev1.VolumeMount

	for i, path := range app.Spec.HostPath {
		mountPair := strings.Split(strings.TrimSpace(path), ":")
		if len(mountPair) == 0 {
			return nil, xerrors.Errorf("invalid hostpath spec")
		}

		hostpath := strings.TrimSpace(mountPair[0])
		if !filepath.IsAbs(hostpath) {
			return nil, xerrors.Errorf("hostpath can't be empty")
		}

		mountpoint := hostpath
		if len(mountPair) > 1 {
			mountpoint = strings.TrimSpace(mountPair[1])
			if !filepath.IsAbs(mountpoint) {
				return nil, xerrors.Errorf("mountpoint must be an absolute path")
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

	// FIXME HTTP proxy configuration
	sharedCtx := corev1.VolumeMount{
		Name:      "shell-context",
		MountPath: "/root",
	}

	var envs []corev1.EnvVar
	for _, kv := range app.Spec.Env {
		envPair := strings.Split(kv, "=")
		if len(envPair) != 2 {
			return nil, xerrors.Errorf(`environment variable must be in the form of "key=value"`)
		}

		env := corev1.EnvVar{
			Name:  strings.TrimSpace(envPair[0]),
			Value: strings.TrimSpace(envPair[1]),
		}

		if len(env.Name) == 0 {
			return nil, xerrors.Errorf(`the key of environment variable must be not empty`)
		}

		envs = append(envs, env)
	}

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", app.Name, rand.String(5)),
			Namespace: app.Namespace,
			Labels:    map[string]string{appLabel: app.Name},
		},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{
				{
					Name:         "shell-context-initializer",
					Image:        shellContextSyncImage,
					Args:         []string{fmt.Sprintf("%s/%s=>/root", r.ControllerNamespace, ShellContextConfigMap)},
					VolumeMounts: []corev1.VolumeMount{sharedCtx},
				},
			},
			Containers: []corev1.Container{
				{
					Name:  appContainer,
					Image: appContextImage,
					Env: append(envs, corev1.EnvVar{
						Name:  "APP_ROOT",
						Value: appRoot,
					}),
					Stdin: true,
					VolumeMounts: append(
						hostMounts,
						sharedCtx,
						corev1.VolumeMount{
							Name:      appImageVolume,
							MountPath: appRoot,
						},
					),
					SecurityContext: &corev1.SecurityContext{
						Capabilities: &corev1.Capabilities{
							Add: []corev1.Capability{"SYS_ADMIN"},
						},
					},
				},
				{
					Name:  shellContextSidecar,
					Image: shellContextSyncImage,
					Args: []string{
						"-w",
						fmt.Sprintf("/root=>%s/%s", r.ControllerNamespace, ShellContextConfigMap),
					},
					VolumeMounts: []corev1.VolumeMount{sharedCtx},
				},
			},
			Volumes: append(hostVolumes,
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
				},
			),
		},
	}, nil
}
