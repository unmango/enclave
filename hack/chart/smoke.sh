#!/usr/bin/env bash

# Installs dist/chart into the current cluster and checks that its admission
# policies are enforced: a user who may create EnclaveClaims but not read a
# Secret must not be able to claim that Secret.

set -euo pipefail

chart="${1:?usage: smoke.sh <chart dir> <image repository> <image tag>}"
repository="${2:?usage: smoke.sh <chart dir> <image repository> <image tag>}"
tag="${3:?usage: smoke.sh <chart dir> <image repository> <image tag>}"
ns=enclave-smoke
user=enclave-smoke-user

helm upgrade --install enclave "$chart" \
  --namespace enclave-system --create-namespace \
  --set manager.image.repository="$repository" \
  --set manager.image.tag="$tag" \
  --set manager.image.pullPolicy=Never \
  --wait --timeout 5m

policies=$(kubectl get validatingadmissionpolicies -o name | grep -c -- '-authorization$' || true)
if [[ $policies -ne 3 ]]; then
  echo "smoke: expected 3 admission policies, found $policies" >&2
  exit 1
fi

kubectl create namespace "$ns" --dry-run=client -o yaml | kubectl apply -f -
kubectl -n "$ns" create secret generic private --from-literal=token=hunter2 \
  --dry-run=client -o yaml | kubectl apply -f -
kubectl -n "$ns" create role claimant --verb=create --resource=enclaveclaims.enclave.unmango.dev \
  --dry-run=client -o yaml | kubectl apply -f -
kubectl -n "$ns" create rolebinding claimant --role=claimant --user="$user" \
  --dry-run=client -o yaml | kubectl apply -f -

claim=$(
  cat <<-YAML
	apiVersion: enclave.unmango.dev/v1alpha1
	kind: EnclaveClaim
	metadata:
	  name: smoke
	  namespace: $ns
	spec:
	  poolRef:
	    name: smoke
	  secretRefs:
	  - name: private
	YAML
)

# A new policy takes a few seconds to reach the API server's admission cache.
for _ in $(seq 30); do
  if out=$(kubectl create --as="$user" -f - <<<"$claim" 2>&1); then
    echo "smoke: a user without get on Secret private created a claim for it" >&2
    kubectl -n "$ns" delete enclaveclaim smoke --ignore-not-found
    sleep 2
    continue
  fi
  if grep -q 'secretRefs may only name Secrets you have permission to get' <<<"$out"; then
    echo "smoke: claim policy enforced"
    exit 0
  fi
  echo "$out" >&2
  exit 1
done

echo "smoke: claim policy was never enforced" >&2
exit 1
