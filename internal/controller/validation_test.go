/*
Copyright 2026 UnMango.

Licensed under the MIT License. See LICENSE in the project root for details.
*/

package controller

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"

	enclavev1alpha1 "github.com/unmango/enclave/api/v1alpha1"
)

var _ = Describe("API validation", func() {
	const (
		first, second = "first", "second"
		repoURL       = "https://example.com/repo.git"
	)

	DescribeTable("repository paths",
		func(path, rejection string) {
			enclave := testEnclave(uniqueName("path"))
			enclave.Spec.Repositories = []enclavev1alpha1.Repository{{
				Name: "repo",
				URL:  repoURL,
				Path: path,
			}}

			err := k8sClient.Create(ctx, enclave)
			if rejection == "" {
				Expect(err).NotTo(HaveOccurred())
			} else {
				Expect(err).To(MatchError(ContainSubstring(rejection)))
			}
		},
		Entry("nested", "src/repo", ""),
		Entry("dots in a name", "a..b", ""),
		Entry("absolute", "/etc", "path must be relative"),
		Entry("parent", "..", "path must be relative"),
		Entry("parent in the middle", "a/../../b", "path must be relative"),
		Entry("current directory", ".", "path must not have empty or '.' segments"),
		Entry("current directory in the middle", "a/./b", "path must not have empty or '.' segments"),
		Entry("trailing slash", "a/", "path must not have empty or '.' segments"),
		Entry("doubled slash", "a//b", "path must not have empty or '.' segments"),
	)

	DescribeTable("repository destinations",
		func(repos []enclavev1alpha1.Repository, valid bool) {
			enclave := testEnclave(uniqueName("dest"))
			enclave.Spec.Repositories = repos

			err := k8sClient.Create(ctx, enclave)
			if valid {
				Expect(err).NotTo(HaveOccurred())
			} else {
				Expect(err).To(MatchError(ContainSubstring("repository paths must be unique")))
			}
		},
		Entry("distinct names", []enclavev1alpha1.Repository{
			{Name: "a", URL: repoURL},
			{Name: "b", URL: repoURL},
		}, true),
		Entry("path matching another name", []enclavev1alpha1.Repository{
			{Name: "a", URL: repoURL},
			{Name: "b", URL: repoURL, Path: "a"},
		}, false),
		Entry("same path", []enclavev1alpha1.Repository{
			{Name: "a", URL: repoURL, Path: "src"},
			{Name: "b", URL: repoURL, Path: "src"},
		}, false),
	)

	It("reserves the enclave- prefix for volume names", func() {
		enclave := testEnclave(uniqueName("volume"))
		enclave.Spec.Template.Spec.Volumes = []corev1.Volume{{
			Name: "enclave-workspace", EmptyDir: &corev1.EmptyDirVolumeSource{},
		}}
		Expect(k8sClient.Create(ctx, enclave)).To(MatchError(ContainSubstring("volume names starting with enclave- are reserved")))

		pool := testPool(uniqueName("volume"), 1)
		pool.Spec.Template.Spec.Template.Spec.Volumes = enclave.Spec.Template.Spec.Volumes
		Expect(k8sClient.Create(ctx, pool)).To(MatchError(ContainSubstring("volume names starting with enclave- are reserved")))
	})

	It("limits Enclave names to the length of a label value", func() {
		// The name is copied into the enclave label on the Pod and its objects.
		Expect(k8sClient.Create(ctx, testEnclave(strings.Repeat("a", 63)))).To(Succeed())
		Expect(k8sClient.Create(ctx, testEnclave(strings.Repeat("a", 64)))).
			To(MatchError(ContainSubstring("name must be no more than 63 characters")))
	})

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

	It("limits claim names so the Enclaves created for them fit in a label value", func() {
		// An Enclave created for a claim adds nine characters to its name.
		Expect(k8sClient.Create(ctx, testClaim(strings.Repeat("a", 54), first))).To(Succeed())
		Expect(k8sClient.Create(ctx, testClaim(strings.Repeat("a", 55), first))).
			To(MatchError(ContainSubstring("name must be no more than 54 characters")))
	})

	It("rejects a claim without a pool name", func() {
		claim := testClaim(uniqueName("claim"), "")
		Expect(k8sClient.Create(ctx, claim)).To(MatchError(ContainSubstring("poolRef.name is required")))
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
