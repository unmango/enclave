/*
Copyright 2026 UnMango.

Licensed under the MIT License. See LICENSE in the project root for details.
*/

package controller

import (
	"os"
	"os/exec"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	enclavev1alpha1 "github.com/unmango/enclave/api/v1alpha1"
)

var _ = Describe("Clone script", func() {
	var workspace, origin string

	git := func(dir string, args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
		)
		out, err := cmd.CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), string(out))
	}

	// run executes the init container's command on the host, with its paths
	// rooted in a temporary workspace.
	run := func(repos ...enclavev1alpha1.Repository) string {
		env := enclavev1alpha1.EnvironmentSpec{Repositories: repos}
		clone, _ := cloneInitContainer(&env, workspace, DefaultGitImage)
		cmd := exec.Command(clone.Command[0], clone.Command[1:]...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
		for _, v := range clone.Env {
			if v.Name != "HOME" {
				cmd.Env = append(cmd.Env, v.Name+"="+v.Value)
			}
		}
		out, err := cmd.CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), string(out))
		return string(out)
	}

	BeforeEach(func() {
		if _, err := exec.LookPath("git"); err != nil {
			Skip("git is not installed")
		}
		workspace = GinkgoT().TempDir()
		origin = GinkgoT().TempDir()
		git(origin, "init", "--initial-branch=main")
		Expect(os.WriteFile(filepath.Join(origin, "README"), []byte("hi"), 0o644)).To(Succeed())
		git(origin, "add", "README")
		git(origin, "commit", "-m", "init")
		git(origin, "branch", "feature")
	})

	It("clones each repository to its path and ref", func() {
		run(
			enclavev1alpha1.Repository{Name: "a", URL: origin},
			enclavev1alpha1.Repository{Name: "b", URL: origin, Ref: "feature", Path: "nested/b"},
		)

		Expect(filepath.Join(workspace, "a", "README")).To(BeARegularFile())
		head, err := os.ReadFile(filepath.Join(workspace, "nested", "b", ".git", "HEAD"))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(head)).To(ContainSubstring("refs/heads/feature"))
	})

	It("leaves a repository that is already present alone", func() {
		repo := enclavev1alpha1.Repository{Name: "a", URL: origin}
		run(repo)
		Expect(os.WriteFile(filepath.Join(workspace, "a", "local"), []byte("edit"), 0o644)).To(Succeed())

		Expect(run(repo)).To(ContainSubstring("Skipping"))
		Expect(filepath.Join(workspace, "a", "local")).To(BeARegularFile())
	})

	It("does not let the shell interpret repository values", func() {
		marker := filepath.Join(workspace, "pwned")
		repo := enclavev1alpha1.Repository{Name: "a", URL: origin + "$(touch " + marker + ")"}
		env := enclavev1alpha1.EnvironmentSpec{Repositories: []enclavev1alpha1.Repository{repo}}
		clone, _ := cloneInitContainer(&env, workspace, DefaultGitImage)
		cmd := exec.Command(clone.Command[0], clone.Command[1:]...)
		for _, v := range clone.Env {
			cmd.Env = append(cmd.Env, v.Name+"="+v.Value)
		}
		cmd.Env = append(cmd.Env, "PATH="+os.Getenv("PATH"))

		Expect(cmd.Run()).NotTo(Succeed())
		Expect(marker).NotTo(BeAnExistingFile())
	})
})
