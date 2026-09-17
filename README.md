# vlt

`vlt` is a planned cross-platform profile manager and transparent launcher for the official [HashiCorp Vault CLI](https://developer.hashicorp.com/vault/docs/commands).

It will let developers keep metadata for multiple Vault hosts, store tokens only in the operating system's native credential store, authenticate through OIDC, and delegate Vault commands without changing the parent shell.

> **Status:** pre-implementation. The approved initial-release requirements are in [`SPEC.md`](SPEC.md), and work is tracked with [Beads](https://github.com/steveyegge/beads).

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

## Security model

- Tokens belong only in Secret Service, Keychain, or Credential Manager.
- Tokens must never enter profile configuration, process arguments, logs, or terminal output.
- Vault subprocesses use argument vectors rather than shell interpolation.
- There is no plaintext or file-based credential fallback.
- Delegated operations are never automatically retried.

Do not use real credentials in tests, fixtures, bug reports, or commits.

## Development

Prerequisites:

- Go (the repository will pin its stable version when the module is initialized)
- [`just`](https://just.systems/) for task shortcuts
- the official `vault` CLI for manual integration checks only
- `bd` for issue tracking

Once the Go module and source tree exist:

```console
just fmt          # apply gofmt
just test         # run fake-backed tests
just check        # formatting, tests, race detector, vet, and build
just build-all    # compile Linux, macOS, and Windows targets
```

See [`CONTRIBUTING.md`](CONTRIBUTING.md) for the development workflow. CI, packaging, installers, auto-update, and telemetry are intentionally outside the initial scope.
