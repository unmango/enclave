/*
Copyright 2026 UnMango.

Licensed under the MIT License. See LICENSE in the project root for details.
*/

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// EnclaveClaimSpec defines the desired state of EnclaveClaim.
type EnclaveClaimSpec struct {
	// poolRef names the EnclavePool to draw an Enclave from.
	// When the pool has no ready Enclave, one is created from its template.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="poolRef is immutable"
	// +required
	PoolRef corev1.LocalObjectReference `json:"poolRef"`

	// secretRefs name Secrets whose keys are projected into the bound Enclave at /var/run/enclave/claim.
	// Keys must be unique across the listed Secrets.
	// +optional
	SecretRefs []corev1.LocalObjectReference `json:"secretRefs,omitempty"`
}

// EnclaveClaimPhase summarizes where an EnclaveClaim is in its lifecycle.
// +kubebuilder:validation:Enum=Pending;Bound;Lost
type EnclaveClaimPhase string

const (
	// ClaimPending means the claim is not bound yet.
	ClaimPending EnclaveClaimPhase = "Pending"
	// ClaimBound means the claim is bound to an Enclave.
	ClaimBound EnclaveClaimPhase = "Bound"
	// ClaimLost means the bound Enclave no longer exists.
	ClaimLost EnclaveClaimPhase = "Lost"
)

// ConditionSecretsProjected is True when the claim's Secrets are projected into the Enclave.
const ConditionSecretsProjected = "SecretsProjected"

// EnclaveClaimStatus defines the observed state of EnclaveClaim.
type EnclaveClaimStatus struct {
	// phase summarizes the conditions.
	// +optional
	Phase EnclaveClaimPhase `json:"phase,omitempty"`

	// enclaveName is the Enclave the claim is bound to.
	// +optional
	EnclaveName string `json:"enclaveName,omitempty"`

	// observedGeneration is the generation last reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// conditions represent the current state of the EnclaveClaim.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=encclaim,categories=enclave
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Pool",type=string,JSONPath=".spec.poolRef.name"
// +kubebuilder:printcolumn:name="Enclave",type=string,JSONPath=".status.enclaveName"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// EnclaveClaim requests an Enclave from an EnclavePool.
// Deleting the claim deletes the Enclave bound to it.
type EnclaveClaim struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of EnclaveClaim
	// +required
	Spec EnclaveClaimSpec `json:"spec"`

	// status defines the observed state of EnclaveClaim
	// +optional
	Status EnclaveClaimStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// EnclaveClaimList contains a list of EnclaveClaim
type EnclaveClaimList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []EnclaveClaim `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &EnclaveClaim{}, &EnclaveClaimList{})
		return nil
	})
}
