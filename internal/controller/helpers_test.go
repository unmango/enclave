/*
Copyright 2026 UnMango.

Licensed under the MIT License. See LICENSE in the project root for details.
*/

package controller

import (
	"fmt"
	"sync/atomic"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"

	enclavev1alpha1 "github.com/unmango/enclave/api/v1alpha1"
)

const (
	testNamespace = "default"
	testImage     = "busybox"
)

var nameSeq atomic.Int64

// uniqueName returns a name no other spec in the suite uses, so specs do not
// have to wait for the previous spec's objects to be garbage collected.
func uniqueName(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, nameSeq.Add(1))
}

func testEnvironment() enclavev1alpha1.EnvironmentSpec {
	return enclavev1alpha1.EnvironmentSpec{
		Template: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Name:    "dev",
					Image:   testImage,
					Command: []string{"sleep", "infinity"},
				}},
			},
		},
	}
}

func testEnclave(name string) *enclavev1alpha1.Enclave {
	return &enclavev1alpha1.Enclave{
		Name: name, Namespace: testNamespace,
		Spec: enclavev1alpha1.EnclaveSpec{EnvironmentSpec: testEnvironment()},
	}
}

func testPool(name string, replicas int32) *enclavev1alpha1.EnclavePool {
	return &enclavev1alpha1.EnclavePool{
		Name: name, Namespace: testNamespace,
		Spec: enclavev1alpha1.EnclavePoolSpec{
			Replicas: &replicas,
			Template: enclavev1alpha1.EnclaveTemplateSpec{Spec: testEnvironment()},
		},
	}
}

func testClaim(name, pool string) *enclavev1alpha1.EnclaveClaim {
	return &enclavev1alpha1.EnclaveClaim{
		Name: name, Namespace: testNamespace,
		Spec: enclavev1alpha1.EnclaveClaimSpec{
			PoolRef: corev1.LocalObjectReference{Name: pool},
		},
	}
}

func clusterRoleRef(name string) rbacv1.RoleRef {
	return rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: name}
}
