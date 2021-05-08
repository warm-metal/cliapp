package gate

import (
	rpc "github.com/warm-metal/cliapp/pkg/session"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"io"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/klog/v2"
)

type clientReader struct {
	s      rpc.AppGate_OpenShellServer
	sizeCh chan *remotecommand.TerminalSize
	stdin  chan string
	closed bool
}

func (r *clientReader) Close() {
	r.closed = true
}

func (r *clientReader) loop() {
	r.stdin = make(chan string)
	defer func() {
		r.closed = true
		close(r.stdin)
		close(r.sizeCh)
	}()

	for {
		if r.closed {
			return
		}

		klog.V(1).Info("prepare to receive stdin")
		req, err := r.s.Recv()
		if err != nil {
			if status.Code(err) != codes.Canceled {
				klog.Errorf("unable to read stdin: %s", err)
			}

			klog.V(1).Infof("unable to read stdin: %s", err)
			return
		}

		klog.V(1).Infof("stdin %s", req.String())

		if req.TerminalSize != nil {
			go func() {
				r.sizeCh <- &remotecommand.TerminalSize{
					Width:  uint16(req.TerminalSize.Width),
					Height: uint16(req.TerminalSize.Height),
				}
			}()
		}

		if len(req.Input) > 0 {
			if len(req.Input) != 1 {
				klog.Errorf("invalid input %#v", req.Input)
				return
			}
			klog.V(1).Info("write stdin channel")
			r.stdin <- req.Input[0]
		}
	}
}

func (r clientReader) Next() *remotecommand.TerminalSize {
	if r.closed {
		return nil
	}

	size, ok := <-r.sizeCh
	if !ok {
		return nil
	}

	return size
}

func (r *clientReader) Read(p []byte) (n int, err error) {
	klog.V(1).Info("read stdin channel")
	in, ok := <-r.stdin
	if !ok {
		err = io.EOF
		return
	}

	if len(p) < len(in) {
		err = io.ErrShortBuffer
		klog.Errorf("buffer too small %d, %d", len(p), len(in))
		return
	}

	klog.V(1).Info("stdin buffers exchange")
	n = copy(p, in)
	return
}

type stdoutWriter struct {
	s rpc.AppGate_OpenShellServer
}

func (w stdoutWriter) Write(p []byte) (n int, err error) {
	klog.V(1).Infof("stdout: %s", string(p))
	err = w.s.Send(&rpc.StdOut{
		Output: p,
		Raw:    true,
	})

	if err != nil {
		klog.Errorf("can't write stdout: %s", err)
		return
	}

	n = len(p)
	return
}

func genClientIOStreams(s rpc.AppGate_OpenShellServer, initSize *rpc.TerminalSize) (reader *clientReader, stdout io.Writer) {
	in := clientReader{s: s, sizeCh: make(chan *remotecommand.TerminalSize, 1)}
	if initSize != nil {
		in.sizeCh <- &remotecommand.TerminalSize{
			Width:  uint16(initSize.Width),
			Height: uint16(initSize.Height),
		}
	}

	go in.loop()
	return &in, &stdoutWriter{s}
}
