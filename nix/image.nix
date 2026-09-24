{
  cacert,
  dockerTools,
  operator,
}:
dockerTools.streamLayeredImage {
  name = "my-operator";
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
