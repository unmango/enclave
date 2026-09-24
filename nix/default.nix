{
  buildGoApplication,
  envtest-assets,
  lib,
  version,
}:
buildGoApplication {
  pname = "";
  inherit version;

  src = lib.cleanSource ../.;
  modules = ./gomod2nix.toml;

  env.KUBEBUILDER_ASSETS = "${envtest-assets}";

  # The only package is cmd/, so the binary lands at bin/cmd. config/manager
  # invokes it as /bin/manager.
  postInstall = ''
    mv $out/bin/cmd $out/bin/manager
  '';

  # envtest starts a real kube-apiserver, which picks its advertise address by
  # looking for a default route. The build sandbox has only loopback and no
  # route, so the apiserver cannot start. CI runs the suites through the dev
  # shell instead, where KUBEBUILDER_ASSETS points at these same binaries.
  doCheck = false;
}
