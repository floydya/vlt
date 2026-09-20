# vlt

`vlt` is a cross-platform profile manager and transparent launcher for the official [HashiCorp Vault CLI](https://developer.hashicorp.com/vault/docs/commands).

It lets developers keep metadata for multiple Vault hosts, store tokens only in the operating system's native credential store, authenticate through OIDC, and delegate Vault commands without changing the parent shell.

The approved initial-release requirements are in [`SPEC.md`](SPEC.md), and work is tracked with [Beads](https://github.com/steveyegge/beads).

## Install with Nix

Add `vlt` to your flake inputs:

```nix
inputs.vlt.url = "github:floydya/vlt";
```

Add the NixOS module to your host's module list:

```nix
outputs = { nixpkgs, vlt, ... }: {
  nixosConfigurations.my-host = nixpkgs.lib.nixosSystem {
    system = "x86_64-linux";
    modules = [
      vlt.nixosModules.default
      ./configuration.nix
    ];
  };
};
```

Then enable `vlt` in `configuration.nix`:

```nix
programs.vlt.enable = true;
```

Home Manager users can add `vlt.homeManagerModules.default` to their `modules` list and use the same option:

```nix
programs.vlt.enable = true;
```

You can also install the default package directly:

```console
nix profile install github:floydya/vlt
```

Install the official `vault` CLI separately and keep it on `PATH`. `vlt` delegates Vault operations to that executable.

## Intended usage

```console
$ vlt profile add team-a \
    --address https://vault.example.com \
    --username example-user \
    --auth-path oidc

$ vlt switch team-a
$ vlt read secret/example
$ vlt --profile team-b read secret/example
```

`vlt` will not reimplement Vault data operations. It will resolve a profile, manage its credential lifecycle, and execute the installed `vault` binary with the original arguments and attached streams.

Vault profiles use HTTPS by default. To connect to a trusted local test Vault over HTTP, add the profile with `--allow-insecure`. Clear the opt-in with `vlt profile update NAME --address https://... --allow-insecure=false`.

## Security model

- Tokens belong only in Secret Service, Keychain, or Credential Manager.
- Tokens must never enter profile configuration, process arguments, logs, or terminal output.
- Vault subprocesses use argument vectors rather than shell interpolation.
- There is no plaintext or file-based credential fallback.
- HTTP Vault addresses require an explicit per-profile `--allow-insecure` opt-in.
- Delegated operations are never automatically retried.

Do not use real credentials in tests, fixtures, bug reports, or commits.

## Development

Prerequisites:

- Go 1.26
- [`just`](https://just.systems/) for task shortcuts
- the official `vault` CLI for manual integration checks only
- `bd` for issue tracking

```console
just fmt          # apply gofmt
just test         # run fake-backed tests
just check        # formatting, tests, race detector, vet, and build
just build-all    # compile Linux, macOS, and Windows targets
```

See [`CONTRIBUTING.md`](CONTRIBUTING.md) for the development workflow. CI, non-Nix installers, auto-update, and telemetry are intentionally outside the initial scope.
