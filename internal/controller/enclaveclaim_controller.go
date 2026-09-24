/*
Copyright 2026 UnMango.

Licensed under the MIT License. See LICENSE in the project root for details.
*/

package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	enclavev1alpha1 "github.com/unmango/enclave/api/v1alpha1"
)

// EnclaveClaimReconciler reconciles a EnclaveClaim object
type EnclaveClaimReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=enclave.unmango.dev,resources=enclaveclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=enclave.unmango.dev,resources=enclaveclaims/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=enclave.unmango.dev,resources=enclaveclaims/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the EnclaveClaim object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.24.1/pkg/reconcile
func (r *EnclaveClaimReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = logf.FromContext(ctx)

	// TODO(user): your logic here

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *EnclaveClaimReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&enclavev1alpha1.EnclaveClaim{}).
		Named("enclaveclaim").
		Complete(r)
}
