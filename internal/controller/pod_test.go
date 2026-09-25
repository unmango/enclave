/*
Copyright 2026 UnMango.

Licensed under the MIT License. See LICENSE in the project root for details.
*/

package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

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
})
