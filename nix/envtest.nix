# Binaries controller-runtime's envtest expects to find in KUBEBUILDER_ASSETS.
# Sourcing them from kubepkgs replaces `setup-envtest`, which downloads them at
# test time and cannot run inside the nix build sandbox.
{
  k8s,
  lib,
  linkFarm,
}:
linkFarm "envtest-assets" {
  "etcd" = lib.getExe k8s.deps.etcd;
  "kube-apiserver" = lib.getExe k8s.kube-apiserver;
  "kubectl" = lib.getExe k8s.kubectl;
}
