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
  };
}
