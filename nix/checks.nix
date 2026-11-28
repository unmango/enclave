{
  bash,
  ginkgo,
  git,
  gnumake,
  golangci-lint,
  k8s,
  kubebuilder,
  kubernetes-controller-tools,
  kubernetes-helm,
  mockgen,
  operator,
}:
let
  check =
    name: script:
    operator.overrideAttrs (old: {
      pname = "enclave-${name}";

      nativeBuildInputs = old.nativeBuildInputs ++ [
        ginkgo
        git
        gnumake
        golangci-lint
        k8s.sigs.kustomize
        # Its wrapper's go would download the toolchain go.mod asks for.
        (kubebuilder.override { inherit (operator) go; })
        kubernetes-controller-tools
        kubernetes-helm
        mockgen
      ];

      buildPhase = ''
        runHook preBuild
        export HOME="$TMPDIR"

        # The sandbox has no /usr/bin/env.
        patchShebangs hack
        substituteInPlace Makefile --replace-fail "/usr/bin/env bash" ${bash}/bin/bash

        # controller-gen loads each vendored module's go.mod as a root.
        chmod -R u+w vendor
        find vendor -mindepth 2 \( -name go.mod -o -name go.work \) -delete

        ${script}
        runHook postBuild
      '';

      doCheck = false;
      installPhase = "touch $out";
    });
in
{
  lint = check "lint" ''
    make lint-config lint helm-lint
    kustomize build config/release > /dev/null

    # dist/chart is `make helm` output, committed.
    cp -r . "$TMPDIR/before"
    make helm
    for path in dist/chart PROJECT Makefile; do
      diff -ru "$TMPDIR/before/$path" "$path"
    done
  '';

  test = check "test" ''
    make test
  '';
}
