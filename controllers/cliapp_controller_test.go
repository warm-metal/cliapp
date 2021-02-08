package controllers

import (
	"context"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	appcorev1 "github.com/warmmetal/cliapp/api/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"time"
)

var _ = Describe("CliApp Controller", func() {
	const (
		appName    = "ctr"
		appImage   = "docker.io/warmmetal/ctr:v1"
		appCommand = "ctr"
		hostPath   = "/var/run/containerd/containerd.sock"
	)
	Context("Create app to live", func() {
		It("Pod should be created", func() {
			By("By creating a new CliApp")
			ctx := context.TODO()
			app := &appcorev1.CliApp{
				TypeMeta: v1.TypeMeta{
					Kind:       "CliApp",
					APIVersion: appcorev1.GroupVersion.String(),
				},
				ObjectMeta: v1.ObjectMeta{
					Name:      appName,
					Namespace: appNamespace,
				},
				Spec: appcorev1.CliAppSpec{
					Image:       appImage,
					Command:     []string{appCommand},
					HostPath:    []string{hostPath},
					TargetPhase: appcorev1.CliAppPhaseLive,
				},
			}

			Expect(k8sClient.Create(ctx, app)).Should(Succeed())

			lookupKey := types.NamespacedName{Name: appName, Namespace: appNamespace}
			createdApp := &appcorev1.CliApp{}

			Eventually(func() bool {
				err := k8sClient.Get(ctx, lookupKey, createdApp)
				if err != nil {
					return false
				}
				return createdApp.Status.Phase == appcorev1.CliAppPhaseLive
			}, time.Minute, 5*time.Second).Should(BeTrue())
		})
	})
})
