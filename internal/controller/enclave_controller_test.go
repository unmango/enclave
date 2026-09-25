/*
Copyright 2026 UnMango.

Licensed under the MIT License. See LICENSE in the project root for details.
*/

package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	enclavev1alpha1 "github.com/unmango/enclave/api/v1alpha1"
)

func getEnclave(g Gomega, name string) *enclavev1alpha1.Enclave {
	enclave := &enclavev1alpha1.Enclave{}
	g.Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: name}, enclave)).To(Succeed())
	return enclave
}

func getPod(g Gomega, name string) *corev1.Pod {
	pod := &corev1.Pod{}
	g.Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: name}, pod)).To(Succeed())
	return pod
}

// setPodPhase stands in for the kubelet, which envtest does not run.
func setPodPhase(name string, phase corev1.PodPhase, ready bool) {
	Eventually(func(g Gomega) {
		pod := getPod(g, name)
		status := corev1.ConditionFalse
		if ready {
			status = corev1.ConditionTrue
		}
		pod.Status.Phase = phase
		pod.Status.Conditions = []corev1.PodCondition{{
			Type: corev1.PodReady, Status: status, LastTransitionTime: metav1.Now(),
		}}
		g.Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())
	}).Should(Succeed())
}

var _ = Describe("Enclave Controller", func() {
	var name string

	BeforeEach(func() {
		name = uniqueName("enclave")
	})

	It("creates the Pod and claim Secret and reports Pending", func() {
		Expect(k8sClient.Create(ctx, testEnclave(name))).To(Succeed())

		Eventually(func(g Gomega) {
			enclave := getEnclave(g, name)
			g.Expect(enclave.Status.PodName).To(Equal(name))
			g.Expect(enclave.Status.Phase).To(Equal(enclavev1alpha1.EnclavePending))
		}).Should(Succeed())

		secret := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: name + "-claim"}, secret)).To(Succeed())
		Expect(secret.Data).To(BeEmpty())

		pod := getPod(Default, name)
		Expect(pod.Labels).To(HaveKeyWithValue(enclavev1alpha1.EnclaveLabel, name))
		Expect(metav1.IsControlledBy(pod, getEnclave(Default, name))).To(BeTrue())
		Expect(pod.Spec.Volumes).To(ContainElement(HaveField("EmptyDir", Not(BeNil()))))
		Expect(pod.Spec.Containers[0].VolumeMounts).To(ConsistOf(
			corev1.VolumeMount{Name: workspaceVolume, MountPath: "/workspace"},
			corev1.VolumeMount{Name: claimVolume, MountPath: enclavev1alpha1.ClaimMountPath, ReadOnly: true},
		))
	})

	It("follows the Pod through Ready, Bound and Failed", func() {
		Expect(k8sClient.Create(ctx, testEnclave(name))).To(Succeed())
		Eventually(func(g Gomega) { getPod(g, name) }).Should(Succeed())

		setPodPhase(name, corev1.PodRunning, true)
		Eventually(func(g Gomega) {
			enclave := getEnclave(g, name)
			g.Expect(enclave.Status.Phase).To(Equal(enclavev1alpha1.EnclaveReady))
			g.Expect(enclave.Status.Conditions).To(ContainElement(And(
				HaveField("Type", enclavev1alpha1.ConditionReady),
				HaveField("Status", metav1.ConditionTrue),
			)))
		}).Should(Succeed())

		enclave := getEnclave(Default, name)
		enclave.Spec.ClaimRef = &corev1.LocalObjectReference{Name: "someone"}
		Expect(k8sClient.Update(ctx, enclave)).To(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(getEnclave(g, name).Status.Phase).To(Equal(enclavev1alpha1.EnclaveBound))
		}).Should(Succeed())

		setPodPhase(name, corev1.PodFailed, false)
		Eventually(func(g Gomega) {
			g.Expect(getEnclave(g, name).Status.Phase).To(Equal(enclavev1alpha1.EnclaveFailed))
		}).Should(Succeed())
	})

	It("backs the workspace with a PersistentVolumeClaim when storage is set", func() {
		enclave := testEnclave(name)
		enclave.Spec.Workspace = &enclavev1alpha1.WorkspaceSpec{
			MountPath: "/home/dev",
			Storage:   &enclavev1alpha1.WorkspaceStorage{Size: resource.MustParse("1Gi")},
		}
		Expect(k8sClient.Create(ctx, enclave)).To(Succeed())

		Eventually(func(g Gomega) {
			pvc := &corev1.PersistentVolumeClaim{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: name + "-workspace"}, pvc)).
				To(Succeed())
			g.Expect(pvc.Spec.AccessModes).To(ConsistOf(corev1.ReadWriteOnce))
			g.Expect(pvc.Spec.Resources.Requests.Storage().String()).To(Equal("1Gi"))
		}).Should(Succeed())

		Eventually(func(g Gomega) {
			pod := getPod(g, name)
			g.Expect(pod.Spec.Volumes).To(ContainElement(HaveField(
				"PersistentVolumeClaim", HaveValue(HaveField("ClaimName", name+"-workspace")),
			)))
			g.Expect(pod.Spec.Containers[0].VolumeMounts).To(ContainElement(
				corev1.VolumeMount{Name: workspaceVolume, MountPath: "/home/dev"},
			))
			g.Expect(getEnclave(g, name).Status.Conditions).To(ContainElement(And(
				HaveField("Type", enclavev1alpha1.ConditionWorkspaceReady),
				HaveField("Status", metav1.ConditionFalse),
			)))
		}).Should(Succeed())
	})

	It("creates a ServiceAccount and one RoleBinding per roleRef", func() {
		view := clusterRoleRef("view")
		edit := clusterRoleRef("edit")
		enclave := testEnclave(name)
		enclave.Spec.ServiceAccount = &enclavev1alpha1.ServiceAccountSpec{RoleRefs: []rbacv1.RoleRef{view, edit}}
		Expect(k8sClient.Create(ctx, enclave)).To(Succeed())

		bindings := func(g Gomega) []rbacv1.RoleBinding {
			list := &rbacv1.RoleBindingList{}
			g.Expect(k8sClient.List(ctx, list, client.InNamespace(testNamespace),
				client.MatchingLabels{enclavev1alpha1.EnclaveLabel: name})).To(Succeed())
			return list.Items
		}

		Eventually(func(g Gomega) {
			sa := &corev1.ServiceAccount{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: name}, sa)).To(Succeed())
			g.Expect(getPod(g, name).Spec.ServiceAccountName).To(Equal(name))
			g.Expect(bindings(g)).To(ConsistOf(
				HaveField("RoleRef", view),
				HaveField("RoleRef", edit),
			))
		}).Should(Succeed())

		Eventually(func(g Gomega) {
			enclave := getEnclave(g, name)
			enclave.Spec.ServiceAccount.RoleRefs = []rbacv1.RoleRef{view}
			g.Expect(k8sClient.Update(ctx, enclave)).To(Succeed())
		}).Should(Succeed())

		Eventually(func(g Gomega) {
			g.Expect(bindings(g)).To(ConsistOf(HaveField("RoleRef", view)))
		}).Should(Succeed())

		Eventually(func(g Gomega) {
			enclave := getEnclave(g, name)
			enclave.Spec.ServiceAccount = nil
			g.Expect(k8sClient.Update(ctx, enclave)).To(Succeed())
		}).Should(Succeed())

		Eventually(func(g Gomega) {
			g.Expect(bindings(g)).To(BeEmpty())
			err := k8sClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: name}, &corev1.ServiceAccount{})
			g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
		}).Should(Succeed())
	})

	DescribeTable("refuses objects it does not control",
		func(existing func(name string) client.Object) {
			Expect(k8sClient.Create(ctx, existing(name))).To(Succeed())
			Expect(k8sClient.Create(ctx, testEnclave(name))).To(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(getEnclave(g, name).Status.Conditions).To(ContainElement(And(
					HaveField("Type", enclavev1alpha1.ConditionReady),
					HaveField("Status", metav1.ConditionFalse),
					HaveField("Reason", "Conflict"),
				)))
			}).Should(Succeed())
		},
		Entry("a Pod", func(name string) client.Object {
			return &corev1.Pod{
				Name: name, Namespace: testNamespace,
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "other", Image: "busybox"}}},
			}
		}),
		Entry("a claim Secret", func(name string) client.Object {
			return &corev1.Secret{Name: name + "-claim", Namespace: testNamespace}
		}),
	)

	It("does not bind roles to a ServiceAccount it does not control", func() {
		Expect(k8sClient.Create(ctx, &corev1.ServiceAccount{Name: name, Namespace: testNamespace})).To(Succeed())
		enclave := testEnclave(name)
		enclave.Spec.ServiceAccount = &enclavev1alpha1.ServiceAccountSpec{RoleRefs: []rbacv1.RoleRef{clusterRoleRef("view")}}
		Expect(k8sClient.Create(ctx, enclave)).To(Succeed())

		Eventually(func(g Gomega) {
			g.Expect(getEnclave(g, name).Status.Conditions).To(ContainElement(HaveField("Reason", "Conflict")))
		}).Should(Succeed())
		list := &rbacv1.RoleBindingList{}
		Expect(k8sClient.List(ctx, list, client.InNamespace(testNamespace),
			client.MatchingLabels{enclavev1alpha1.EnclaveLabel: name})).To(Succeed())
		Expect(list.Items).To(BeEmpty())
	})

	It("clones repositories in an init container that runs as the first container", func() {
		enclave := testEnclave(name)
		enclave.Spec.Template.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{
			RunAsUser: new(int64(1000)),
		}
		enclave.Spec.Repositories = []enclavev1alpha1.Repository{
			{Name: "public", URL: "https://example.com/public.git", Ref: "main"},
			{
				Name: "private", URL: "git@example.com:private.git", Path: "src/private",
				CredentialsSecretRef: &corev1.LocalObjectReference{Name: "git-creds"},
			},
		}
		Expect(k8sClient.Create(ctx, enclave)).To(Succeed())

		Eventually(func(g Gomega) {
			pod := getPod(g, name)
			g.Expect(pod.Spec.InitContainers).To(HaveLen(1))
			clone := pod.Spec.InitContainers[0]
			g.Expect(clone.Name).To(Equal(cloneContainer))
			g.Expect(clone.Image).To(Equal(DefaultGitImage))
			g.Expect(clone.SecurityContext.RunAsUser).To(HaveValue(BeEquivalentTo(1000)))
			g.Expect(clone.Env).To(ContainElements(
				corev1.EnvVar{Name: "REPO_COUNT", Value: "2"},
				corev1.EnvVar{Name: "REPO_0_URL", Value: "https://example.com/public.git"},
				corev1.EnvVar{Name: "REPO_0_REF", Value: "main"},
				corev1.EnvVar{Name: "REPO_0_DEST", Value: "/workspace/public"},
				corev1.EnvVar{Name: "REPO_1_DEST", Value: "/workspace/src/private"},
				corev1.EnvVar{Name: "REPO_1_CREDS", Value: "/var/run/enclave/git/private"},
			))
			g.Expect(pod.Spec.Volumes).To(ContainElement(HaveField(
				"Secret", HaveValue(HaveField("SecretName", "git-creds")),
			)))
		}).Should(Succeed())
	})
})
