/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// CliAppSpec defines the desired state of CliApp
type CliAppSpec struct {
	// Important: Run "make" to regenerate code after modifying this file

	// Specify that the app will fork a workload in the same namespace.
	// +optional
	Fork *ForkObject `json:"fork,omitempty"`

	// Specify the image the app uses.
	// Only one of Image or Dockerfile can be set.
	// +optional
	Image string `json:"image,omitempty"`

	// Specify a Dockerfile to build a image used to run the app. Http(s) URI is also supported.
	// Only one of Image or Dockerfile can be set.
	// +optional
	Dockerfile string `json:"dockerfile,omitempty"`

	// Set the command to be executed when client runs the app.
	// It is usually an executable binary. It should be found in the PATH, or an absolute path to the binary.
	// If no set, session-gate will run commands in the app context rootfs instead of the rootfs of Spec.Image.
	// +optional
	Command []string `json:"command,omitempty"`

	// Host paths would be mounted to the app.
	// Each HostPath can be an absolute host path, or in the form of "hostpath:mount-point".
	// +optional
	HostPath []string `json:"hostpath,omitempty"`

	// Environment variables in the form of "key=value".
	// +optional
	Env []string `json:"env,omitempty"`

	// Distro the app dependents. The default is alpine.
	// +optional
	// Valid values are:
	// - "alpine" (default): The app works on Alpine;
	// - "ubuntu: The app works on Ubuntu.
	Distro CliAppDistro `json:"distro,omitempty"`

	// The shell interpreter you preferred. Can be either bash or zsh.
	// +optional
	// Valid values are:
	// - "bash" (default): The app will run in Bash;
	// - "zsh: The app will run in Zsh.
	Shell CliAppShell `json:"shell,omitempty"`

	// The target phase the app should achieve.
	// Valid values are:
	// - "Rest" (default): The app is installed but not started;
	// - "Live": The app is running.
	TargetPhase CliAppPhase `json:"targetPhase,omitempty"`

	// Set if uninstalls the App when it transits out of phase Live
	// +optional
	UninstallUnlessLive bool `json:"uninstall,omitempty"`
}

type ForkObject struct {
	// Specify the kind and name of the object to be forked.
	// The object could be either of Deployment, StatefulSet, DaemonSet, ReplicaSet, (Cron)Job, or Pod.
	// The valid format would be Kind/Name.
	// +optional
	Object string `json:"object,omitempty"`

	// Set the target container name if the ForObject has more than one containers.
	// +optional
	Container string `json:"container,omitempty"`

	// Set if expected to inherit envs from the original workload
	// +optional
	WithEnvs bool `json:"withEnvs,omitempty"`
}

// CliAppDistro describes Linux Distro the app depends.
// +kubebuilder:validation:Enum=alpine;ubuntu
type CliAppDistro string

const (
	CliAppDistroAlpine CliAppDistro = "alpine"
	CliAppDistroUbuntu CliAppDistro = "ubuntu"
)

// CliAppShell describes the shell interpreter the app prefers to use.
// +kubebuilder:validation:Enum=bash;zsh
type CliAppShell string

const (
	CliAppShellBash CliAppShell = "bash"
	CliAppShellZsh  CliAppShell = "zsh"
)

// CliAppStatus defines the observed state of CliApp
type CliAppStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Show the app state.
	// Valid values are:
	// - "Rest" (default): The app is installed but not started;
	// - "Recovering": The app is starting;
	// - "Building": The app is waiting for image building;
	// - "Live": The app is running;
	// - "WaitingForSessions": The app is waiting for new sessions and will be shutdown later;
	// - "ShuttingDown": The app is shutting down.
	// +optional
	Phase CliAppPhase `json:"phase,omitempty"`

	// Timestamp of the last phase transition
	// +optional
	LastPhaseTransition metav1.Time `json:"lastPhaseTransition,omitempty"`

	// Specify the Pod name if app is in phase Live.
	// +optional
	PodName string `json:"podName,omitempty"`

	// Specify Errors on reconcile.
	// +optional
	Error string `json:"error,omitempty"`
}

// CliAppPhase describes the app status.
// +kubebuilder:validation:Enum=Rest;Recovering;Building;Live;WaitingForSessions;ShuttingDown
type CliAppPhase string

const (
	CliAppPhaseRest               CliAppPhase = "Rest"
	CliAppPhaseRecovering         CliAppPhase = "Recovering"
	CliAppPhaseBuilding           CliAppPhase = "Building"
	CliAppPhaseLive               CliAppPhase = "Live"
	CliAppPhaseWaitingForSessions CliAppPhase = "WaitingForSessions"
	CliAppPhaseShuttingDown       CliAppPhase = "ShuttingDown"
)

//+genclient
//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="TargetPhase",type=string,JSONPath=`.spec.targetPhase`
//+kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
//+kubebuilder:printcolumn:name="Pod",type=string,JSONPath=`.status.podName`
//+kubebuilder:printcolumn:name="Error",type=string,JSONPath=`.status.error`
//+kubebuilder:printcolumn:name="Distro",type=string,JSONPath=`.spec.distro`
//+kubebuilder:printcolumn:name="Shell",type=string,JSONPath=`.spec.shell`

// CliApp is the Schema for the cliapps API
type CliApp struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CliAppSpec   `json:"spec,omitempty"`
	Status CliAppStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// CliAppList contains a list of CliApp
type CliAppList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CliApp `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CliApp{}, &CliAppList{})
}
