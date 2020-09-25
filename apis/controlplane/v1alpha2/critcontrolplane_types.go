/*
Copyright 2020 Critical Stack, LLC

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

package v1alpha2

import (
	"fmt"
	"hash/fnv"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/cluster-api/controllers/mdutil"

	cabpcv1 "github.com/criticalstack/cluster-api-bootstrap-provider-crit/apis/bootstrap/v1alpha2"
)

const (
	CritControlPlaneFinalizer           = "crit.controlplane.cluster.x-k8s.io"
	CritControlPlaneHashLabelKey        = "crit.controlplane.cluster.x-k8s.io/hash"
	SelectedForUpgradeAnnotation        = "crit.controlplane.cluster.x-k8s.io/selected-for-upgrade"
	UpgradeReplacementCreatedAnnotation = "crit.controlplane.cluster.x-k8s.io/upgrade-replacement-created"
)

// CritControlPlaneSpec defines the desired state of CritControlPlane
type CritControlPlaneSpec struct {
	Replicas               int                    `json:"replicas"`
	Version                string                 `json:"version"`
	InfrastructureTemplate corev1.ObjectReference `json:"infrastructureTemplate"`
	CritConfigSpec         cabpcv1.CritConfigSpec `json:"critConfigSpec"`
}

func (c *CritControlPlaneSpec) Hash() string {
	h := fnv.New32a()
	mdutil.DeepHashObject(h, struct {
		version                string
		infrastructureTemplate corev1.ObjectReference
		critConfigSpec         cabpcv1.CritConfigSpec
	}{
		version:                c.Version,
		infrastructureTemplate: c.InfrastructureTemplate,
		critConfigSpec:         c.CritConfigSpec,
	})
	return fmt.Sprintf("%d", h.Sum32())
}

// CritControlPlaneStatus defines the observed state of CritControlPlane
type CritControlPlaneStatus struct {
	// Selector is the label selector in string format to avoid introspection
	// by clients, and is used to provide the CRD-based integration for the
	// scale subresource and additional integrations for things like kubectl
	// describe.. The string will be in the same format as the query-param syntax.
	// More info about label selectors: http://kubernetes.io/docs/user-guide/labels#label-selectors
	// +optional
	Selector string `json:"selector,omitempty"`

	// Total number of non-terminated machines targeted by this control plane
	// (their labels match the selector).
	// +optional
	Replicas int32 `json:"replicas,omitempty"`

	// Total number of non-terminated machines targeted by this control plane
	// that have the desired template spec.
	// +optional
	UpdatedReplicas int32 `json:"updatedReplicas,omitempty"`

	// Total number of fully running and ready control plane machines.
	// +optional
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`

	// Total number of unavailable machines targeted by this control plane.
	// This is the total number of machines that are still required for
	// the deployment to have 100% available capacity. They may either
	// be machines that are running but not yet ready or machines
	// that still have not been created.
	// +optional
	UnavailableReplicas int32 `json:"unavailableReplicas,omitempty"`

	// Initialized denotes whether or not the control plane has been
	// initialized. This will only ever happen once for any given cluster and
	// requires that nodes are available equal to the number of replicas.
	// Available here is meaning that the Kubernetes components are ready, but
	// the node itself does not necessarily need to be in Ready status.
	// +optional
	Initialized bool `json:"initialized"`

	// Ready denotes that the CritControlPlane API Server is ready to
	// receive requests and has satisfied all readiness conditions.
	// +optional
	Ready bool `json:"ready"`

	// FailureReason indicates that there is a terminal problem reconciling the
	// state, and will be set to a token value suitable for
	// programmatic interpretation.
	// +optional
	FailureReason string `json:"failureReason,omitempty"`

	// ErrorMessage indicates that there is a terminal problem reconciling the
	// state, and will be set to a descriptive error message.
	// +optional
	FailureMessage *string `json:"failureMessage,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=critcontrolplanes,shortName=ccp,scope=Namespaced,categories=cluster-api
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.replicas,selectorpath=.status.selector
// +kubebuilder:printcolumn:name="Ready",type=boolean,JSONPath=".status.ready",description="CritControlPlane API Server is ready to receive requests"
// +kubebuilder:printcolumn:name="Initialized",type=boolean,JSONPath=".status.initialized",description="This denotes whether or not the control plane has the uploaded crit-config configmap"
// +kubebuilder:printcolumn:name="Replicas",type=integer,JSONPath=".status.replicas",description="Total number of non-terminated machines targeted by this control plane"
// +kubebuilder:printcolumn:name="Ready Replicas",type=integer,JSONPath=".status.readyReplicas",description="Total number of fully running and ready control plane machines"
// +kubebuilder:printcolumn:name="Updated Replicas",type=integer,JSONPath=".status.updatedReplicas",description="Total number of non-terminated machines targeted by this control plane that have the desired template spec"
// +kubebuilder:printcolumn:name="Unavailable Replicas",type=integer,JSONPath=".status.unavailableReplicas",description="Total number of unavailable machines targeted by this control plane"

// CritControlPlane is the Schema for the critcontrolplanes API
type CritControlPlane struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CritControlPlaneSpec   `json:"spec,omitempty"`
	Status CritControlPlaneStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// CritControlPlaneList contains a list of CritControlPlane
type CritControlPlaneList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CritControlPlane `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CritControlPlane{}, &CritControlPlaneList{})
}
