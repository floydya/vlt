{
  buildGoModule,
  lib,
}:

buildGoModule {
  pname = "vlt";
  version = "unstable";

  src = lib.cleanSource ../.;
  vendorHash = "sha256-t973Wsc1ncEqkm1kswTJSl4Ku8yjokuXbULxjCNR6Uw=";

  subPackages = [ "cmd/vlt" ];

  meta = {
    description = "Profile manager and transparent launcher for the HashiCorp Vault CLI";
    mainProgram = "vlt";
    platforms = lib.platforms.unix;
  };
}
