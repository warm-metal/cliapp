//go:generate protoc -I../../pkg/session ../../pkg/session/rpc.proto --go_out=plugins=grpc:../../pkg/session

package main

import (
	"flag"
	"github.com/warm-metal/cliapp/pkg/gate"
	"google.golang.org/grpc"
	_ "google.golang.org/grpc/grpclog"
	"k8s.io/klog/v2"
	"net"
)

var addr = flag.String("addr", ":8001", "TCP address to listen on")

func init() {
	klog.InitFlags(flag.CommandLine)
}

func main() {
	flag.Parse()
	klog.LogToStderr(true)
	defer klog.Flush()
	s := grpc.NewServer()
	gate.PrepareGate(s)

	l, err := net.Listen("tcp", *addr)
	if err != nil {
		panic(err)
	}

	if err = s.Serve(l); err != nil {
		panic(err)
	}
}
