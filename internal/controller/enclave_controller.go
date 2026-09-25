/*
Copyright 2026 UnMango.

Licensed under the MIT License. See LICENSE in the project root for details.
*/

package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	enclavev1alpha1 "github.com/unmango/enclave/api/v1alpha1"
)

// EnclaveReconciler reconciles an Enclave into its Pod, workspace, claim
// Secret and ServiceAccount.
type EnclaveReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// GitImage runs the clone init container.
	GitImage string
}

// +kubebuilder:rbac:groups=enclave.unmango.dev,resources=enclaves,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=enclave.unmango.dev,resources=enclaves/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=enclave.unmango.dev,resources=enclaves/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=pods;persistentvolumeclaims;secrets;serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;clusterroles,verbs=bind

// Reconcile creates the objects an Enclave owns and reports their state.
// Owner references on every object let garbage collection clean up.
func (r *EnclaveReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	enclave := &enclavev1alpha1.Enclave{}
	if err := r.Get(ctx, req.NamespacedName, enclave); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !enclave.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}
	original := enclave.DeepCopy()

	pod, workspaceReady, err := r.ensureObjects(ctx, enclave)
	if conflict, ok := errors.AsType[*conflictError](err); ok {
		setConflict(enclave, conflict)
		if err := r.Status().Patch(ctx, enclave, client.MergeFrom(original)); err != nil {
			return ctrl.Result{}, err
		}
	}
	if err != nil {
		return ctrl.Result{}, err
	}

	r.setStatus(enclave, pod, workspaceReady)
	if err := r.Status().Patch(ctx, enclave, client.MergeFrom(original)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// ensureObjects creates whatever the Enclave owns that is missing.
func (r *EnclaveReconciler) ensureObjects(ctx context.Context, enclave *enclavev1alpha1.Enclave) (*corev1.Pod, bool, error) {
	if err := r.ensureClaimSecret(ctx, enclave); err != nil {
		return nil, false, err
	}
	workspaceReady, err := r.ensureWorkspace(ctx, enclave)
	if err != nil {
		return nil, false, err
	}
	if err := r.ensureServiceAccount(ctx, enclave); err != nil {
		return nil, false, err
	}
	if err := checkMounts(&enclave.Spec.EnvironmentSpec); err != nil {
		return nil, false, err
	}
	pod := buildPod(enclave, r.gitImage())
	if err := r.createIfMissing(ctx, enclave, pod); err != nil {
		return nil, false, err
	}
	return pod, workspaceReady, nil
}

func (r *EnclaveReconciler) gitImage() string {
	if r.GitImage == "" {
		return DefaultGitImage
	}
	return r.GitImage
}

// ensureClaimSecret creates the Secret mounted at the claim path. It starts
// empty; the EnclaveClaim controller fills it when the Enclave is bound.
func (r *EnclaveReconciler) ensureClaimSecret(ctx context.Context, enclave *enclavev1alpha1.Enclave) error {
	secret := &corev1.Secret{
		Name:      claimSecretName(enclave),
		Namespace: enclave.Namespace,
		Labels:    map[string]string{enclavev1alpha1.EnclaveLabel: enclave.Name},
	}
	return r.createIfMissing(ctx, enclave, secret)
}

func (r *EnclaveReconciler) ensureWorkspace(ctx context.Context, enclave *enclavev1alpha1.Enclave) (bool, error) {
	ws := enclave.Spec.Workspace
	if ws == nil || ws.Storage == nil {
		return true, nil
	}

	accessModes := ws.Storage.AccessModes
	if len(accessModes) == 0 {
		accessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
	}
	pvc := &corev1.PersistentVolumeClaim{
		Name:      workspacePVCName(enclave),
		Namespace: enclave.Namespace,
		Labels:    map[string]string{enclavev1alpha1.EnclaveLabel: enclave.Name},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      accessModes,
			StorageClassName: ws.Storage.StorageClassName,
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: ws.Storage.Size},
			},
		},
	}
	if err := r.createIfMissing(ctx, enclave, pvc); err != nil {
		return false, err
	}
	return pvc.Status.Phase == corev1.ClaimBound, nil
}

func (r *EnclaveReconciler) ensureServiceAccount(ctx context.Context, enclave *enclavev1alpha1.Enclave) error {
	spec := enclave.Spec.ServiceAccount
	if spec == nil {
		return r.deleteServiceAccount(ctx, enclave)
	}

	sa := &corev1.ServiceAccount{
		Name:      enclave.Name,
		Namespace: enclave.Namespace,
		Labels:    map[string]string{enclavev1alpha1.EnclaveLabel: enclave.Name},
	}
	if err := r.createIfMissing(ctx, enclave, sa); err != nil {
		return err
	}

	// RoleBinding.roleRef is immutable, so bindings are named by a hash of
	// their roleRef and replaced rather than updated.
	want := map[string]bool{}
	for _, ref := range spec.RoleRefs {
		name := roleBindingName(enclave, ref)
		want[name] = true
		rb := &rbacv1.RoleBinding{
			Name:      name,
			Namespace: enclave.Namespace,
			Labels:    map[string]string{enclavev1alpha1.EnclaveLabel: enclave.Name},
			RoleRef:   ref,
			Subjects: []rbacv1.Subject{{
				Kind:      rbacv1.ServiceAccountKind,
				Name:      sa.Name,
				Namespace: sa.Namespace,
			}},
		}
		if err := r.createIfMissing(ctx, enclave, rb); err != nil {
			return err
		}
	}
	return r.deleteStaleRoleBindings(ctx, enclave, want)
}

// deleteServiceAccount removes the ServiceAccount and RoleBindings of an
// Enclave whose spec no longer asks for them. The Pod is not recreated on spec
// changes, so deleting the ServiceAccount is what revokes its tokens.
func (r *EnclaveReconciler) deleteServiceAccount(ctx context.Context, enclave *enclavev1alpha1.Enclave) error {
	if err := r.deleteStaleRoleBindings(ctx, enclave, nil); err != nil {
		return err
	}
	sa := &corev1.ServiceAccount{}
	err := r.Get(ctx, client.ObjectKey{Namespace: enclave.Namespace, Name: enclave.Name}, sa)
	if err != nil || !metav1.IsControlledBy(sa, enclave) {
		return client.IgnoreNotFound(err)
	}
	return client.IgnoreNotFound(r.Delete(ctx, sa))
}

// deleteStaleRoleBindings deletes the Enclave's RoleBindings not named in want.
func (r *EnclaveReconciler) deleteStaleRoleBindings(ctx context.Context, enclave *enclavev1alpha1.Enclave, want map[string]bool) error {
	existing := &rbacv1.RoleBindingList{}
	if err := r.List(ctx, existing, client.InNamespace(enclave.Namespace),
		client.MatchingLabels{enclavev1alpha1.EnclaveLabel: enclave.Name}); err != nil {
		return err
	}
	for i := range existing.Items {
		rb := &existing.Items[i]
		if want[rb.Name] || !metav1.IsControlledBy(rb, enclave) {
			continue
		}
		if err := r.Delete(ctx, rb); client.IgnoreNotFound(err) != nil {
			return err
		}
	}
	return nil
}

func roleBindingName(enclave *enclavev1alpha1.Enclave, ref rbacv1.RoleRef) string {
	sum := sha256.Sum256(fmt.Appendf(nil, "%s/%s/%s", ref.APIGroup, ref.Kind, ref.Name))
	return enclave.Name + "-" + hex.EncodeToString(sum[:])[:10]
}

// conflictError reports a name or path the Enclave needs that something else
// already uses.
type conflictError struct {
	msg string
}

func (e *conflictError) Error() string {
	return e.msg
}

// createIfMissing creates obj owned by the Enclave, or reads the existing
// object into obj. An existing object the Enclave does not control is a
// conflict; adopting it could mount another workload's Secret or bind roles to
// its ServiceAccount.
func (r *EnclaveReconciler) createIfMissing(ctx context.Context, enclave *enclavev1alpha1.Enclave, obj client.Object) error {
	err := r.Get(ctx, client.ObjectKeyFromObject(obj), obj)
	if err == nil && !metav1.IsControlledBy(obj, enclave) {
		gvk, gvkErr := apiutil.GVKForObject(obj, r.Scheme)
		if gvkErr != nil {
			return gvkErr
		}
		return &conflictError{fmt.Sprintf("%s %s already exists and is not controlled by this Enclave", gvk.Kind, obj.GetName())}
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	if err := controllerutil.SetControllerReference(enclave, obj, r.Scheme); err != nil {
		return err
	}
	if err := r.Create(ctx, obj); err != nil {
		return err
	}
	logf.FromContext(ctx).Info("Created object", "kind", fmt.Sprintf("%T", obj), "name", obj.GetName())
	return nil
}

func (r *EnclaveReconciler) setStatus(enclave *enclavev1alpha1.Enclave, pod *corev1.Pod, workspaceReady bool) {
	status := &enclave.Status
	status.PodName = pod.Name
	status.ObservedGeneration = enclave.Generation
	gen := enclave.Generation

	if workspaceReady {
		meta.SetStatusCondition(&status.Conditions, metav1.Condition{
			Type: enclavev1alpha1.ConditionWorkspaceReady, Status: metav1.ConditionTrue,
			Reason: "Bound", ObservedGeneration: gen,
		})
	} else {
		meta.SetStatusCondition(&status.Conditions, metav1.Condition{
			Type: enclavev1alpha1.ConditionWorkspaceReady, Status: metav1.ConditionFalse,
			Reason: "Pending", Message: "Workspace PersistentVolumeClaim is not bound", ObservedGeneration: gen,
		})
	}

	failed := pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded
	ready := !failed && podReady(pod)
	switch {
	case failed:
		meta.SetStatusCondition(&status.Conditions, metav1.Condition{
			Type: enclavev1alpha1.ConditionReady, Status: metav1.ConditionFalse,
			Reason: "PodTerminated", Message: fmt.Sprintf("Pod is %s", pod.Status.Phase), ObservedGeneration: gen,
		})
	case ready:
		meta.SetStatusCondition(&status.Conditions, metav1.Condition{
			Type: enclavev1alpha1.ConditionReady, Status: metav1.ConditionTrue,
			Reason: "PodReady", ObservedGeneration: gen,
		})
	default:
		meta.SetStatusCondition(&status.Conditions, metav1.Condition{
			Type: enclavev1alpha1.ConditionReady, Status: metav1.ConditionFalse,
			Reason: "PodNotReady", ObservedGeneration: gen,
		})
	}

	if ref := enclave.Spec.ClaimRef; ref != nil {
		meta.SetStatusCondition(&status.Conditions, metav1.Condition{
			Type: enclavev1alpha1.ConditionBound, Status: metav1.ConditionTrue,
			Reason: "Claimed", Message: fmt.Sprintf("Bound to EnclaveClaim %s", ref.Name), ObservedGeneration: gen,
		})
	} else {
		meta.SetStatusCondition(&status.Conditions, metav1.Condition{
			Type: enclavev1alpha1.ConditionBound, Status: metav1.ConditionFalse,
			Reason: "Unclaimed", ObservedGeneration: gen,
		})
	}

	switch {
	case failed:
		status.Phase = enclavev1alpha1.EnclaveFailed
	case !ready:
		status.Phase = enclavev1alpha1.EnclavePending
	case enclave.Spec.ClaimRef != nil:
		status.Phase = enclavev1alpha1.EnclaveBound
	default:
		status.Phase = enclavev1alpha1.EnclaveReady
	}
}

func setConflict(enclave *enclavev1alpha1.Enclave, conflict *conflictError) {
	enclave.Status.Phase = enclavev1alpha1.EnclavePending
	enclave.Status.ObservedGeneration = enclave.Generation
	meta.SetStatusCondition(&enclave.Status.Conditions, metav1.Condition{
		Type: enclavev1alpha1.ConditionReady, Status: metav1.ConditionFalse,
		Reason: "Conflict", Message: conflict.Error(), ObservedGeneration: enclave.Generation,
	})
}

func podReady(pod *corev1.Pod) bool {
	for _, c := range pod.Status.Conditions {
		if c.Type == corev1.PodReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

// SetupWithManager sets up the controller with the Manager.
func (r *EnclaveReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&enclavev1alpha1.Enclave{}).
		Owns(&corev1.Pod{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Owns(&corev1.Secret{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&rbacv1.RoleBinding{}).
		Named("enclave").
		Complete(r)
}
