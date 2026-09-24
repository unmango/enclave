#!/usr/bin/env bash

# Scaffold the project with kubebuilder. Run it once, from the dev shell.
#
#   DOMAIN=unmango.dev REPO=github.com/unmango/my-operator ./hack/init.sh

set -euo pipefail

DOMAIN=${DOMAIN:-example.com}
REPO=${REPO:-github.com/example/my-operator}
OWNER=${OWNER:-example}

# kubebuilder scaffolds files this template already provides, so they are moved
# aside for the duration of the init and put back afterwards.
OURS=(Makefile .gitignore .golangci.yml .editorconfig)

restore() {
  for f in "${OURS[@]}"; do
    if [ -e "$f.template" ]; then mv -f "$f.template" "$f"; fi
  done
}

# A failed init would otherwise leave the template holding Makefile.template
# and friends.
trap restore EXIT

for f in "${OURS[@]}"; do
  if [ -e "$f" ]; then mv "$f" "$f.template"; fi
done

kubebuilder init \
  --domain "$DOMAIN" \
  --repo "$REPO" \
  --owner "$OWNER" \
  --plugins go/v4 \
  --license apache2

restore
trap - EXIT

# The manager image is built by nix/image.nix, and .github/workflows/ci.yml
# already covers lint, test, and e2e through the dev shell.
rm -f Dockerfile .dockerignore
rm -f .github/workflows/lint.yml .github/workflows/test.yml .github/workflows/test-e2e.yml

# The dev shell already carries every tool the devcontainer installs.
rm -rf .devcontainer

make tidy

# Add APIs and controllers from here, for example:
#
#   kubebuilder create api \
#     --group widgets \
#     --version v1alpha1 \
#     --kind Widget \
#     --resource \
#     --controller
#
# To watch a type owned by someone else, scaffold the controller without the
# resource:
#
#   kubebuilder create api \
#     --group networking \
#     --version v1 \
#     --kind Ingress \
#     --resource=false \
#     --controller
