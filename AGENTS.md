# vlt project guidance

## Project

`vlt` is a cross-platform profile manager and transparent launcher for the official HashiCorp Vault CLI.

- Keep profile and favorite metadata local.
- Store tokens only in the operating system's native credential store.
- Keep management commands inside `vlt`.
- Delegate every other command to the installed `vault` executable without changing its arguments, streams, or exit status.

## Architecture

- Keep dependency wiring in `cmd/vlt`.
- Keep command parsing, help, prompts, completion, and presentation in `internal/cli`.
- Keep profile, favorite, configuration, credential, secure-file, locking, and Vault execution concerns in their existing packages.
- Inject filesystem, clock, keyring, terminal, and process boundaries. Avoid mutable global state.
- Follow [`SPEC.md`](SPEC.md) for detailed behavior and package boundaries.

## Security boundaries

- Never store, print, log, or place real Vault tokens in command arguments.
- Use argument-vector subprocess execution. Never interpolate Vault commands through a shell.
- Keep HTTPS as the default. Require the persisted per-profile opt-in before using HTTP.
- Redact credentials and token-shaped values from diagnostics.
- Preserve trusted-file checks, atomic writes, rollback behavior, and the shared mutation lock.
- Do not use real credentials in tests, fixtures, examples, issues, or commits.

## Development

- Use Go 1.26 and the repository's `just` recipes.
- Add or update tests for behavior changes. Cover security-sensitive failures and regressions.
- Run focused tests while developing, then run `just check` before completion.
- Run `just build-all` when a change can affect platform-specific behavior or release readiness.
- Treat native keyring, real OIDC, and shell integration checks as separate manual verification.
- Update documentation when behavior, setup, commands, configuration, or public interfaces change.

## Change discipline

- Write commit subjects as `type(scope): concise imperative summary`. Use a Conventional Commits type and a meaningful lowercase scope, for example `feat(storage): store tokens in the native keyring`.
- Make the smallest safe change that fully addresses the request.
- Preserve public behavior and unrelated work unless the request requires a change.
- Do not add or replace dependencies without approval.
- Do not weaken, delete, or skip tests to make a check pass.
- Report which checks ran and which runtime or platform checks remain unverified.
