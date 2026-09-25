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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	enclavev1alpha1 "github.com/unmango/enclave/api/v1alpha1"
)

func poolEnclaves(g Gomega, pool string) []enclavev1alpha1.Enclave {
	list := &enclavev1alpha1.EnclaveList{}
	g.Expect(k8sClient.List(ctx, list, client.InNamespace(testNamespace),
		client.MatchingLabels{enclavev1alpha1.PoolLabel: pool})).To(Succeed())
	return list.Items
}

func getPool(g Gomega, name string) *enclavev1alpha1.EnclavePool {
	pool := &enclavev1alpha1.EnclavePool{}
	g.Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: name}, pool)).To(Succeed())
	return pool
}

// readyAll marks the Pod of every listed Enclave ready.
func readyAll(enclaves []enclavev1alpha1.Enclave) {
	for _, e := range enclaves {
		Eventually(func(g Gomega) { getPod(g, e.Name) }).Should(Succeed())
		setPodPhase(e.Name, corev1.PodRunning, true)
	}
}

var _ = Describe("EnclavePool Controller", func() {
	var name string

	BeforeEach(func() {
		name = uniqueName("pool")
	})

	It("keeps replicas warm Enclaves created from the template", func() {
		pool := testPool(name, 2)
		pool.Spec.Template.Labels = map[string]string{"team": "dev"}
		Expect(k8sClient.Create(ctx, pool)).To(Succeed())

		var enclaves []enclavev1alpha1.Enclave
		Eventually(func(g Gomega) {
			enclaves = poolEnclaves(g, name)
			g.Expect(enclaves).To(HaveLen(2))
		}).Should(Succeed())
		Consistently(func(g Gomega) {
			g.Expect(poolEnclaves(g, name)).To(HaveLen(2))
		}, "1s").Should(Succeed())

		pool = getPool(Default, name)
		for _, e := range enclaves {
			Expect(e.Labels).To(HaveKeyWithValue("team", "dev"))
			Expect(e.Spec.PoolRef).To(HaveValue(HaveField("Name", name)))
			Expect(e.Spec.ClaimRef).To(BeNil())
			Expect(metav1.IsControlledBy(&e, pool)).To(BeTrue())
		}

		Eventually(func(g Gomega) {
			status := getPool(g, name).Status
			g.Expect(status.Replicas).To(BeEquivalentTo(2))
			g.Expect(status.AvailableReplicas).To(BeEquivalentTo(0))
			g.Expect(status.Selector).To(Equal("!enclave.unmango.dev/claim,enclave.unmango.dev/pool=" + name))
		}).Should(Succeed())

		readyAll(enclaves)
		Eventually(func(g Gomega) {
			pool := getPool(g, name)
			g.Expect(pool.Status.AvailableReplicas).To(BeEquivalentTo(2))
			g.Expect(pool.Status.Conditions).To(ContainElement(And(
				HaveField("Type", enclavev1alpha1.ConditionReady),
				HaveField("Status", metav1.ConditionTrue),
			)))
		}).Should(Succeed())
	})

	It("scales down, deleting Enclaves that are not ready first", func() {
		Expect(k8sClient.Create(ctx, testPool(name, 3))).To(Succeed())
		var enclaves []enclavev1alpha1.Enclave
		Eventually(func(g Gomega) {
			enclaves = poolEnclaves(g, name)
			g.Expect(enclaves).To(HaveLen(3))
		}).Should(Succeed())
		readyAll(enclaves[:1])
		Eventually(func(g Gomega) {
			g.Expect(getEnclave(g, enclaves[0].Name).Status.Phase).To(Equal(enclavev1alpha1.EnclaveReady))
		}).Should(Succeed())

		Eventually(func(g Gomega) {
			pool := getPool(g, name)
			pool.Spec.Replicas = new(int32(1))
			g.Expect(k8sClient.Update(ctx, pool)).To(Succeed())
		}).Should(Succeed())

		Eventually(func(g Gomega) {
			g.Expect(poolEnclaves(g, name)).To(ConsistOf(HaveField("Name", enclaves[0].Name)))
		}).Should(Succeed())
	})

	It("does not count bound Enclaves towards replicas", func() {
		const otherClaim = "someone"
		Expect(k8sClient.Create(ctx, testPool(name, 1))).To(Succeed())
		var first enclavev1alpha1.Enclave
		Eventually(func(g Gomega) {
			enclaves := poolEnclaves(g, name)
			g.Expect(enclaves).To(HaveLen(1))
			first = enclaves[0]
		}).Should(Succeed())

		Eventually(func(g Gomega) {
			enclave := getEnclave(g, first.Name)
			enclave.Labels[enclavev1alpha1.ClaimLabel] = otherClaim
			enclave.Spec.ClaimRef = &corev1.LocalObjectReference{Name: otherClaim}
			g.Expect(k8sClient.Update(ctx, enclave)).To(Succeed())
		}).Should(Succeed())

		Eventually(func(g Gomega) {
			g.Expect(poolEnclaves(g, name)).To(HaveLen(2))
			status := getPool(g, name).Status
			g.Expect(status.Replicas).To(BeEquivalentTo(1))
			g.Expect(status.BoundReplicas).To(BeEquivalentTo(1))
		}).Should(Succeed())
	})

	It("leaves Enclaves it does not control alone", func() {
		Expect(k8sClient.Create(ctx, testPool(name, 1))).To(Succeed())
		var owned []enclavev1alpha1.Enclave
		Eventually(func(g Gomega) {
			owned = poolEnclaves(g, name)
			g.Expect(owned).To(HaveLen(1))
		}).Should(Succeed())
		readyAll(owned)

		foreign := testEnclave(uniqueName("foreign"))
		foreign.Labels = map[string]string{enclavev1alpha1.PoolLabel: name}
		Expect(k8sClient.Create(ctx, foreign)).To(Succeed())

		Consistently(func(g Gomega) {
			getEnclave(g, foreign.Name)
			g.Expect(getPool(g, name).Status.Replicas).To(BeEquivalentTo(1))
		}, "2s").Should(Succeed())
	})

	It("rejects names too long for the Enclaves it creates", func() {
		// Generated Enclave names add six characters, and must fit in a label value.
		Expect(k8sClient.Create(ctx, testPool(strings.Repeat("a", 57), 0))).To(Succeed())
		Expect(k8sClient.Create(ctx, testPool(strings.Repeat("a", 58), 0))).
			To(MatchError(ContainSubstring("name must be no more than 57 characters")))
	})

	It("replaces warm Enclaves after a template change once new ones are ready", func() {
		Expect(k8sClient.Create(ctx, testPool(name, 1))).To(Succeed())
		var old enclavev1alpha1.Enclave
		Eventually(func(g Gomega) {
			enclaves := poolEnclaves(g, name)
			g.Expect(enclaves).To(HaveLen(1))
			old = enclaves[0]
		}).Should(Succeed())

		Eventually(func(g Gomega) {
			pool := getPool(g, name)
			pool.Spec.Template.Spec.Template.Spec.Containers[0].Image = "alpine"
			g.Expect(k8sClient.Update(ctx, pool)).To(Succeed())
		}).Should(Succeed())

		var replacement enclavev1alpha1.Enclave
		Eventually(func(g Gomega) {
			enclaves := poolEnclaves(g, name)
			g.Expect(enclaves).To(HaveLen(2))
			for _, e := range enclaves {
				if e.Name != old.Name {
					replacement = e
				}
			}
		}).Should(Succeed())
		Expect(replacement.Spec.Template.Spec.Containers[0].Image).To(Equal("alpine"))

		By("keeping the old Enclave until its replacement is ready")
		Consistently(func(g Gomega) {
			g.Expect(poolEnclaves(g, name)).To(HaveLen(2))
		}, "1s").Should(Succeed())

		readyAll([]enclavev1alpha1.Enclave{replacement})
		Eventually(func(g Gomega) {
			g.Expect(poolEnclaves(g, name)).To(ConsistOf(HaveField("Name", replacement.Name)))
		}).Should(Succeed())
	})
})
