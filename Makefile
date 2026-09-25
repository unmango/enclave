# Every tool below comes from the nix dev shell. Run `direnv allow` once, or
# prefix invocations with `nix develop -c` when the shell is not loaded.
GO             ?= go
GINKGO         ?= ginkgo
GOMOD2NIX      ?= gomod2nix
CONTROLLER_GEN ?= controller-gen
GOLANGCI_LINT  ?= golangci-lint
HELM           ?= helm
KIND           ?= kind
KUBECTL        ?= kubectl
KUSTOMIZE      ?= kustomize
SKOPEO         ?= skopeo

GO_SRC ?= $(shell find . -name '*.go')

SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: build

##@ General

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: manifests
manifests: ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd:generateEmbeddedObjectMeta=true webhook paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: ## Generate DeepCopy implementations and mocks.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."
	$(GO) generate ./...

.PHONY: fmt format
fmt format: ## Run nix fmt against code.
	nix fmt

.PHONY: vet
vet: ## Run go vet against code.
	$(GO) vet ./...

.PHONY: test
test: manifests generate vet ## Run unit and envtest suites.
	$(GINKGO) run -r --skip-package=test

.PHONY: lint
lint: ## Run golangci-lint linter.
	$(GOLANGCI_LINT) run

.PHONY: lint-fix
lint-fix: ## Run golangci-lint linter and perform fixes.
	$(GOLANGCI_LINT) run --fix

.PHONY: lint-config
lint-config: ## Verify golangci-lint linter configuration.
	$(GOLANGCI_LINT) config verify

##@ E2E

KIND_CLUSTER ?= enclave-e2e

# The e2e suite installs CRDs and a Deployment into whatever cluster the ambient
# kubeconfig points at. Write the Kind credentials to a file of their own and
# export KUBECONFIG for the suite so it cannot reach a real cluster.
E2E_KUBECONFIG ?= $(CURDIR)/bin/$(KIND_CLUSTER).kubeconfig

define cleanup-e2e-cluster
$(KIND) delete cluster --name $(KIND_CLUSTER); rm -f $(E2E_KUBECONFIG)
endef

.PHONY: setup-test-e2e
setup-test-e2e: | bin ## Create the Kind cluster used for e2e tests if it does not exist.
	@if $(KIND) get clusters | grep -qxF '$(KIND_CLUSTER)'; then \
		echo "Kind cluster '$(KIND_CLUSTER)' already exists."; \
	else \
		$(KIND) create cluster --name $(KIND_CLUSTER) --config hack/kind-config.yaml; \
	fi
	$(KIND) export kubeconfig --name $(KIND_CLUSTER) --kubeconfig $(E2E_KUBECONFIG)

.PHONY: test-e2e
# The trap is set before any prerequisite runs, so a failing codegen step or
# suite still tears down a cluster this run created. A cluster that already
# existed is left in place.
test-e2e: ## Run the e2e tests against a Kind cluster.
	if ! $(KIND) get clusters | grep -qxF '$(KIND_CLUSTER)'; then \
		trap '$(cleanup-e2e-cluster)' EXIT; \
	fi; \
	$(MAKE) setup-test-e2e manifests generate vet; \
	KUBECONFIG=$(E2E_KUBECONFIG) KIND=$(KIND) KIND_CLUSTER=$(KIND_CLUSTER) $(GO) test -tags=e2e ./test/e2e/ -v -ginkgo.v

.PHONY: cleanup-test-e2e
cleanup-test-e2e: ## Tear down the Kind cluster used for e2e tests.
	@$(cleanup-e2e-cluster)

##@ Build

.PHONY: build
build: ## Build the operator with nix.
	nix build .#

.PHONY: run
run: manifests generate vet ## Run a controller from your host.
	$(GO) run ./cmd/main.go

.PHONY: dist build-installer
# The helm plugin shells out to `make build-installer` by name, so that name has
# to stay even though `dist` reads better at the command line.
dist build-installer: manifests generate ## Generate a consolidated YAML with CRDs and deployment.
	mkdir -p dist
	$(KUSTOMIZE) build config/default > dist/install.yaml

##@ Chart

HELM_RELEASE   ?= enclave
HELM_NAMESPACE ?= enclave-system
CHART          ?= dist/chart

.PHONY: helm-lint
helm-lint: ## Lint the chart and render it with the default values.
	$(HELM) lint $(CHART)
	$(HELM) template $(HELM_RELEASE) $(CHART) > /dev/null

.PHONY: helm-deploy
# Defining this target also stops `kubebuilder edit` from appending its own helm
# deployment section, which installs helm by piping curl into bash.
helm-deploy: ## Install or upgrade the chart in the cluster specified in ~/.kube/config.
	$(HELM) upgrade --install $(HELM_RELEASE) $(CHART) \
		--namespace $(HELM_NAMESPACE) --create-namespace \
		--history-max 3 --wait --timeout 5m

##@ Image

# The image is built by nix/image.nix, which produces a script that streams the
# tarball to stdout rather than a tarball in the store.
hack/stream-image: flake.nix nix/image.nix nix/default.nix
	nix build .#image --out-link hack/stream-image

bin:
	mkdir -p bin

# A real file target, so publishing several tags streams the image once.
bin/image.tar: hack/stream-image | bin
	./hack/stream-image > bin/image.tar

.PHONY: image-tar
image-tar: bin/image.tar ## Stream the image to bin/image.tar.

.PHONY: kind-load
kind-load: hack/stream-image ## Load the image into the kind cluster.
	./hack/stream-image | $(KIND) load image-archive /dev/stdin --name $(KIND_CLUSTER)

# nix/image.nix always tags the archive `latest`; the tag that matters is the
# destination one, which skopeo sets on the way out. That keeps the local name
# config/manager and `make kind-load` expect free of the release version.
PUSH_IMAGE ?= ghcr.io/unmango/enclave
IMAGE_TAG  ?= latest

.PHONY: push-image
push-image: bin/image.tar ## Push the image to $(PUSH_IMAGE):$(IMAGE_TAG), writing its digest to bin/image.digest.
	$(SKOPEO) copy --digestfile bin/image.digest docker-archive:bin/image.tar 'docker://$(PUSH_IMAGE):$(IMAGE_TAG)'

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: install
# A large CRD bundle has to be piped straight into kubectl; holding it in a
# shell variable first can exceed the exec argument limit.
#
# It also goes in server-side. A client-side apply records the whole object in
# the last-applied-configuration annotation, and a CRD past the 256KiB the API
# server allows for annotations is rejected.
install: manifests ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) apply --server-side --force-conflicts -f -

.PHONY: uninstall
uninstall: manifests ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: deploy
deploy: manifests ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/default | $(KUBECTL) apply --server-side --force-conflicts -f -

.PHONY: undeploy
undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/default | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f -

##@ Nix

.PHONY: update
update: ## Update nix flake inputs.
	nix flake update

.PHONY: check
check: ## Run nix flake checks.
	nix flake check

.PHONY: tidy
tidy: go.sum nix/gomod2nix.toml ## Tidy go modules and regenerate the gomod2nix lock.

go.sum: go.mod ${GO_SRC}
	$(GO) mod tidy

nix/gomod2nix.toml: go.sum ${GO_SRC}
	$(GOMOD2NIX) generate --dir ${CURDIR} --outdir ${@D}
