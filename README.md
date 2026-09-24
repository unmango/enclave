# enclave

A Kubernetes operator for development environments inside a cluster.

An environment is a pod plus the state around it, either drawn from a warm pool kept ready ahead of demand or provisioned on request.
The pool exists so that claiming an environment costs a scheduling decision rather than a cold start.

## Development

```bash
nix develop
make help
```

- `nix build .#` builds the manager, and `nix build .#image` builds a script that streams its container image.
- `make test` runs the unit and envtest suites with `KUBEBUILDER_ASSETS` from the dev shell.
- `make test-e2e` creates a Kind cluster, loads the image, deploys the operator, and tears the cluster down afterwards.

The Kubernetes package set is pinned by `k8sVersion` in `flake.nix`.
