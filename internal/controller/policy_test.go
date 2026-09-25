/*
Copyright 2026 UnMango.

Licensed under the MIT License. See LICENSE in the project root for details.
*/

package controller

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	enclavev1alpha1 "github.com/unmango/enclave/api/v1alpha1"
)

// installPolicies creates the admission policies from config/policy, which
// envtest does not load on its own.
func installPolicies() {
	paths, err := filepath.Glob(filepath.Join("..", "..", "config", "policy", "*_policy.yaml"))
	Expect(err).NotTo(HaveOccurred())
	Expect(paths).NotTo(BeEmpty())
	for _, path := range paths {
		f, err := os.Open(path)
		Expect(err).NotTo(HaveOccurred())
		decoder := yaml.NewYAMLOrJSONDecoder(f, 4096)
		for {
			obj := &unstructured.Unstructured{}
			if err := decoder.Decode(&obj.Object); err != nil {
				break
			}
			if len(obj.Object) > 0 {
				Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			}
		}
		Expect(f.Close()).To(Succeed())
	}
}

var _ = Describe("Admission policy", func() {
	const (
		unbindable = "roleRefs may only name roles you have permission to bind"
		unreadable = "secretRefs may only name Secrets you have permission to get"
	)
	view := clusterRoleRef("view")
	admin := clusterRoleRef("admin")

	var user string
	var author client.Client

	// grant binds a new ClusterRole holding rules to the author in the test namespace.
	grant := func(rules ...rbacv1.PolicyRule) {
		role := &rbacv1.ClusterRole{Name: uniqueName("policy-test"), Rules: rules}
		Expect(k8sClient.Create(ctx, role)).To(Succeed())
		Expect(k8sClient.Create(ctx, &rbacv1.RoleBinding{
			Name: role.Name, Namespace: testNamespace,
			RoleRef:  clusterRoleRef(role.Name),
			Subjects: []rbacv1.Subject{{Kind: rbacv1.UserKind, APIGroup: rbacv1.GroupName, Name: user}},
		})).To(Succeed())
	}
	canCreatePods := rbacv1.PolicyRule{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"create"}}
	canBind := func(ref rbacv1.RoleRef) rbacv1.PolicyRule {
		resource := "clusterroles"
		if ref.Kind == "Role" {
			resource = "roles"
		}
		return rbacv1.PolicyRule{
			APIGroups: []string{rbacv1.GroupName}, Resources: []string{resource},
			ResourceNames: []string{ref.Name}, Verbs: []string{"bind"},
		}
	}

	// grantBindable creates n ClusterRole references the author may bind.
	grantBindable := func(n int) []rbacv1.RoleRef {
		refs := make([]rbacv1.RoleRef, n)
		rules := make([]rbacv1.PolicyRule, n)
		for i := range refs {
			refs[i] = clusterRoleRef(uniqueName("many"))
			rules[i] = canBind(refs[i])
		}
		grant(rules...)
		return refs
	}

	canGetSecret := func(name string) rbacv1.PolicyRule {
		return rbacv1.PolicyRule{
			APIGroups: []string{""}, Resources: []string{"secrets"},
			ResourceNames: []string{name}, Verbs: []string{"get"},
		}
	}
	claimWith := func(secrets ...string) *enclavev1alpha1.EnclaveClaim {
		claim := testClaim(uniqueName("policy"), "pool")
		for _, name := range secrets {
			claim.Spec.SecretRefs = append(claim.Spec.SecretRefs, corev1.LocalObjectReference{Name: name})
		}
		return claim
	}

	enclaveWith := func(refs ...rbacv1.RoleRef) *enclavev1alpha1.Enclave {
		enclave := testEnclave(uniqueName("policy"))
		enclave.Spec.ServiceAccount = &enclavev1alpha1.ServiceAccountSpec{RoleRefs: refs}
		return enclave
	}

	BeforeEach(func() {
		user = uniqueName("author")
		authn, err := testEnv.AddUser(envtest.User{Name: user}, cfg)
		Expect(err).NotTo(HaveOccurred())
		author, err = client.New(authn.Config(), client.Options{Scheme: k8sClient.Scheme()})
		Expect(err).NotTo(HaveOccurred())
		grant(rbacv1.PolicyRule{
			APIGroups: []string{enclavev1alpha1.GroupVersion.Group},
			Resources: []string{"enclaves", "enclavepools", "enclaveclaims"},
			Verbs:     []string{"get", "create", "update"},
		})
	})

	It("requires permission to create Pods", func() {
		Eventually(func() error {
			return author.Create(ctx, testEnclave(uniqueName("policy")))
		}).Should(MatchError(ContainSubstring("requires permission to create Pods")))

		grant(canCreatePods)
		Eventually(func() error {
			return author.Create(ctx, testEnclave(uniqueName("policy")))
		}).Should(Succeed())
	})

	It("requires bind on each roleRef of an Enclave", func() {
		grant(canCreatePods, canBind(view))

		Eventually(func() error {
			return author.Create(ctx, enclaveWith(view, admin))
		}).Should(MatchError(ContainSubstring(unbindable)))
		Expect(author.Create(ctx, enclaveWith(view))).To(Succeed())
	})

	It("checks Roles in the Enclave's namespace", func() {
		local := rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: uniqueName("local")}
		grant(canCreatePods)

		Eventually(func() error {
			return author.Create(ctx, enclaveWith(local))
		}).Should(MatchError(ContainSubstring(unbindable)))

		grant(canBind(local))
		Eventually(func() error {
			return author.Create(ctx, enclaveWith(local))
		}).Should(Succeed())
	})

	It("rejects adding a roleRef on update", func() {
		grant(canCreatePods, canBind(view))
		enclave := enclaveWith(view)
		Eventually(func() error { return author.Create(ctx, enclave) }).Should(Succeed())

		enclave.Spec.ServiceAccount.RoleRefs = append(enclave.Spec.ServiceAccount.RoleRefs, admin)
		Expect(author.Update(ctx, enclave)).To(MatchError(ContainSubstring(unbindable)))
	})

	It("requires bind on each roleRef of an EnclavePool template", func() {
		grant(canCreatePods)
		pool := testPool(uniqueName("policy"), 0)
		pool.Spec.Template.Spec.ServiceAccount = &enclavev1alpha1.ServiceAccountSpec{RoleRefs: []rbacv1.RoleRef{admin}}

		Eventually(func() error {
			return author.Create(ctx, pool)
		}).Should(MatchError(ContainSubstring(unbindable)))
	})
	It("checks every roleRef of an Enclave with many", func() {
		grant(canCreatePods)
		refs := grantBindable(15)

		Eventually(func() error {
			return author.Create(ctx, enclaveWith(refs...))
		}).Should(Succeed())
		Expect(author.Create(ctx, enclaveWith(append(refs, admin)...))).
			To(MatchError(ContainSubstring(unbindable)))
		Expect(author.Create(ctx, enclaveWith(append(refs, view, view)...))).
			To(MatchError(ContainSubstring("must have at most 16 items")))
	})

	It("checks every roleRef of an EnclavePool template with many", func() {
		grant(canCreatePods)
		refs := grantBindable(16)
		pool := testPool(uniqueName("policy"), 0)
		pool.Spec.Template.Spec.ServiceAccount = &enclavev1alpha1.ServiceAccountSpec{RoleRefs: refs}

		Eventually(func() error {
			return author.Create(ctx, pool)
		}).Should(Succeed())
	})

	It("allows a claim without secretRefs", func() {
		Eventually(func() error {
			return author.Create(ctx, claimWith())
		}).Should(Succeed())
	})

	It("requires get on each Secret a claim projects", func() {
		mine, theirs := uniqueName("mine"), uniqueName("theirs")
		grant(canGetSecret(mine))

		Eventually(func() error {
			return author.Create(ctx, claimWith(mine, theirs))
		}).Should(MatchError(ContainSubstring(unreadable)))
		Expect(author.Create(ctx, claimWith(mine))).To(Succeed())
	})

	It("rejects adding a secretRef on update", func() {
		mine, theirs := uniqueName("mine"), uniqueName("theirs")
		grant(canGetSecret(mine))
		claim := claimWith(mine)
		Eventually(func() error { return author.Create(ctx, claim) }).Should(Succeed())

		claim.Spec.SecretRefs = append(claim.Spec.SecretRefs, corev1.LocalObjectReference{Name: theirs})
		Expect(author.Update(ctx, claim)).To(MatchError(ContainSubstring(unreadable)))
	})

	It("checks every Secret of a claim with many", func() {
		names := make([]string, 16, 17)
		rules := make([]rbacv1.PolicyRule, len(names))
		for i := range names {
			names[i] = uniqueName("many")
			rules[i] = canGetSecret(names[i])
		}
		grant(rules...)

		Eventually(func() error {
			return author.Create(ctx, claimWith(names...))
		}).Should(Succeed())
		Expect(author.Create(ctx, claimWith(append(names, "extra")...))).
			To(MatchError(ContainSubstring("must have at most 16 items")))
	})
})
