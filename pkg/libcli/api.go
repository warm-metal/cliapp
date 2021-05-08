package libcli

import (
	"context"
	"fmt"
	"github.com/moby/term"
	appcorev1 "github.com/warm-metal/cliapp/pkg/apis/cliapp/v1"
	rpc "github.com/warm-metal/cliapp/pkg/session"
	"golang.org/x/sys/unix"
	"golang.org/x/xerrors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"io"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/exec"
	"net/url"
	"os"
	"os/signal"
	"strconv"
)

func FetchGateEndpoints(ctx context.Context, clientset *kubernetes.Clientset) ([]string, error) {
	return fetchServiceEndpoints(
		ctx, clientset, "cliapp-system", "cliapp-session-gate", "session-gate",
	)
}

func fetchServiceEndpoints(
	ctx context.Context, clientset *kubernetes.Clientset, namespace, service, port string,
) (addrs []string, err error) {
	svc, err := clientset.CoreV1().Services(namespace).
		Get(ctx, service, metav1.GetOptions{})
	if err != nil {
		return nil, xerrors.Errorf(
			`can't fetch endpoint from Service "%s/%s": %s`, namespace, service, err)
	}

	svcPort := int32(0)
	nodePort := int32(0)
	for _, p := range svc.Spec.Ports {
		if p.Name != port {
			continue
		}

		svcPort = p.Port
		nodePort = p.NodePort
	}

	if svcPort > 0 {
		for _, ingress := range svc.Status.LoadBalancer.Ingress {
			if len(ingress.Hostname) > 0 {
				addrs = append(addrs, fmt.Sprintf("tcp://%s:%d", ingress.Hostname, svcPort))
			}

			if len(ingress.IP) > 0 {
				addrs = append(addrs, fmt.Sprintf("tcp://%s:%d", ingress.IP, svcPort))
			}
		}
	}

	if nodePort > 0 {
		nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, xerrors.Errorf(`can't list node while enumerating Service NodePort: %s`, err)
		}

		for _, node := range nodes.Items {
			for _, addr := range node.Status.Addresses {
				if len(addr.Address) > 0 {
					addrs = append(addrs, fmt.Sprintf("tcp://%s:%d", addr.Address, nodePort))
				}
			}
		}
	}

	addrs = append(addrs, fmt.Sprintf("tcp://%s:%d", svc.Spec.ClusterIP, svcPort))
	return
}

func ExecCliApp(ctx context.Context, endpoints []string, app *appcorev1.CliApp, args []string, stdin io.Reader, stdout io.Writer) error {
	var cc *grpc.ClientConn
	for i, ep := range endpoints {
		endpoint, err := url.Parse(ep)
		if err != nil {
			panic(err)
		}
		cc, err = grpc.DialContext(ctx, endpoint.Host, grpc.WithInsecure(), grpc.WithBlock())
		if err == nil {
			break
		}

		fmt.Fprintf(os.Stderr, `can't connect to app session gate "%s": %s`+"\n", endpoint.Host, err)
		i++
		if i < len(endpoints) {
			fmt.Fprintf(os.Stderr, `Try the next endpoint %s`+"\n", endpoints[i])
		}
	}

	if cc == nil {
		return xerrors.Errorf("all remote endpoints are unavailable")
	}

	stdInFd, stdinIsTerminal := term.GetFdInfo(stdin)
	stdOutFd, stdOutIsTerminal := term.GetFdInfo(stdout)

	sh, err := rpc.NewAppGateClient(cc).OpenShell(ctx)
	if err != nil {
		return xerrors.Errorf("unable to open app session: %s", err)
	}

	var initTermSize *rpc.TerminalSize
	if stdOutIsTerminal {
		initTermSize = getSize(stdOutFd)
	}

	err = sh.Send(&rpc.StdIn{
		App: &rpc.App{
			Name:      app.Name,
			Namespace: app.Namespace,
		},
		Input:        args,
		TerminalSize: initTermSize,
	})

	if err != nil {
		return xerrors.Errorf("unable to open app: %s", err)
	}

	errCh := make(chan error)
	defer close(errCh)

	inCh := make(chan string)

	go func() {
		defer close(inCh)

		buf := make([]byte, 1024)
		for {
			n, err := stdin.Read(buf)
			if err != nil {
				if err == io.EOF {
					return
				}

				errCh <- xerrors.Errorf("can't read the input:%s", err)
				return
			}

			inCh <- string(buf[:n])
		}
	}()

	rawOutCh := make(chan string)
	go func() {
		defer close(rawOutCh)
		for {
			resp, err := sh.Recv()
			if err != nil {
				if err == io.EOF {
					errCh <- err
					return
				}

				st, ok := status.FromError(err)
				if ok && st.Code() == codes.Aborted {
					if code, failed := strconv.Atoi(st.Message()); failed == nil {
						errCh <- exec.CodeExitError{Code: code, Err: err}
						return
					}
				}

				errCh <- xerrors.Errorf("can't read the remote response:%s", err)
				return
			}

			if len(resp.Output) > 0 {
				if resp.Raw {
					rawOutCh <- string(resp.Output)
				} else {
					fmt.Print(string(resp.Output))
				}
			}
		}
	}()

	winch := make(chan os.Signal, 1)
	signal.Notify(winch, unix.SIGWINCH)
	defer signal.Stop(winch)
	defer close(winch)

	var originalStdinState *term.State
	defer func() {
		if originalStdinState != nil {
			term.RestoreTerminal(stdInFd, originalStdinState)
		}
	}()

	for {
		select {
		case err := <-errCh:
			if err == io.EOF {
				return nil
			}

			return err
		case <-winch:
			if !stdOutIsTerminal {
				break
			}

			size := getSize(stdOutFd)
			if err = sh.Send(&rpc.StdIn{TerminalSize: size}); err != nil {
				return err
			}
		case <-ctx.Done():
			return nil
		case in, ok := <-inCh:
			if ok {
				if err = sh.Send(&rpc.StdIn{Input: []string{in}}); err != nil {
					return err
				}
			}
		case out, ok := <-rawOutCh:
			if ok {
				// Once the first stdout received, the shell session is actually opened.
				// Before that, users also could exit the command by sent an interrupt.
				if stdinIsTerminal && originalStdinState == nil {
					var err error
					originalStdinState, err = term.MakeRaw(stdInFd)
					if err != nil {
						return xerrors.Errorf("can't initialize terminal: %s", err)
					}
				}

				fmt.Fprint(stdout, out)
			}
		}
	}
}

func getSize(fd uintptr) *rpc.TerminalSize {
	winsize, err := term.GetWinsize(fd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to get terminal size: %v", err)
		return nil
	}

	return &rpc.TerminalSize{Width: uint32(winsize.Width), Height: uint32(winsize.Height)}
}
