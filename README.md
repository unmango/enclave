# enclave

A Kubernetes operator for development environments inside a cluster.

An environment is a pod plus the state around it, either drawn from a warm pool kept ready ahead of demand or provisioned on request.
The pool exists so that claiming an environment costs a scheduling decision rather than a cold start.

- `Enclave` is one environment: a Pod, a workspace volume, cloned git repositories, and optionally a ServiceAccount.
- `EnclavePool` keeps a number of unbound Enclaves warm.
- `EnclaveClaim` binds one Enclave from a pool and hands it Secrets. Deleting the claim deletes the Enclave.

## Development

```bash
nix develop
make help
```

- `nix build .#` builds the manager, and `nix build .#image` builds a script that streams its container image.
- `make test` runs the unit and envtest suites with `KUBEBUILDER_ASSETS` from the dev shell.
- `make test-e2e` creates a Kind cluster, loads the image, deploys the operator, and tears the cluster down afterwards.

The Kubernetes package set is pinned by `k8sVersion` in `flake.nix`.

## Usage

Create a pool of warm environments, then claim one:

```yaml
apiVersion: enclave.unmango.dev/v1alpha1
kind: EnclavePool
metadata:
  name: dev
spec:
  replicas: 2
  template:
    spec:
      workspace:
        storage:
          size: 10Gi
      repositories:
        - name: enclave
          url: https://github.com/unmango/enclave.git
      template:
        spec:
          containers:
            - name: dev
              image: golang:1.27
              command: ["sleep", "infinity"]
---
apiVersion: enclave.unmango.dev/v1alpha1
kind: EnclaveClaim
metadata:
  name: alice
spec:
  poolRef:
    name: dev
```

```bash
kubectl get enclaveclaim alice          # PHASE Bound, ENCLAVE dev-xxxxx
kubectl exec -it dev-xxxxx -- bash      # the Pod has the Enclave's name
kubectl delete enclaveclaim alice       # deletes the Enclave; the pool refills
```

`docs/design.md` covers binding, Secret projection, template changes, and the RBAC the operator holds.
`config/samples/claude-remote-control.yaml` runs Claude Code Remote Control in claimed environments.
