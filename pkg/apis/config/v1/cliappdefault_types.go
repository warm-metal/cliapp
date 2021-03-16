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
	cfg "sigs.k8s.io/controller-runtime/pkg/config/v1alpha1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

//+kubebuilder:object:root=true

// CliAppDefault is the Schema for the cliappdefaults API
type CliAppDefault struct {
	metav1.TypeMeta `json:",inline"`

	// ControllerManagerConfigurationSpec returns the contfigurations for controllers
	cfg.ControllerManagerConfigurationSpec `json:",inline"`

	// The context image to start an app
	DefaultAppContextImage string `json:"defaultAppContextImage,omitempty"`

	// The shell cliapp used as default. The default value is bash. You can also use zsh instead.
	DefaultShell string `json:"defaultShell,omitempty"`

	// Linux distro on that the app works as default. The default value is alpine. Another supported distro is ubuntu.
	DefaultDistro string `json:"defaultDistro,omitempty"`

	// Duration in that the background pod would be still alive even no active session opened.
	IdleLiveDuration metav1.Duration `json:"idleLiveDuration,omitempty"`

	// buildkitd endpoint used to build image for app
	BuilderService string `json:"builder,omitempty"`
}

func init() {
	SchemeBuilder.Register(&CliAppDefault{})
}
