# Contributing

Start with [`SPEC.md`](SPEC.md). It defines the approved behavior, security boundaries, architecture, and release criteria for the initial `vlt` release.

## Workflow

1. Run `bd prime` for repository-specific issue guidance.
2. Find unblocked work with `bd ready`.
3. Inspect and claim one issue with `bd show <id>` and `bd update <id> --claim`.
4. Keep the change limited to that issue and add or update tests for behavior changes.
5. Run the relevant focused checks, then `just check` before completion.
6. Review the diff for credentials and unrelated changes.
7. Close completed work with `bd close <id>`.

Use Beads rather than markdown task lists. Record durable project insights with `bd remember "insight"`.

## Engineering rules

- Use idiomatic Go and `gofmt`.
- Keep package boundaries aligned with `SPEC.md`.
- Inject filesystem, clock, keyring, and process boundaries; avoid mutable globals.
- Add no dependency beyond the approved keyring package without prior approval.
- Never include live Vault credentials in source, fixtures, output, errors, or command arguments.
- Preserve opaque Vault arguments, attached streams, and delegated exit behavior.
- Do not weaken, delete, or skip security tests to make checks pass.

## Verification

```console
just fmt-check
just test
just test-race
just vet
just build
just build-all
```

Fake-backed automated tests must not require a live Vault server or desktop keyring. Real OIDC and native-keyring checks are manual, use non-production credentials, and must not record sensitive host data.

## Commits

Use a conventional commit with the Beads issue as its scope, such as `feat(VLT-123): ...` or `fix(VLT-123): ...`. Add `!` or a `BREAKING CHANGE:` footer for an incompatible change. Use `fix(perf): ...` for a performance fix. Maintenance-only `build`, `chore`, `ci`, `docs`, `refactor`, `style`, and `test` commits do not independently trigger a release.

Keep commits small and atomic. Do not mix formatting-only changes, refactors, and behavior changes without a clear reason. See [`docs/releasing.md`](docs/releasing.md) for the automated release flow.
