/*
Copyright 2026 UnMango.

Licensed under the MIT License. See LICENSE in the project root for details.
*/

package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	enclavev1alpha1 "github.com/unmango/enclave/api/v1alpha1"
)

func getClaim(g Gomega, name string) *enclavev1alpha1.EnclaveClaim {
	claim := &enclavev1alpha1.EnclaveClaim{}
	g.Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: name}, claim)).To(Succeed())
	return claim
}

func claimSecretData(g Gomega, enclave string) map[string][]byte {
	secret := &corev1.Secret{}
	g.Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: enclave + "-claim"}, secret)).
		To(Succeed())
	return secret.Data
}

// boundEnclaveName waits for the claim to bind and returns its Enclave.
func boundEnclaveName(claim string) string {
	var name string
	Eventually(func(g Gomega) {
		c := getClaim(g, claim)
		g.Expect(c.Status.Phase).To(Equal(enclavev1alpha1.ClaimBound))
		name = c.Status.EnclaveName
	}).Should(Succeed())
	return name
}

// warmPool creates a pool of one Enclave and waits until it is ready.
func warmPool(name string) enclavev1alpha1.Enclave {
	Expect(k8sClient.Create(ctx, testPool(name, 1))).To(Succeed())
	var enclaves []enclavev1alpha1.Enclave
	Eventually(func(g Gomega) {
		enclaves = poolEnclaves(g, name)
		g.Expect(enclaves).To(HaveLen(1))
	}).Should(Succeed())
	readyAll(enclaves)
	Eventually(func(g Gomega) {
		g.Expect(getPool(g, name).Status.AvailableReplicas).To(BeEquivalentTo(1))
	}).Should(Succeed())
	return enclaves[0]
}

var _ = Describe("EnclaveClaim Controller", func() {
	var pool, name string

	BeforeEach(func() {
		pool = uniqueName("pool")
		name = uniqueName("claim")
	})

	It("binds a warm Enclave and the pool backfills", func() {
		warm := warmPool(pool)

		Expect(k8sClient.Create(ctx, testClaim(name, pool))).To(Succeed())
		Expect(boundEnclaveName(name)).To(Equal(warm.Name))

		claim := getClaim(Default, name)
		enclave := getEnclave(Default, warm.Name)
		Expect(enclave.Spec.ClaimRef).To(HaveValue(HaveField("Name", name)))
		Expect(enclave.Labels).To(HaveKeyWithValue(enclavev1alpha1.ClaimLabel, name))
		Expect(metav1.IsControlledBy(enclave, claim)).To(BeTrue())
		Expect(enclave.OwnerReferences).To(HaveLen(1))

		Eventually(func(g Gomega) {
			g.Expect(getEnclave(g, warm.Name).Status.Phase).To(Equal(enclavev1alpha1.EnclaveBound))
			g.Expect(poolEnclaves(g, pool)).To(HaveLen(2))
			g.Expect(getPool(g, pool).Status.BoundReplicas).To(BeEquivalentTo(1))
		}).Should(Succeed())
	})

	It("creates an Enclave from the pool template when the pool is empty", func() {
		Expect(k8sClient.Create(ctx, testPool(pool, 0))).To(Succeed())
		claim := testClaim(name, pool)
		Expect(k8sClient.Create(ctx, claim)).To(Succeed())

		enclaveName := boundEnclaveName(name)
		enclave := getEnclave(Default, enclaveName)
		Expect(enclave.Spec.ClaimRef).To(HaveValue(HaveField("Name", name)))
		Expect(enclave.Spec.PoolRef).To(HaveValue(HaveField("Name", pool)))
		Expect(metav1.IsControlledBy(enclave, getClaim(Default, name))).To(BeTrue())
		Consistently(func(g Gomega) {
			g.Expect(poolEnclaves(g, pool)).To(HaveLen(1))
		}, "1s").Should(Succeed())
	})

	It("stays pending until its pool exists", func() {
		Expect(k8sClient.Create(ctx, testClaim(name, pool))).To(Succeed())
		Eventually(func(g Gomega) {
			claim := getClaim(g, name)
			g.Expect(claim.Status.Phase).To(Equal(enclavev1alpha1.ClaimPending))
			g.Expect(claim.Status.Conditions).To(ContainElement(HaveField("Reason", "PoolNotFound")))
		}).Should(Succeed())

		Expect(k8sClient.Create(ctx, testPool(pool, 0))).To(Succeed())
		boundEnclaveName(name)
	})

	It("never binds two claims to the same Enclave", func() {
		warmPool(pool)
		names := []string{uniqueName("claim"), uniqueName("claim"), uniqueName("claim")}
		for _, n := range names {
			Expect(k8sClient.Create(ctx, testClaim(n, pool))).To(Succeed())
		}

		bound := map[string]string{}
		for _, n := range names {
			enclave := boundEnclaveName(n)
			Expect(bound).NotTo(HaveKey(enclave), "Enclave %s bound twice", enclave)
			bound[enclave] = n
		}
		for enclave, claim := range bound {
			Expect(getEnclave(Default, enclave).Spec.ClaimRef).To(HaveValue(HaveField("Name", claim)))
		}
	})

	It("projects the claim's Secrets into the Enclave", func() {
		const key = "TOKEN"
		warmPool(pool)
		token := &corev1.Secret{
			Name: uniqueName("token"), Namespace: testNamespace,
			StringData: map[string]string{key: "one"},
		}
		Expect(k8sClient.Create(ctx, token)).To(Succeed())

		claim := testClaim(name, pool)
		claim.Spec.SecretRefs = []corev1.LocalObjectReference{{Name: token.Name}}
		Expect(k8sClient.Create(ctx, claim)).To(Succeed())
		enclave := boundEnclaveName(name)

		Eventually(func(g Gomega) {
			g.Expect(claimSecretData(g, enclave)).To(Equal(map[string][]byte{
				enclavev1alpha1.ClaimNameKey: []byte(name),
				key:                          []byte("one"),
			}))
		}).Should(Succeed())

		By("following changes to the source Secret")
		token.StringData = map[string]string{key: "two"}
		Expect(k8sClient.Update(ctx, token)).To(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(claimSecretData(g, enclave)).To(HaveKeyWithValue(key, []byte("two")))
		}).Should(Succeed())
	})

	It("reports missing Secrets and conflicting keys without overwriting", func() {
		warmPool(pool)
		first := &corev1.Secret{
			Name: uniqueName("first"), Namespace: testNamespace,
			StringData: map[string]string{"KEY": "first"},
		}
		second := &corev1.Secret{
			Name: uniqueName("second"), Namespace: testNamespace,
			StringData: map[string]string{"KEY": "second"},
		}
		Expect(k8sClient.Create(ctx, first)).To(Succeed())
		Expect(k8sClient.Create(ctx, second)).To(Succeed())

		claim := testClaim(name, pool)
		claim.Spec.SecretRefs = []corev1.LocalObjectReference{
			{Name: first.Name}, {Name: second.Name}, {Name: "missing"},
		}
		Expect(k8sClient.Create(ctx, claim)).To(Succeed())
		enclave := boundEnclaveName(name)

		Eventually(func(g Gomega) {
			g.Expect(claimSecretData(g, enclave)).To(HaveKeyWithValue("KEY", []byte("first")))
			g.Expect(getClaim(g, name).Status.Conditions).To(ContainElement(And(
				HaveField("Type", enclavev1alpha1.ConditionSecretsProjected),
				HaveField("Status", metav1.ConditionFalse),
				HaveField("Message", And(
					ContainSubstring("key KEY in Secret "+second.Name),
					ContainSubstring("Secret missing not found"),
				)),
			)))
		}).Should(Succeed())
	})

	It("reports Lost when its Enclave is deleted", func() {
		warmPool(pool)
		Expect(k8sClient.Create(ctx, testClaim(name, pool))).To(Succeed())
		enclave := getEnclave(Default, boundEnclaveName(name))

		Expect(k8sClient.Delete(ctx, enclave)).To(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(getClaim(g, name).Status.Phase).To(Equal(enclavev1alpha1.ClaimLost))
		}).Should(Succeed())
	})
})
