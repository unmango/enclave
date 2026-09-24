/*
Copyright 2026 UnMango.

Licensed under the MIT License. See LICENSE in the project root for details.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// EnclaveTemplateSpec describes the Enclaves an EnclavePool creates.
type EnclaveTemplateSpec struct {
	// metadata holds labels and annotations copied onto each Enclave.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec is the environment each Enclave runs.
	// +required
	Spec EnvironmentSpec `json:"spec"`
}

// EnclavePoolSpec defines the desired state of EnclavePool.
type EnclavePoolSpec struct {
	// replicas is the number of unbound, warm Enclaves to keep.
	// Bound Enclaves do not count towards it.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=1
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// template describes the Enclaves the pool creates.
	// Warm Enclaves created from an older template are replaced; bound ones are left alone.
	// +required
	Template EnclaveTemplateSpec `json:"template"`
}

// EnclavePoolStatus defines the observed state of EnclavePool.
type EnclavePoolStatus struct {
	// replicas is the number of unbound Enclaves.
	// +optional
	Replicas int32 `json:"replicas"`

	// availableReplicas is the number of unbound Enclaves that are ready to claim.
	// +optional
	AvailableReplicas int32 `json:"availableReplicas"`

	// boundReplicas is the number of Enclaves from this pool bound to a claim.
	// +optional
	BoundReplicas int32 `json:"boundReplicas"`

	// selector is the label selector for the pool's unbound Enclaves, for the scale subresource.
	// +optional
	Selector string `json:"selector,omitempty"`

	// templateHash is the hash of the current template.
	// +optional
	TemplateHash string `json:"templateHash,omitempty"`

	// observedGeneration is the generation last reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// conditions represent the current state of the EnclavePool.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.replicas,selectorpath=.status.selector
// +kubebuilder:resource:shortName=encpool,categories=enclave
// +kubebuilder:printcolumn:name="Desired",type=integer,JSONPath=".spec.replicas"
// +kubebuilder:printcolumn:name="Available",type=integer,JSONPath=".status.availableReplicas"
// +kubebuilder:printcolumn:name="Bound",type=integer,JSONPath=".status.boundReplicas"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// EnclavePool keeps warm Enclaves ready so that claiming one costs a scheduling decision rather than a cold start.
type EnclavePool struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of EnclavePool
	// +required
	Spec EnclavePoolSpec `json:"spec"`

	// status defines the observed state of EnclavePool
	// +optional
	Status EnclavePoolStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// EnclavePoolList contains a list of EnclavePool
type EnclavePoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []EnclavePool `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &EnclavePool{}, &EnclavePoolList{})
		return nil
	})
}
