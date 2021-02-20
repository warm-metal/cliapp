package controllers

import (
	"context"
	"golang.org/x/xerrors"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/scale/scheme/extensionsv1beta1"
)

func (r *CliAppReconciler) fetchForkTargetPod(
	namespace string, kindAndName string, container string,
) (pod *corev1.Pod, target int, err error) {
	result := resource.NewBuilder(r.RestClient).
		Unstructured().
		ContinueOnError().
		NamespaceParam(namespace).DefaultNamespace().
		ResourceTypeOrNameArgs(true, kindAndName).
		SingleResourceType().
		Flatten().
		Do()
	if result.Err() != nil {
		err = xerrors.Errorf(`unable to fetch "%s": %s`, kindAndName, result.Err())
		return
	}

	infos, err := result.Infos()
	if err != nil {
		err = xerrors.Errorf(`unable to fetch result of "%s": %s`, kindAndName, result.Err())
		return
	}

	if len(infos) == 0 {
		err = xerrors.Errorf(`no "%#v" found`, kindAndName)
		return
	}

	if len(infos) > 1 {
		panic(infos)
	}

	config, err := r.RestClient.ToRESTConfig()
	if err != nil {

	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return
	}

	ctx := context.TODO()
	opt := metav1.GetOptions{}
	info := infos[0]
	var podTmpl *corev1.PodSpec
	var labels map[string]string
	switch info.Mapping.GroupVersionKind.Kind {
	case "Deployment":
		switch info.Mapping.GroupVersionKind.Group {
		case extensionsv1beta1.GroupName:
			deploy, failed := clientset.ExtensionsV1beta1().
				Deployments(info.Namespace).
				Get(ctx, info.Name, opt)
			if failed != nil {
				err = xerrors.Errorf("can't fetch %s/%s: %s", info.Mapping.GroupVersionKind, info.Name, failed)
				return
			}

			podTmpl = &deploy.Spec.Template.Spec
			labels = deploy.Spec.Template.Labels
		case appsv1.GroupName:
			deploy, failed := clientset.AppsV1().Deployments(info.Namespace).Get(ctx, info.Name, opt)
			if failed != nil {
				err = xerrors.Errorf("can't fetch %s/%s: %s", info.Mapping.GroupVersionKind, info.Name, failed)
				return
			}

			podTmpl = &deploy.Spec.Template.Spec
			labels = deploy.Spec.Template.Labels
		default:
			panic(info.Mapping.GroupVersionKind)
		}
	case "StatefulSet":
		if info.Mapping.GroupVersionKind.Group != appsv1.GroupName {
			panic(infos[0].Mapping.GroupVersionKind)
		}

		sfs, failed := clientset.AppsV1().StatefulSets(info.Namespace).Get(ctx, info.Name, opt)
		if failed != nil {
			err = xerrors.Errorf("can't fetch %s/%s: %s", info.Mapping.GroupVersionKind, info.Name, failed)
			return
		}

		podTmpl = &sfs.Spec.Template.Spec
		labels = sfs.Spec.Template.Labels
	case "Job":
		if info.Mapping.GroupVersionKind.Group != batchv1.GroupName {
			panic(infos[0].Mapping.GroupVersionKind)
		}

		job, failed := clientset.BatchV1().Jobs(info.Namespace).Get(ctx, info.Name, opt)
		if failed != nil {
			err = xerrors.Errorf("can't fetch %s/%s: %s", info.Mapping.GroupVersionKind, info.Name, failed)
			return
		}

		podTmpl = &job.Spec.Template.Spec
		labels = job.Spec.Template.Labels
	case "CronJob":
		if info.Mapping.GroupVersionKind.Group != batchv1.GroupName {
			panic(info.Mapping.GroupVersionKind)
		}

		job, failed := clientset.BatchV1beta1().CronJobs(info.Namespace).Get(ctx, info.Name, opt)
		if failed != nil {
			err = xerrors.Errorf("can't fetch %s/%s: %s", info.Mapping.GroupVersionKind, info.Name, err)
			return
		}

		podTmpl = &job.Spec.JobTemplate.Spec.Template.Spec
		labels = job.Spec.JobTemplate.Spec.Template.Labels
	case "DaemonSet":
		switch info.Mapping.GroupVersionKind.Group {
		case extensionsv1beta1.GroupName:
			ds, failed := clientset.ExtensionsV1beta1().DaemonSets(info.Namespace).Get(ctx, info.Name, opt)
			if failed != nil {
				err = xerrors.Errorf("can't fetch %s/%s: %s", info.Mapping.GroupVersionKind, info.Name, failed)
				return
			}

			podTmpl = &ds.Spec.Template.Spec
			labels = ds.Spec.Template.Labels
		case appsv1.GroupName:
			ds, failed := clientset.AppsV1().DaemonSets(info.Namespace).Get(ctx, info.Name, opt)
			if failed != nil {
				err = xerrors.Errorf("can't fetch %s/%s: %s", info.Mapping.GroupVersionKind, info.Name, failed)
				return
			}

			podTmpl = &ds.Spec.Template.Spec
			labels = ds.Spec.Template.Labels
		default:
			panic(info.Mapping.GroupVersionKind)
		}
	case "ReplicaSet":
		switch info.Mapping.GroupVersionKind.Group {
		case extensionsv1beta1.GroupName:
			rs, failed := clientset.ExtensionsV1beta1().ReplicaSets(info.Namespace).Get(ctx, info.Name, opt)
			if failed != nil {
				err = xerrors.Errorf("can't fetch %s/%s: %s", info.Mapping.GroupVersionKind, info.Name, failed)
				return
			}

			podTmpl = &rs.Spec.Template.Spec
			labels = rs.Spec.Template.Labels
		case appsv1.GroupName:
			rs, failed := clientset.AppsV1().ReplicaSets(info.Namespace).Get(ctx, info.Name, opt)
			if failed != nil {
				err = xerrors.Errorf("can't fetch %s/%s: %s", info.Mapping.GroupVersionKind, info.Name, failed)
				return
			}

			podTmpl = &rs.Spec.Template.Spec
			labels = rs.Spec.Template.Labels
		default:
			panic(info.Mapping.GroupVersionKind)
		}
	case "Pod":
		if info.Mapping.GroupVersionKind.Group != corev1.GroupName {
			panic(info.Mapping.GroupVersionKind)
		}

		po, failed := clientset.CoreV1().Pods(info.Namespace).Get(ctx, info.Name, opt)
		if failed != nil {
			err = xerrors.Errorf("can't fetch %s/%s: %s", info.Mapping.GroupVersionKind, info.Name, failed)
			return
		}

		podTmpl = &po.Spec
		labels = po.Labels
	default:
		err = xerrors.Errorf("object %s/%s is not supported", info.Mapping.GroupVersionKind, info.Name)
		return
	}

	for i := range podTmpl.Containers {
		container := &podTmpl.Containers[i]
		container.StartupProbe = nil
		container.LivenessProbe = nil
		container.ReadinessProbe = nil
	}

	pod = &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels: labels,
		},
		Spec: *podTmpl,
	}

	if len(container) > 1 {
		target = -1
		for i, c := range pod.Spec.Containers {
			if c.Name == container {
				target = i
				break
			}
		}

		if target < 0 {
			err = xerrors.Errorf("container %s doesn't found in %s", container, kindAndName)
			return
		}
	} else if len(pod.Spec.Containers) > 0 {
		err = xerrors.Errorf("%s has more than 1 container. Specify a container name", kindAndName)
		return
	}

	return
}
