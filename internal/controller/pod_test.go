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

var _ = Describe("Clone init container", func() {
	It("gives the clone a writable /tmp", func() {
		env := enclavev1alpha1.EnvironmentSpec{Repositories: []enclavev1alpha1.Repository{
			{Name: "a", URL: "https://example.com/a.git"},
		}}
		clone, volumes := cloneInitContainer(&env, "/workspace", DefaultGitImage)

		Expect(clone.VolumeMounts).To(ContainElement(HaveField("MountPath", "/tmp")))
		Expect(volumes).To(ContainElement(HaveField("EmptyDir", Not(BeNil()))))
	})

	It("keeps credential volume names within the DNS label limit", func() {
		env := enclavev1alpha1.EnvironmentSpec{Repositories: []enclavev1alpha1.Repository{{
			Name: strings.Repeat("a", 63), URL: "https://example.com/a.git",
			CredentialsSecretRef: &corev1.LocalObjectReference{Name: "creds"},
		}}}
		_, volumes := cloneInitContainer(&env, "/workspace", DefaultGitImage)

		for _, v := range volumes {
			Expect(len(v.Name)).To(BeNumerically("<=", 63), v.Name)
		}
	})
})
