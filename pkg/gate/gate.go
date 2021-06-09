package gate

import (
	"context"
	appcorev1 "github.com/warm-metal/cliapp/pkg/apis/cliapp/v1"
	appv1 "github.com/warm-metal/cliapp/pkg/clientset/versioned"
	rpc "github.com/warm-metal/cliapp/pkg/session"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"io"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/client-go/util/exec"
	"k8s.io/klog/v2"
	"sync"
	"time"
)

func PrepareGate(s *grpc.Server) {
	gate := terminalGate{
		sessionMap: make(map[types.NamespacedName]*appSession),
	}
	gate.init()
	rpc.RegisterAppGateServer(s, &gate)
}

type terminalGate struct {
	config    *rest.Config
	clientset *kubernetes.Clientset
	appClient *appv1.Clientset

	sessionMap   map[types.NamespacedName]*appSession
	sessionGuard sync.Mutex
}

func timeoutContext(parent ...context.Context) (context.Context, context.CancelFunc) {
	if len(parent) > 1 {
		panic(len(parent))
	}

	if len(parent) > 0 {
		return context.WithTimeout(parent[0], 5*time.Second)
	} else {
		return context.WithTimeout(context.TODO(), 5*time.Second)
	}
}

func (t *terminalGate) init() {
	if err := appcorev1.AddToScheme(scheme.Scheme); err != nil {
		panic(err)
	}

	var err error
	t.config, err = rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}

	t.clientset, err = kubernetes.NewForConfig(t.config)
	if err != nil {
		panic(err.Error())
	}

	t.appClient, err = appv1.NewForConfig(t.config)
	if err != nil {
		return
	}

	_, err = t.appClient.CliappV1().CliApps(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		panic(err)
	}
}

func (t *terminalGate) attach(app *appcorev1.CliApp, cmd []string, in *clientReader, stdout io.Writer) (err error) {
	opts := &corev1.PodExecOptions{
		Container: "workspace",
		Stdin:     true,
		Stdout:    true,
		Stderr:    false,
		TTY:       true,
	}

	if len(app.Spec.Command) > 0 {
		opts.Command = append([]string{"chroot", "/app-root"}, append(app.Spec.Command, cmd...)...)
	} else {
		// For debug command, the cmd usually is bash or zsh.
		opts.Command = cmd
	}

	req := t.clientset.CoreV1().RESTClient().Post().
		Resource("pods").Name(app.Status.PodName).Namespace(app.Namespace).
		SubResource("exec").
		VersionedParams(opts, scheme.ParameterCodec)

	remoteExec, err := remotecommand.NewSPDYExecutor(t.config, "POST", req.URL())
	if err != nil {
		klog.Errorf("can't create executor: %s", err)
		return
	}

	klog.Infof("open session to Pod %s/%s", app.Namespace, app.Status.PodName)

	err = remoteExec.Stream(remotecommand.StreamOptions{
		Stdin:             in,
		Stdout:            stdout,
		Tty:               true,
		TerminalSizeQueue: in,
	})

	return
}

func (t *terminalGate) OpenShell(s rpc.AppGate_OpenShellServer) error {
	req, err := s.Recv()
	if err != nil {
		klog.Errorf("can't receive date from client: %s", err)
		return status.Error(codes.Unavailable, err.Error())
	}

	if req.App.Name == "" {
		return status.Error(codes.InvalidArgument, "App.Name is required in the first request.")
	}
	if req.App.Namespace == "" {
		return status.Error(codes.InvalidArgument, "App.Namespace is required in the first request.")
	}

	sessionKey := types.NamespacedName{
		Namespace: req.App.Namespace,
		Name:      req.App.Name,
	}

	t.sessionGuard.Lock()

	session := t.sessionMap[sessionKey]
	if session == nil {
		session = &appSession{appClient: t.appClient}
		t.sessionMap[sessionKey] = session
	}

	t.sessionGuard.Unlock()

	defer func() {
		if err := session.close(s.Context(), &sessionKey); err != nil {
			klog.Errorf("unable to close session of app %s: %s", &sessionKey, err)
		}

		klog.Infof("app %s closed", &sessionKey)
	}()

	klog.Infof("open app %s", &sessionKey)
	app, err := session.open(s.Context(), newProgressWriter(s), &sessionKey)
	if err != nil {
		klog.Errorf("unable to open app %s: %s", &sessionKey, err)
		return status.Error(codes.Unavailable, err.Error())
	}

	stdin, stdout := genClientIOStreams(s, req.TerminalSize)
	defer stdin.Close()

	if err = t.attach(app, req.Input, stdin, stdout); err != nil {
		if details, ok := err.(exec.CodeExitError); ok {
			klog.Errorf("unable to open stream of app %s: %s", &sessionKey, details.Err.Error())
			return status.Errorf(codes.Aborted, "%d", details.Code)
		} else {
			klog.Errorf("unable to open stream of app %s: %#v", &sessionKey, err)
			return status.Error(codes.Unavailable, err.Error())
		}
	}

	return nil
}
