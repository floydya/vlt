{
  config,
  lib,
  pkgs,
  ...
}:

let
  cfg = config.programs.vlt;
in
{
  options.programs.vlt = {
    enable = lib.mkEnableOption "vlt";

    package = lib.mkOption {
      type = lib.types.package;
      default = pkgs.callPackage ./package.nix { };
      defaultText = lib.literalExpression "vlt.packages.${pkgs.stdenv.hostPlatform.system}.default";
      description = "The vlt package to install.";
    };
  };

  config = lib.mkIf cfg.enable {
    environment.systemPackages = [ cfg.package ];
  };
}
