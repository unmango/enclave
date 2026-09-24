/*
Copyright 2026 UnMango.

Licensed under the MIT License. See LICENSE in the project root for details.
*/

package controller

import (
	"errors"
	"io"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
)

var _ = Describe("Samples", func() {
	It("are accepted by the API server", func() {
		files, err := filepath.Glob(filepath.Join("..", "..", "config", "samples", "*.yaml"))
		Expect(err).NotTo(HaveOccurred())

		created := 0
		for _, file := range files {
			if filepath.Base(file) == "kustomization.yaml" {
				continue
			}
			f, err := os.Open(file)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(f.Close)

			decoder := yaml.NewYAMLOrJSONDecoder(f, 4096)
			for {
				obj := &unstructured.Unstructured{}
				err := decoder.Decode(&obj.Object)
				if errors.Is(err, io.EOF) {
					break
				}
				Expect(err).NotTo(HaveOccurred(), file)
				if len(obj.Object) == 0 {
					continue
				}
				obj.SetNamespace(testNamespace)
				obj.SetName(uniqueName(obj.GetName()))
				Expect(k8sClient.Create(ctx, obj)).To(Succeed(), "%s: %s %s", file, obj.GetKind(), obj.GetName())
				created++
			}
		}
		Expect(created).To(BeNumerically(">=", 5))
	})
})
