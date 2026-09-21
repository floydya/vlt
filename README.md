# vlt

`vlt` is a cross-platform profile manager and transparent launcher for the official [HashiCorp Vault CLI](https://developer.hashicorp.com/vault/docs/commands).

It lets you keep metadata for multiple Vault hosts, store tokens only in your operating system's native credential store, authenticate through OIDC, and delegate Vault commands without changing your current shell.

## Table of contents

- [Installation](#installation)
  - [Homebrew on macOS](#homebrew-on-macos)
  - [Nix](#nix)
- [Usage](#usage)
- [Security model](#security-model)
- [Development](#development)

## Installation

`vlt` requires the official `vault` CLI on `PATH`. It delegates Vault operations to that executable.

### Homebrew on macOS

Install the official Vault CLI and `vlt` from their dedicated taps:

```console
brew tap hashicorp/tap
brew install hashicorp/tap/vault
brew install floydya/tap/vlt
```

The `vlt` formula builds the tagged source release locally and installs Bash, Zsh, and Fish completions.

### Nix

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

## Usage

```console
$ vlt profile add team-a \
    --address https://vault.example.com \
    --username example-user \
    --auth-path oidc \
    --color '#3366CC'

$ vlt switch team-a
$ vlt profile current
$ vlt read secret/example
$ vlt --profile team-b read secret/example

$ vlt favorite add secret/data/example --profile team-a --operation kv-get --note daily
$ vlt favorite
$ vlt favorite list
$ vlt favorite update f_0123456789abcdef --note reviewed
```

`vlt` will not reimplement Vault data operations. It will resolve a profile, manage its credential lifecycle, and execute the installed `vault` binary with the original arguments and attached streams.

Bash and Zsh completion suggest command names from the installed Vault CLI beside `vlt` commands. They also follow Vault for nested commands, flags, and supported local values. This works with or without `--profile NAME` and does not require Vault's separate shell completion hook.

Vault profiles use HTTPS by default. To connect to a trusted local test Vault over HTTP, add the profile with `--allow-insecure`. Clear the opt-in with `vlt profile update NAME --address https://... --allow-insecure=false`.

Set a profile color with `--color '#3366CC'` on `profile add` or `profile update`. It colors that profile's name and active marker. Favorite rows linked to the profile use its color. Shared headings, labels, forms, and status text keep their usual colors. Clear it with `vlt profile update NAME --color=`. A color-only update keeps the stored token. `NO_COLOR` and redirected output remain plain.

`vlt favorite list` shows each favorite's successful run count. Favorites with more runs appear first, with path, profile, and operation breaking ties. After Vault succeeds, `vlt` records one run when it can save local metadata. A failed count save does not change Vault's output or exit status. Changing a favorite's profile, operation, or path resets the count; changing its note keeps it.

Each favorite has a stable ID shown by `vlt favorite list`. Use the ID with `favorite update` or `favorite remove` to target it after edits or list reordering. Current list numbers still work. Guided `favorite add` starts with the active profile selected. Narrow terminals wrap list entries, and pickers show details for the selected item. Redirected lists keep their full tables.

### Upgrading from v0.2.x

Profile metadata needs no migration. The new version reads existing favorites without changing their file. The first favorite write saves version 2 metadata with stable IDs. v0.2.x cannot read that new file, so keep a private copy of `favorites.json` before using the new version if you may need to roll back. Restoring that copy also discards favorite changes made after the copy. Vault tokens remain in the native credential store and are not part of this file.

## Security model

- Tokens belong only in Secret Service, Keychain, or Credential Manager.
- Tokens must never enter profile configuration, process arguments, logs, or terminal output.
- Vault subprocesses use argument vectors rather than shell interpolation.
- There is no plaintext or file-based credential fallback.
- HTTP Vault addresses require an explicit per-profile `--allow-insecure` opt-in.
- On Unix, profile and favorite metadata files require a user-owned private `vlt` directory and private regular files. Windows relies on the current user's profile-directory ACLs. Both platforms reject symbolic links and non-regular metadata files.
- State-changing commands share one application lock. Concurrent reads and delegated Vault commands remain unlocked.
- Delegated operations are never automatically retried.

Do not use real credentials in tests, fixtures, bug reports, or commits.

## Development

Prerequisites:

- Go 1.26
- [`just`](https://just.systems/) for task shortcuts
- the official `vault` CLI for manual integration checks only

```console
just fmt          # apply gofmt
just test         # run fake-backed tests
just check        # formatting, tests, race detector, vet, and build
just build-all    # compile Linux, macOS, and Windows targets
```

See [`CONTRIBUTING.md`](CONTRIBUTING.md) for the development workflow, [`SPEC.md`](SPEC.md) for the design and behavior, and [`docs/releasing.md`](docs/releasing.md) for release maintenance. Automatic updates and telemetry remain outside the initial scope.
