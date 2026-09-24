# my-operator

A Kubernetes operator, primed for `kubebuilder init`.

## Getting started

```bash
nix develop
DOMAIN=example.com REPO=github.com/example/my-operator ./hack/init.sh
```

`hack/init.sh` runs `kubebuilder init`, preserving the files this template owns, and regenerates `go.sum` and `nix/gomod2nix.toml`.
Until it has run there is no `go.mod`, so `nix build .#` and `nix flake check` will fail.

After that, `make help` lists the targets, and `kubebuilder create api` adds APIs and controllers.

## What the flake provides

- `packages.default` builds the manager with `buildGoApplication`.
- `packages.image` streams a manager container image from `nix/image.nix`. There is no Dockerfile.
- `packages.envtest-assets` is a directory of `etcd`, `kube-apiserver`, and `kubectl` from [kubepkgs](https://github.com/unmango/kubepkgs), exported as `KUBEBUILDER_ASSETS` in the dev shell. `setup-envtest` never has to download anything.
- The dev shell carries `kubebuilder`, `controller-gen`, `kustomize`, `kubectl`, `kind`, `helm`, `ginkgo`, `golangci-lint`, and `skopeo`, and sets the matching `CONTROLLER_GEN`, `KUSTOMIZE`, `KUBECTL`, `KIND`, `HELM`, and `ENVTEST` variables.

The Kubernetes package set is pinned by `k8sVersion` in `flake.nix`.

## Placeholders to replace

`my-operator` in `nix/image.nix`, `Makefile`, and this file; `pname` in `nix/default.nix`; `PUSH_IMAGE` in the `Makefile`.
