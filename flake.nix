{
  description = "A profile manager and transparent launcher for the HashiCorp Vault CLI";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      supportedSystems = [
        "aarch64-darwin"
        "aarch64-linux"
        "x86_64-linux"
      ];
      forAllSystems = nixpkgs.lib.genAttrs supportedSystems;
    in
    {
      packages = forAllSystems (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
          vlt = pkgs.callPackage ./nix/package.nix { };
        in
        {
          default = vlt;
          inherit vlt;
        }
      );

      checks = forAllSystems (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
          inherit (self.packages.${system}) vlt;
          moduleOptions = {
            options = {
              environment.systemPackages = nixpkgs.lib.mkOption {
                type = nixpkgs.lib.types.listOf nixpkgs.lib.types.package;
                default = [ ];
              };
              home.packages = nixpkgs.lib.mkOption {
                type = nixpkgs.lib.types.listOf nixpkgs.lib.types.package;
                default = [ ];
              };
            };
          };
          evaluateModule =
            module: config:
            nixpkgs.lib.evalModules {
              specialArgs = { inherit pkgs; };
              modules = [
                moduleOptions
                module
                config
              ];
            };
          nixosConfig = evaluateModule self.nixosModules.default { programs.vlt.enable = true; };
          nixosDisabledConfig = evaluateModule self.nixosModules.default { };
          nixosOverrideConfig = evaluateModule self.nixosModules.default {
            programs.vlt = {
              enable = true;
              package = pkgs.hello;
            };
          };
          homeManagerConfig = evaluateModule self.homeManagerModules.default { programs.vlt.enable = true; };
          homeManagerDisabledConfig = evaluateModule self.homeManagerModules.default { };
          homeManagerOverrideConfig = evaluateModule self.homeManagerModules.default {
            programs.vlt = {
              enable = true;
              package = pkgs.hello;
            };
          };
          modulesWork =
            nixosConfig.config.environment.systemPackages == [ vlt ]
            && nixosDisabledConfig.config.environment.systemPackages == [ ]
            && nixosOverrideConfig.config.environment.systemPackages == [ pkgs.hello ]
            && homeManagerConfig.config.home.packages == [ vlt ]
            && homeManagerDisabledConfig.config.home.packages == [ ]
            && homeManagerOverrideConfig.config.home.packages == [ pkgs.hello ];
        in
        {
          package = vlt;
          modules =
            assert modulesWork;
            pkgs.runCommand "vlt-module-check" { } "touch $out";
        }
      );

      formatter = forAllSystems (system: nixpkgs.legacyPackages.${system}.nixfmt-tree);

      nixosModules = {
        default = self.nixosModules.vlt;
        vlt = import ./nix/nixos-module.nix;
      };

      homeManagerModules = {
        default = self.homeManagerModules.vlt;
        vlt = import ./nix/home-manager-module.nix;
      };
    };
}
