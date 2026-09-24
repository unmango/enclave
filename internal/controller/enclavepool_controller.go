/*
Copyright 2026 UnMango.

Licensed under the MIT License. See LICENSE in the project root for details.
*/

package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
	"slices"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	enclavev1alpha1 "github.com/unmango/enclave/api/v1alpha1"
)

// EnclavePoolReconciler keeps a pool's count of warm, unbound Enclaves at its
// replicas.
type EnclavePoolReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// APIReader lists a pool's Enclaves past the cache, so Enclaves created by
	// the previous reconcile are counted even before the cache sees them.
	APIReader client.Reader
}

// +kubebuilder:rbac:groups=enclave.unmango.dev,resources=enclavepools,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=enclave.unmango.dev,resources=enclavepools/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=enclave.unmango.dev,resources=enclavepools/finalizers,verbs=update
// +kubebuilder:rbac:groups=enclave.unmango.dev,resources=enclaves,verbs=get;list;watch;create;update;patch;delete

// Reconcile creates and deletes unbound Enclaves to match replicas, and
// replaces unbound Enclaves created from an older template once enough
// current ones are ready.
func (r *EnclavePoolReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	pool := &enclavev1alpha1.EnclavePool{}
	if err := r.Get(ctx, req.NamespacedName, pool); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !pool.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}
	original := pool.DeepCopy()

	hash, err := templateHash(&pool.Spec.Template)
	if err != nil {
		return ctrl.Result{}, err
	}

	list := &enclavev1alpha1.EnclaveList{}
	if err := r.APIReader.List(ctx, list, client.InNamespace(pool.Namespace),
		client.MatchingLabels{enclavev1alpha1.PoolLabel: pool.Name}); err != nil {
		return ctrl.Result{}, err
	}

	var bound, current, stale []*enclavev1alpha1.Enclave
	for i := range list.Items {
		enclave := &list.Items[i]
		switch {
		case !enclave.DeletionTimestamp.IsZero():
		case enclave.Spec.ClaimRef != nil:
			bound = append(bound, enclave)
		case enclave.Labels[enclavev1alpha1.TemplateHashLabel] != hash:
			stale = append(stale, enclave)
		default:
			current = append(current, enclave)
		}
	}

	desired := int(ptrOr(pool.Spec.Replicas, 1))
	for len(current) < desired {
		enclave := newPoolEnclave(pool, hash)
		if err := controllerutil.SetControllerReference(pool, enclave, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.Create(ctx, enclave); err != nil {
			return ctrl.Result{}, err
		}
		log.Info("Created Enclave", "enclave", enclave.Name)
		current = append(current, enclave)
	}

	if len(current) > desired {
		// Not-ready Enclaves go first, then the newest, so the warmest stay.
		slices.SortStableFunc(current, func(a, b *enclavev1alpha1.Enclave) int {
			if ar, br := isReady(a), isReady(b); ar != br {
				if ar {
					return 1
				}
				return -1
			}
			return b.CreationTimestamp.Compare(a.CreationTimestamp.Time)
		})
		for _, enclave := range current[:len(current)-desired] {
			if err := r.deleteUnbound(ctx, enclave); err != nil {
				return ctrl.Result{}, err
			}
		}
		current = current[len(current)-desired:]
	}

	available := countReady(current)
	// Stale Enclaves stay claimable until current ones can take their place.
	if available >= desired {
		for _, enclave := range stale {
			if err := r.deleteUnbound(ctx, enclave); err != nil {
				return ctrl.Result{}, err
			}
		}
		stale = nil
	}

	selector := labels.NewSelector()
	poolReq, err := labels.NewRequirement(enclavev1alpha1.PoolLabel, selection.Equals, []string{pool.Name})
	if err != nil {
		return ctrl.Result{}, err
	}
	unclaimedReq, err := labels.NewRequirement(enclavev1alpha1.ClaimLabel, selection.DoesNotExist, nil)
	if err != nil {
		return ctrl.Result{}, err
	}

	pool.Status = enclavev1alpha1.EnclavePoolStatus{
		Replicas:           int32(len(current) + len(stale)),
		AvailableReplicas:  int32(available + countReady(stale)),
		BoundReplicas:      int32(len(bound)),
		Selector:           selector.Add(*poolReq, *unclaimedReq).String(),
		TemplateHash:       hash,
		ObservedGeneration: pool.Generation,
		Conditions:         pool.Status.Conditions,
	}
	if available >= desired {
		meta.SetStatusCondition(&pool.Status.Conditions, metav1.Condition{
			Type: enclavev1alpha1.ConditionReady, Status: metav1.ConditionTrue,
			Reason: "PoolFilled", ObservedGeneration: pool.Generation,
		})
	} else {
		meta.SetStatusCondition(&pool.Status.Conditions, metav1.Condition{
			Type: enclavev1alpha1.ConditionReady, Status: metav1.ConditionFalse,
			Reason: "Warming", Message: "Waiting for Enclaves to become ready", ObservedGeneration: pool.Generation,
		})
	}
	if err := r.Status().Patch(ctx, pool, client.MergeFrom(original)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// deleteUnbound deletes an Enclave only if it is unchanged since it was
// listed, so an Enclave a claim has just bound is never deleted.
func (r *EnclavePoolReconciler) deleteUnbound(ctx context.Context, enclave *enclavev1alpha1.Enclave) error {
	err := r.Delete(ctx, enclave, client.Preconditions{
		UID:             &enclave.UID,
		ResourceVersion: &enclave.ResourceVersion,
	})
	if apierrors.IsConflict(err) || apierrors.IsNotFound(err) {
		return nil
	}
	if err == nil {
		logf.FromContext(ctx).Info("Deleted Enclave", "enclave", enclave.Name)
	}
	return err
}

// newPoolEnclave renders an unbound Enclave from a pool's template.
func newPoolEnclave(pool *enclavev1alpha1.EnclavePool, hash string) *enclavev1alpha1.Enclave {
	tmpl := pool.Spec.Template.DeepCopy()
	lbls := map[string]string{}
	maps.Copy(lbls, tmpl.Labels)
	lbls[enclavev1alpha1.PoolLabel] = pool.Name
	lbls[enclavev1alpha1.TemplateHashLabel] = hash

	return &enclavev1alpha1.Enclave{
		GenerateName: pool.Name + "-",
		Namespace:    pool.Namespace,
		Labels:       lbls,
		Annotations:  tmpl.Annotations,
		Spec: enclavev1alpha1.EnclaveSpec{
			EnvironmentSpec: tmpl.Spec,
			PoolRef:         &corev1.LocalObjectReference{Name: pool.Name},
		},
	}
}

func templateHash(tmpl *enclavev1alpha1.EnclaveTemplateSpec) (string, error) {
	data, err := json.Marshal(tmpl)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])[:10], nil
}

func isReady(enclave *enclavev1alpha1.Enclave) bool {
	return enclave.Status.Phase == enclavev1alpha1.EnclaveReady
}

func countReady(enclaves []*enclavev1alpha1.Enclave) int {
	n := 0
	for _, e := range enclaves {
		if isReady(e) {
			n++
		}
	}
	return n
}

func ptrOr[T any](p *T, def T) T {
	if p == nil {
		return def
	}
	return *p
}

// SetupWithManager sets up the controller with the Manager.
func (r *EnclavePoolReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.APIReader == nil {
		r.APIReader = mgr.GetAPIReader()
	}
	// Enclaves are mapped by label rather than owner, because a bound Enclave
	// is owned by its claim and still counts towards the pool's status.
	return ctrl.NewControllerManagedBy(mgr).
		For(&enclavev1alpha1.EnclavePool{}).
		Watches(&enclavev1alpha1.Enclave{}, handler.EnqueueRequestsFromMapFunc(
			func(_ context.Context, obj client.Object) []reconcile.Request {
				pool, ok := obj.GetLabels()[enclavev1alpha1.PoolLabel]
				if !ok {
					return nil
				}
				return []reconcile.Request{{Namespace: obj.GetNamespace(), Name: pool}}
			})).
		Named("enclavepool").
		Complete(r)
}
