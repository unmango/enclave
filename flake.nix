{
  description = "A Kubernetes operator";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs?ref=nixos-unstable";
    systems.url = "github:UnstoppableMango/nix-systems";

    flake-parts = {
      url = "github:hercules-ci/flake-parts";
      inputs.nixpkgs-lib.follows = "nixpkgs";
    };

    treefmt-nix = {
      url = "github:numtide/treefmt-nix";
      inputs.nixpkgs.follows = "nixpkgs";
    };

    gomod2nix = {
      url = "github:nix-community/gomod2nix";
      inputs.nixpkgs.follows = "nixpkgs";
      inputs.flake-utils.inputs.systems.follows = "systems";
    };

    kubepkgs = {
      url = "github:unmango/kubepkgs";
      inputs.nixpkgs.follows = "nixpkgs";
      inputs.systems.follows = "systems";
    };
  };

  outputs =
    inputs@{ flake-parts, ... }:
    flake-parts.lib.mkFlake { inherit inputs; } {
      systems = import inputs.systems;

      imports = with inputs; [
        systems.flakeModule
        treefmt-nix.flakeModule
      ];

      perSystem =
        {
          inputs',
          lib,
          pkgs,
          system,
          ...
        }:
        let
          version = "0.0.1";
          k8sVersion = "1.37";

          k8s = inputs'.kubepkgs.legacyPackages.kubernetes.${k8sVersion};
          envtest-assets = pkgs.callPackage ./nix/envtest.nix { inherit k8s; };
          operator = pkgs.callPackage ./nix { inherit envtest-assets version; };
        in
        {
          _module.args.pkgs = import inputs.nixpkgs {
            inherit system;
            overlays = with inputs; [
              gomod2nix.overlays.default
            ];
          };

          packages = {
            default = operator;
            image = pkgs.callPackage ./nix/image.nix { inherit operator; };
            inherit envtest-assets;
          };

          # `nix flake check` evaluates `packages` without building them, so the
          # operator is exposed as a check to make CI compile it.
          checks.operator = operator;

          devShells.default = pkgs.mkShellNoCC {
            packages =
              (with pkgs; [
                ginkgo
                gnumake
                go
                golangci-lint
                gomod2nix
                gopls
                kubebuilder
                kubernetes-controller-tools
                kubernetes-helm
                mockgen
                nixfmt
                setup-envtest
                skopeo
              ])
              ++ [
                k8s.kubectl
                k8s.sigs.kind
                k8s.sigs.kustomize
              ];

            KUBEBUILDER_ASSETS = "${envtest-assets}";
            CONTROLLER_GEN = lib.getExe' pkgs.kubernetes-controller-tools "controller-gen";
            ENVTEST = lib.getExe pkgs.setup-envtest;
            HELM = lib.getExe pkgs.kubernetes-helm;
            KIND = lib.getExe k8s.sigs.kind;
            KUBECTL = lib.getExe k8s.kubectl;
            KUSTOMIZE = lib.getExe k8s.sigs.kustomize;
          };

          treefmt.programs = {
            actionlint.enable = true;
            gofmt.enable = true;
            nixfmt.enable = true;
            shfmt.enable = true;
            zizmor.enable = true;
          };
        };
    };
}
