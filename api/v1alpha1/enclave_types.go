/*
Copyright 2026 UnMango.

Licensed under the MIT License. See LICENSE in the project root for details.
*/

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	// PoolLabel names the EnclavePool an Enclave was created by.
	PoolLabel = "enclave.unmango.dev/pool"
	// ClaimLabel names the EnclaveClaim an Enclave is bound to.
	ClaimLabel = "enclave.unmango.dev/claim"
	// EnclaveLabel names the Enclave a Pod or supporting object belongs to.
	EnclaveLabel = "enclave.unmango.dev/enclave"
	// TemplateHashLabel records the hash of the pool template an Enclave was created from.
	TemplateHashLabel = "enclave.unmango.dev/template-hash"

	// ClaimMountPath is where every container sees the Secret the claim projects into.
	ClaimMountPath = "/var/run/enclave/claim"
	// ClaimNameKey is the key in the claim Secret that holds the bound claim's name.
	ClaimNameKey = "ENCLAVE_CLAIM"

	// DefaultWorkspaceMountPath is where the workspace is mounted when no mountPath is given.
	DefaultWorkspaceMountPath = "/workspace"
)

// EnvironmentSpec describes a development environment: the pod and the state around it.
// It is shared by Enclave and the template of an EnclavePool.
type EnvironmentSpec struct {
	// template describes the Pod that runs the environment.
	// The operator adds the workspace and claim volumes, and a clone init container when repositories are listed.
	// +required
	Template corev1.PodTemplateSpec `json:"template"`

	// workspace is the directory shared by every container, into which repositories are cloned.
	// Without it, the workspace is an emptyDir mounted at /workspace.
	// +optional
	Workspace *WorkspaceSpec `json:"workspace,omitempty"`

	// repositories are git repositories cloned into the workspace before the environment starts.
	// A repository that is already present in the workspace is left alone.
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=32
	// +kubebuilder:validation:XValidation:rule="self.all(r, self.exists_one(o, (has(o.path) ? o.path : o.name) == (has(r.path) ? r.path : r.name)))",message="repository paths must be unique"
	// +optional
	Repositories []Repository `json:"repositories,omitempty"`

	// serviceAccount requests a ServiceAccount of the Enclave's own, bound to the listed roles.
	// Without it, the Pod runs as the template's serviceAccountName.
	// +optional
	ServiceAccount *ServiceAccountSpec `json:"serviceAccount,omitempty"`
}

// WorkspaceSpec configures the workspace volume.
type WorkspaceSpec struct {
	// mountPath is where the workspace is mounted in every container.
	// +kubebuilder:default="/workspace"
	// +optional
	MountPath string `json:"mountPath,omitempty"`

	// storage backs the workspace with a PersistentVolumeClaim.
	// Without it, the workspace is an emptyDir and does not survive the Pod.
	// +optional
	Storage *WorkspaceStorage `json:"storage,omitempty"`
}

// WorkspaceStorage configures the PersistentVolumeClaim behind a workspace.
type WorkspaceStorage struct {
	// size is the requested capacity.
	// +required
	Size resource.Quantity `json:"size"`

	// storageClassName selects the StorageClass. The cluster default is used when unset.
	// +optional
	StorageClassName *string `json:"storageClassName,omitempty"`

	// accessModes of the claim. Defaults to ReadWriteOnce.
	// +optional
	AccessModes []corev1.PersistentVolumeAccessMode `json:"accessModes,omitempty"`
}

// Repository is a git repository cloned into the workspace.
type Repository struct {
	// name identifies the repository and is the default path.
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	// +kubebuilder:validation:MaxLength=63
	// +required
	Name string `json:"name"`

	// url is the clone URL.
	// +kubebuilder:validation:MinLength=1
	// +required
	URL string `json:"url"`

	// ref is a branch or tag to check out. The remote's default branch is used when unset.
	// +optional
	Ref string `json:"ref,omitempty"`

	// path is the clone destination relative to the workspace. Defaults to name.
	// +kubebuilder:validation:MaxLength=255
	// +kubebuilder:validation:XValidation:rule="!self.startsWith('/') && !self.matches('(^|/)[.][.](/|$)')",message="path must be relative and stay inside the workspace"
	// +kubebuilder:validation:XValidation:rule="!self.matches('(^|/)[.](/|$)|/$|//')",message="path must not have empty or '.' segments"
	// +optional
	Path string `json:"path,omitempty"`

	// credentialsSecretRef names a Secret of type kubernetes.io/basic-auth or kubernetes.io/ssh-auth
	// used to clone the repository.
	// +optional
	CredentialsSecretRef *corev1.LocalObjectReference `json:"credentialsSecretRef,omitempty"`
}

// ServiceAccountSpec configures the ServiceAccount created for an Enclave.
type ServiceAccountSpec struct {
	// roleRefs are bound to the ServiceAccount with one RoleBinding each, in the Enclave's namespace.
	// +optional
	RoleRefs []rbacv1.RoleRef `json:"roleRefs,omitempty"`
}

// EnclaveSpec defines the desired state of Enclave.
// Changes to the environment fields apply only to Pods created afterwards; a running Pod is left alone.
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.poolRef) || (has(self.poolRef) && self.poolRef == oldSelf.poolRef)",message="poolRef is immutable once set"
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.claimRef) || (has(self.claimRef) && self.claimRef == oldSelf.claimRef)",message="claimRef is immutable once set"
type EnclaveSpec struct {
	EnvironmentSpec `json:",inline"`

	// poolRef names the EnclavePool that created this Enclave.
	// +optional
	PoolRef *corev1.LocalObjectReference `json:"poolRef,omitempty"`

	// claimRef names the EnclaveClaim this Enclave is bound to.
	// A bound Enclave is never handed to another claim.
	// +optional
	ClaimRef *corev1.LocalObjectReference `json:"claimRef,omitempty"`
}

// EnclavePhase summarizes where an Enclave is in its lifecycle.
// +kubebuilder:validation:Enum=Pending;Ready;Bound;Failed
type EnclavePhase string

const (
	// EnclavePending means the Pod or its state is not ready yet.
	EnclavePending EnclavePhase = "Pending"
	// EnclaveReady means the Pod is ready and the Enclave is not bound to a claim.
	EnclaveReady EnclavePhase = "Ready"
	// EnclaveBound means the Pod is ready and the Enclave is bound to a claim.
	EnclaveBound EnclavePhase = "Bound"
	// EnclaveFailed means the Pod terminated and will not be restarted.
	EnclaveFailed EnclavePhase = "Failed"
)

// Condition types reported on an Enclave.
const (
	// ConditionReady is True when the Pod is ready.
	ConditionReady = "Ready"
	// ConditionWorkspaceReady is True when the workspace volume exists and is bound.
	ConditionWorkspaceReady = "WorkspaceReady"
	// ConditionBound is True when the Enclave or claim is bound.
	ConditionBound = "Bound"
)

// EnclaveStatus defines the observed state of Enclave.
type EnclaveStatus struct {
	// phase summarizes the conditions.
	// +optional
	Phase EnclavePhase `json:"phase,omitempty"`

	// podName is the Pod running the environment.
	// +optional
	PodName string `json:"podName,omitempty"`

	// observedGeneration is the generation last reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// conditions represent the current state of the Enclave.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=enc,categories=enclave
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Pool",type=string,JSONPath=".spec.poolRef.name"
// +kubebuilder:printcolumn:name="Claim",type=string,JSONPath=".spec.claimRef.name"
// +kubebuilder:printcolumn:name="Pod",type=string,JSONPath=".status.podName"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// Enclave is a development environment: a Pod plus the state around it.
type Enclave struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of Enclave
	// +required
	Spec EnclaveSpec `json:"spec"`

	// status defines the observed state of Enclave
	// +optional
	Status EnclaveStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// EnclaveList contains a list of Enclave
type EnclaveList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []Enclave `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &Enclave{}, &EnclaveList{})
		return nil
	})
}
