{
  cacert,
  dockerTools,
  operator,
}:
dockerTools.streamLayeredImage {
  name = "enclave";
  tag = "latest";

  contents = [
    cacert
    dockerTools.fakeNss
    operator
  ];

  config = {
    Entrypoint = [ "/bin/manager" ];
    User = "65532:65532";
    # Links the ghcr.io package to the repository.
    Labels."org.opencontainers.image.source" = "https://github.com/unmango/enclave";
  };
}
