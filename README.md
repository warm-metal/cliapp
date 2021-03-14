# CliApp

*cliapp* is a framework which provides a capability of executing a CLI program in a k8s cluster via a local shortcut,
especially for minikube clusters running on hypervisor.

That is, considering a single node minikube cluster, by installing a cliapp named `ctr`,
we can run command ctr on the Host to check status of containerd on the single cluster node.

We can also export the cliapp list and import them to another cluster w/ CliApp installed.

Currently, cliapp only supports **containerd** as the container runtime.

![overview](https://github.com/warm-metal/official-site/blob/master/image/cliapp-overview.gif?raw=true)

## Install

We also build a kubectl plugin [kubectl-dev](https://github.com/warm-metal/kubectl-dev#install) to support cliapp management. 

## CliApp Object

A cliapp object looks like below.
Execute `kubectl get cliapp --all-namespaces` could retrieve all installed cliapps.

```yaml
apiVersion: core.cliapp.warm-metal.tech/v1
kind: CliApp
metadata:
  creationTimestamp: "2021-03-14T14:23:32Z"
  generation: 2
  managedFields:
  - apiVersion: core.cliapp.warm-metal.tech/v1
    fieldsType: FieldsV1
    fieldsV1:
      f:spec:
        .: {}
        f:command: {}
        f:env: {}
        f:hostpath: {}
        f:image: {}
    manager: kubectl-dev
    operation: Update
    time: "2021-03-14T14:23:32Z"
  - apiVersion: core.cliapp.warm-metal.tech/v1
    fieldsType: FieldsV1
    fieldsV1:
      f:status:
        .: {}
        f:lastPhaseTransition: {}
        f:phase: {}
        f:podName: {}
    manager: manager
    operation: Update
    time: "2021-03-14T14:23:32Z"
  - apiVersion: core.cliapp.warm-metal.tech/v1
    fieldsType: FieldsV1
    fieldsV1:
      f:spec:
        f:targetPhase: {}
    manager: session-gate
    operation: Update
    time: "2021-03-14T14:23:35Z"
  name: crictl
  namespace: default
  resourceVersion: "404083"
  uid: 5fb3d41b-59ad-44a8-bceb-c76638e7ff5d
spec:
  command:
  - crictl
  env:
  - CONTAINER_RUNTIME_ENDPOINT=unix:///var/run/containerd/containerd.sock
  - http_proxy=http://192.168.64.1:1087
  - HTTP_PROXY=http://192.168.64.1:1087
  - https_proxy=http://192.168.64.1:1087
  - HTTPS_PROXY=http://192.168.64.1:1087
  - no_proxy=localhost,127.0.0.1,10.0.0.0/8,192.168.0.0/16,172.16.0.0/12
  - NO_PROXY=localhost,127.0.0.1,10.0.0.0/8,192.168.0.0/16,172.16.0.0/12
  hostpath:
  - /var/run/containerd/containerd.sock
  image: docker.io/warmmetal/app-crictl:v0.1.0
  targetPhase: Rest
status:
  lastPhaseTransition: "2021-03-14T14:33:35Z"
  phase: ShuttingDown
  podName: crictl-88hgl
```