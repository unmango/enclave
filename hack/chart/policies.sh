#!/usr/bin/env bash

# The kubebuilder helm plugin writes a ValidatingAdmissionPolicy and its binding
# to the same file under templates/extras, so the binding overwrites the policy
# and the chart installs bindings to policies that do not exist. Nothing would
# check Enclave, EnclavePool or EnclaveClaim authors.
#
# This removes those files and renders config/policy into templates/policy
# instead, templating the object names and the operator's ServiceAccount.
# It fails rather than emitting a chart whose policies do not match config/.

set -euo pipefail

chart="${1:?usage: policies.sh <chart dir>}"
out="$chart/templates/policy"
operator="system:serviceaccount:enclave-system:enclave-controller-manager"

rm -f "$chart"/templates/extras/*-authorization.yaml
rmdir "$chart/templates/extras" 2>/dev/null || true
rm -rf "$out"
mkdir -p "$out"

for src in config/policy/*_policy.yaml; do
  dest="$out/$(basename "$src")"
  sed -E \
    -e 's/^(  )(name|policyName): (.+)$/\1\2: {{ include "enclave.resourceName" (dict "suffix" "\3" "context" $) }}/' \
    -e "s/'$operator'/'system:serviceaccount:{{ .Release.Namespace }}:{{ include \"enclave.serviceAccountName\" . }}'/" \
    "$src" >"$dest"

  # Each file holds one policy and one binding: two names and one policyName.
  names=$(grep -c 'include "enclave.resourceName"' "$dest" || true)
  if [[ $names -ne 3 ]]; then
    echo "policies: expected 3 templated names in $dest, found $names" >&2
    exit 1
  fi
  if grep -q 'not-the-operator' "$dest" && ! grep -q '{{ .Release.Namespace }}' "$dest"; then
    echo "policies: $src exempts an operator ServiceAccount other than $operator" >&2
    exit 1
  fi
done
