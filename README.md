# CliApp

*cliapp* is a framework which provides a capability of executing a CLI program in a k8s cluster via a local shortcut,
especially for minikube clusters running on hypervisor.

That is, considering a single node minikube cluster, by installing a cliapp named `ctr`,
we can run command ctr on the Host to check status of containerd on the single cluster node.

Currently, cliapp only support **containerd** as the container runtime.

## Install

We also build a kubectl plugin [kubectl-dev](https://github.com/warm-metal/kubectl-dev#install) to support cliapp management. 
