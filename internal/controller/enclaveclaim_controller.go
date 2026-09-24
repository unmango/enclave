/*
Copyright 2026 UnMango.

Licensed under the MIT License. See LICENSE in the project root for details.
*/

package controller

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	enclavev1alpha1 "github.com/unmango/enclave/api/v1alpha1"
)

const (
	claimPoolIndex   = "spec.poolRef.name"
	claimSecretIndex = "spec.secretRefs.name"
)

// EnclaveClaimReconciler binds EnclaveClaims to Enclaves and projects the
// claim's Secrets into them.
type EnclaveClaimReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// APIReader reads past the cache when a cold Enclave already exists but
	// the cache has not seen it yet.
	APIReader client.Reader
}

// +kubebuilder:rbac:groups=enclave.unmango.dev,resources=enclaveclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=enclave.unmango.dev,resources=enclaveclaims/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=enclave.unmango.dev,resources=enclaveclaims/finalizers,verbs=update
// +kubebuilder:rbac:groups=enclave.unmango.dev,resources=enclavepools,verbs=get;list;watch
// +kubebuilder:rbac:groups=enclave.unmango.dev,resources=enclaves,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;update;patch

// Reconcile binds an unbound claim to an Enclave from its pool, then keeps
// the claim's Secrets projected into the bound Enclave.
func (r *EnclaveClaimReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	claim := &enclavev1alpha1.EnclaveClaim{}
	if err := r.Get(ctx, req.NamespacedName, claim); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !claim.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}
	original := claim.DeepCopy()
	claim.Status.ObservedGeneration = claim.Generation

	enclave, err := r.boundEnclave(ctx, claim)
	if err != nil {
		return ctrl.Result{}, err
	}

	switch {
	case enclave == nil && claim.Status.EnclaveName != "":
		claim.Status.Phase = enclavev1alpha1.ClaimLost
		meta.SetStatusCondition(&claim.Status.Conditions, metav1.Condition{
			Type: enclavev1alpha1.ConditionBound, Status: metav1.ConditionFalse, Reason: "EnclaveLost",
			Message:            fmt.Sprintf("Enclave %s no longer exists", claim.Status.EnclaveName),
			ObservedGeneration: claim.Generation,
		})
	case enclave == nil:
		enclave, err = r.bind(ctx, claim)
		if err != nil {
			return ctrl.Result{}, err
		}
		if enclave == nil {
			claim.Status.Phase = enclavev1alpha1.ClaimPending
			meta.SetStatusCondition(&claim.Status.Conditions, metav1.Condition{
				Type: enclavev1alpha1.ConditionBound, Status: metav1.ConditionFalse, Reason: "PoolNotFound",
				Message:            fmt.Sprintf("EnclavePool %s does not exist", claim.Spec.PoolRef.Name),
				ObservedGeneration: claim.Generation,
			})
		}
	}

	if enclave != nil {
		claim.Status.Phase = enclavev1alpha1.ClaimBound
		claim.Status.EnclaveName = enclave.Name
		meta.SetStatusCondition(&claim.Status.Conditions, metav1.Condition{
			Type: enclavev1alpha1.ConditionBound, Status: metav1.ConditionTrue, Reason: "Bound",
			Message:            fmt.Sprintf("Bound to Enclave %s", enclave.Name),
			ObservedGeneration: claim.Generation,
		})
		if err := r.projectSecrets(ctx, claim, enclave); err != nil {
			return ctrl.Result{}, err
		}
	}

	if err := r.Status().Patch(ctx, claim, client.MergeFrom(original)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// boundEnclave returns the Enclave bound to the claim, if any. It finds the
// Enclave by label as well as by status, so a bind whose status update was
// lost is still found.
func (r *EnclaveClaimReconciler) boundEnclave(
	ctx context.Context, claim *enclavev1alpha1.EnclaveClaim,
) (*enclavev1alpha1.Enclave, error) {
	list := &enclavev1alpha1.EnclaveList{}
	if err := r.List(ctx, list, client.InNamespace(claim.Namespace),
		client.MatchingLabels{enclavev1alpha1.ClaimLabel: claim.Name}); err != nil {
		return nil, err
	}
	for i := range list.Items {
		enclave := &list.Items[i]
		if boundTo(enclave, claim) {
			return enclave, nil
		}
	}
	return nil, nil
}

func boundTo(enclave *enclavev1alpha1.Enclave, claim *enclavev1alpha1.EnclaveClaim) bool {
	return enclave.DeletionTimestamp.IsZero() &&
		enclave.Spec.ClaimRef != nil && enclave.Spec.ClaimRef.Name == claim.Name &&
		metav1.IsControlledBy(enclave, claim)
}

// bind claims the warmest unbound Enclave in the pool, or creates one from
// the pool's template when the pool is empty. It returns nil when the pool
// does not exist.
func (r *EnclaveClaimReconciler) bind(
	ctx context.Context, claim *enclavev1alpha1.EnclaveClaim,
) (*enclavev1alpha1.Enclave, error) {
	log := logf.FromContext(ctx)

	pool := &enclavev1alpha1.EnclavePool{}
	err := r.Get(ctx, client.ObjectKey{Namespace: claim.Namespace, Name: claim.Spec.PoolRef.Name}, pool)
	if apierrors.IsNotFound(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}

	list := &enclavev1alpha1.EnclaveList{}
	if err := r.List(ctx, list, client.InNamespace(claim.Namespace),
		client.MatchingLabels{enclavev1alpha1.PoolLabel: pool.Name}); err != nil {
		return nil, err
	}
	var candidates []*enclavev1alpha1.Enclave
	for i := range list.Items {
		enclave := &list.Items[i]
		if enclave.Spec.ClaimRef == nil && enclave.DeletionTimestamp.IsZero() && metav1.IsControlledBy(enclave, pool) {
			candidates = append(candidates, enclave)
		}
	}
	// Ready Enclaves first, then the oldest, which is the closest to ready.
	slices.SortStableFunc(candidates, func(a, b *enclavev1alpha1.Enclave) int {
		if ar, br := isReady(a), isReady(b); ar != br {
			if ar {
				return -1
			}
			return 1
		}
		return a.CreationTimestamp.Compare(b.CreationTimestamp.Time)
	})

	var conflict error
	for _, enclave := range candidates {
		enclave.Spec.ClaimRef = &corev1.LocalObjectReference{Name: claim.Name}
		if enclave.Labels == nil {
			enclave.Labels = map[string]string{}
		}
		enclave.Labels[enclavev1alpha1.ClaimLabel] = claim.Name
		enclave.OwnerReferences = slices.DeleteFunc(enclave.OwnerReferences, func(ref metav1.OwnerReference) bool {
			return ref.Controller != nil && *ref.Controller
		})
		if err := controllerutil.SetControllerReference(claim, enclave, r.Scheme); err != nil {
			return nil, err
		}

		// The update carries the listed resourceVersion, so it fails if another
		// claim bound the Enclave or the pool deleted it first.
		err := r.Update(ctx, enclave)
		if apierrors.IsConflict(err) || apierrors.IsNotFound(err) {
			conflict = err
			continue
		}
		if err != nil {
			return nil, err
		}
		log.Info("Bound Enclave", "enclave", enclave.Name, "ready", isReady(enclave))
		return enclave, nil
	}

	// Every candidate changed since the cache listed it. The cache may be
	// behind a warm Enclave that is still free, so retry before going cold.
	if conflict != nil {
		return nil, conflict
	}
	return r.createBound(ctx, claim, pool)
}

// createBound creates an Enclave from the pool's template, already bound to
// the claim. Its name derives from the claim's UID, so a retry after a lost
// status update finds the same Enclave instead of creating another.
func (r *EnclaveClaimReconciler) createBound(
	ctx context.Context, claim *enclavev1alpha1.EnclaveClaim, pool *enclavev1alpha1.EnclavePool,
) (*enclavev1alpha1.Enclave, error) {
	hash, err := templateHash(&pool.Spec.Template)
	if err != nil {
		return nil, err
	}
	enclave := newPoolEnclave(pool, hash)
	enclave.GenerateName = ""
	enclave.Name = fmt.Sprintf("%s-%s", claim.Name, strings.ReplaceAll(string(claim.UID), "-", "")[:8])
	enclave.Labels[enclavev1alpha1.ClaimLabel] = claim.Name
	enclave.Spec.ClaimRef = &corev1.LocalObjectReference{Name: claim.Name}
	if err := controllerutil.SetControllerReference(claim, enclave, r.Scheme); err != nil {
		return nil, err
	}

	err = r.Create(ctx, enclave)
	if apierrors.IsAlreadyExists(err) {
		existing := &enclavev1alpha1.Enclave{}
		if err := r.APIReader.Get(ctx, client.ObjectKeyFromObject(enclave), existing); err != nil {
			return nil, err
		}
		if !boundTo(existing, claim) {
			return nil, fmt.Errorf("enclave %s exists and is not bound to this claim", enclave.Name)
		}
		return existing, nil
	}
	if err != nil {
		return nil, err
	}
	logf.FromContext(ctx).Info("Created Enclave for claim", "enclave", enclave.Name)
	return enclave, nil
}

// projectSecrets writes the claim's name and the keys of its Secrets into the
// Secret mounted in the Enclave's containers.
func (r *EnclaveClaimReconciler) projectSecrets(
	ctx context.Context, claim *enclavev1alpha1.EnclaveClaim, enclave *enclavev1alpha1.Enclave,
) error {
	target := &corev1.Secret{}
	err := r.Get(ctx, client.ObjectKey{Namespace: enclave.Namespace, Name: claimSecretName(enclave)}, target)
	if apierrors.IsNotFound(err) {
		// The Enclave controller has not created it yet; its creation
		// updates the Enclave, which requeues the claim.
		meta.SetStatusCondition(&claim.Status.Conditions, metav1.Condition{
			Type: enclavev1alpha1.ConditionSecretsProjected, Status: metav1.ConditionFalse,
			Reason: "Waiting", Message: "Waiting for the Enclave's claim Secret",
			ObservedGeneration: claim.Generation,
		})
		return nil
	} else if err != nil {
		return err
	}

	data := map[string][]byte{enclavev1alpha1.ClaimNameKey: []byte(claim.Name)}
	source := map[string]string{enclavev1alpha1.ClaimNameKey: "the claim name"}
	var problems []string
	for _, ref := range claim.Spec.SecretRefs {
		secret := &corev1.Secret{}
		err := r.Get(ctx, client.ObjectKey{Namespace: claim.Namespace, Name: ref.Name}, secret)
		if apierrors.IsNotFound(err) {
			problems = append(problems, fmt.Sprintf("Secret %s not found", ref.Name))
			continue
		} else if err != nil {
			return err
		}
		for _, key := range slices.Sorted(maps.Keys(secret.Data)) {
			if from, ok := source[key]; ok {
				problems = append(problems, fmt.Sprintf("key %s in Secret %s is already set by %s", key, ref.Name, from))
				continue
			}
			data[key] = secret.Data[key]
			source[key] = "Secret " + ref.Name
		}
	}

	if !maps.EqualFunc(target.Data, data, bytes.Equal) {
		target.Data = data
		if err := r.Update(ctx, target); err != nil {
			return err
		}
		logf.FromContext(ctx).Info("Projected claim Secrets", "secret", target.Name, "keys", len(data))
	}

	if len(problems) > 0 {
		meta.SetStatusCondition(&claim.Status.Conditions, metav1.Condition{
			Type: enclavev1alpha1.ConditionSecretsProjected, Status: metav1.ConditionFalse,
			Reason: "SecretsIncomplete", Message: strings.Join(problems, "; "),
			ObservedGeneration: claim.Generation,
		})
	} else {
		meta.SetStatusCondition(&claim.Status.Conditions, metav1.Condition{
			Type: enclavev1alpha1.ConditionSecretsProjected, Status: metav1.ConditionTrue,
			Reason: "Projected", ObservedGeneration: claim.Generation,
		})
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *EnclaveClaimReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.APIReader == nil {
		r.APIReader = mgr.GetAPIReader()
	}

	ctx := context.Background()
	indexer := mgr.GetFieldIndexer()
	if err := indexer.IndexField(ctx, &enclavev1alpha1.EnclaveClaim{}, claimPoolIndex,
		func(obj client.Object) []string {
			return []string{obj.(*enclavev1alpha1.EnclaveClaim).Spec.PoolRef.Name}
		}); err != nil {
		return err
	}
	if err := indexer.IndexField(ctx, &enclavev1alpha1.EnclaveClaim{}, claimSecretIndex,
		func(obj client.Object) []string {
			refs := obj.(*enclavev1alpha1.EnclaveClaim).Spec.SecretRefs
			names := make([]string, 0, len(refs))
			for _, ref := range refs {
				names = append(names, ref.Name)
			}
			return names
		}); err != nil {
		return err
	}

	claimsBy := func(index string) handler.MapFunc {
		return func(ctx context.Context, obj client.Object) []reconcile.Request {
			list := &enclavev1alpha1.EnclaveClaimList{}
			if err := r.List(ctx, list, client.InNamespace(obj.GetNamespace()),
				client.MatchingFields{index: obj.GetName()}); err != nil {
				logf.FromContext(ctx).Error(err, "Failed to list EnclaveClaims", "index", index)
				return nil
			}
			reqs := make([]reconcile.Request, 0, len(list.Items))
			for _, claim := range list.Items {
				reqs = append(reqs, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&claim)})
			}
			return reqs
		}
	}

	// Enclaves map to claims by label, which covers the claim Secret too: the
	// Enclave controller owns it, and its creation updates the Enclave.
	return ctrl.NewControllerManagedBy(mgr).
		For(&enclavev1alpha1.EnclaveClaim{}).
		Watches(&enclavev1alpha1.Enclave{}, handler.EnqueueRequestsFromMapFunc(
			func(_ context.Context, obj client.Object) []reconcile.Request {
				claim, ok := obj.GetLabels()[enclavev1alpha1.ClaimLabel]
				if !ok {
					return nil
				}
				return []reconcile.Request{{Namespace: obj.GetNamespace(), Name: claim}}
			})).
		Watches(&enclavev1alpha1.EnclavePool{}, handler.EnqueueRequestsFromMapFunc(claimsBy(claimPoolIndex))).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(claimsBy(claimSecretIndex))).
		Named("enclaveclaim").
		Complete(r)
}
