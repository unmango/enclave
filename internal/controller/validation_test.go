/*
Copyright 2026 UnMango.

Licensed under the MIT License. See LICENSE in the project root for details.
*/

package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"

	enclavev1alpha1 "github.com/unmango/enclave/api/v1alpha1"
)

var _ = Describe("API validation", func() {
	const first, second = "first", "second"

	DescribeTable("repository paths",
		func(path string, valid bool) {
			enclave := testEnclave(uniqueName("path"))
			enclave.Spec.Repositories = []enclavev1alpha1.Repository{{
				Name: "repo",
				URL:  "https://example.com/repo.git",
				Path: path,
			}}

			err := k8sClient.Create(ctx, enclave)
			if valid {
				Expect(err).NotTo(HaveOccurred())
			} else {
				Expect(err).To(MatchError(ContainSubstring("path must be relative")))
			}
		},
		Entry("nested", "src/repo", true),
		Entry("dots in a name", "a..b", true),
		Entry("absolute", "/etc", false),
		Entry("parent", "..", false),
		Entry("parent in the middle", "a/../../b", false),
	)

	It("rejects changing an Enclave's claimRef", func() {
		enclave := testEnclave(uniqueName("claimref"))
		enclave.Spec.ClaimRef = &corev1.LocalObjectReference{Name: first}
		Expect(k8sClient.Create(ctx, enclave)).To(Succeed())

		enclave.Spec.ClaimRef.Name = second
		Expect(k8sClient.Update(ctx, enclave)).To(MatchError(ContainSubstring("claimRef is immutable")))

		enclave.Spec.ClaimRef = nil
		Expect(k8sClient.Update(ctx, enclave)).To(MatchError(ContainSubstring("claimRef is immutable")))
	})

	It("allows setting an Enclave's claimRef once", func() {
		enclave := testEnclave(uniqueName("claimref"))
		Expect(k8sClient.Create(ctx, enclave)).To(Succeed())

		enclave.Spec.ClaimRef = &corev1.LocalObjectReference{Name: first}
		Expect(k8sClient.Update(ctx, enclave)).To(Succeed())
	})

	It("rejects changing an Enclave's poolRef", func() {
		enclave := testEnclave(uniqueName("poolref"))
		enclave.Spec.PoolRef = &corev1.LocalObjectReference{Name: first}
		Expect(k8sClient.Create(ctx, enclave)).To(Succeed())

		enclave.Spec.PoolRef.Name = second
		Expect(k8sClient.Update(ctx, enclave)).To(MatchError(ContainSubstring("poolRef is immutable")))
	})

	It("rejects changing a claim's poolRef", func() {
		claim := testClaim(uniqueName("claim"), first)
		Expect(k8sClient.Create(ctx, claim)).To(Succeed())

		claim.Spec.PoolRef.Name = second
		Expect(k8sClient.Update(ctx, claim)).To(MatchError(ContainSubstring("poolRef is immutable")))
	})

	It("keeps labels on a pool's embedded templates", func() {
		pool := testPool(uniqueName("pool"), 0)
		pool.Spec.Template.Labels = map[string]string{"enclave": "label"}
		pool.Spec.Template.Spec.Template.Labels = map[string]string{"pod": "label"}
		Expect(k8sClient.Create(ctx, pool)).To(Succeed())

		Expect(pool.Spec.Template.Labels).To(HaveKeyWithValue("enclave", "label"))
		Expect(pool.Spec.Template.Spec.Template.Labels).To(HaveKeyWithValue("pod", "label"))
	})

	It("defaults a pool's replicas and workspace mount path", func() {
		pool := testPool(uniqueName("pool"), 0)
		pool.Spec.Replicas = nil
		pool.Spec.Template.Spec.Workspace = &enclavev1alpha1.WorkspaceSpec{}
		Expect(k8sClient.Create(ctx, pool)).To(Succeed())

		Expect(pool.Spec.Replicas).To(HaveValue(BeEquivalentTo(1)))
		Expect(pool.Spec.Template.Spec.Workspace.MountPath).To(Equal(enclavev1alpha1.DefaultWorkspaceMountPath))
	})
})
