# Spec: `vlt` Vault Profile CLI

**Status:** Approved  
**Scope:** Initial cross-platform release  
**Primary verification platform:** Linux

## Objective

Build `vlt`, a cross-platform command-line profile manager and transparent launcher for the official HashiCorp Vault CLI. It lets a user switch frequently between Vault hosts without repeatedly completing OIDC authentication or manually changing `VAULT_ADDR` and `VAULT_TOKEN`.

The initial user is a developer who uses multiple independent Vault hosts with the same or different usernames and OIDC settings.

A typical workflow is:

```console
$ vlt profile add team-a \
    --address https://vault.example.com \
    --username example-user \
    --auth-path oidc
# Browser-based OIDC login opens once.

$ vlt switch team-a
Switched to profile "team-a".

$ vlt read secret/example
# Equivalent Vault operation, using team-a's address and stored token.

$ vlt --profile team-b read secret/example
# Uses team-b for this command only; team-a remains active.
```

`vlt` does not reimplement Vault operations. It performs profile and credential lifecycle work, then launches the installed `vault` executable with the requested arguments.

## Capability Map

| Module ID | Responsibility | Depends on |
|---|---|---|
| `profile-management` | Create, list, inspect, update, remove, and select profile metadata | — |
| `credential-lifecycle` | OIDC login, native keyring storage, token validation, and renewal | `profile-management` |
| `vault-delegation` | Resolve a profile, inject Vault environment variables, and execute the official `vault` binary | `profile-management`, `credential-lifecycle` |

Build order: `profile-management` → `credential-lifecycle` → `vault-delegation`.

The requirement headings below identify the owning module so implementation and tests remain traceable to this map.

## Functional Requirements

### `profile-management`

#### Profile model

Each profile has:

- `name`: required, unique, stable identifier used by scripts;
- `address`: required Vault server URL;
- `username`: required value supplied to OIDC login;
- `auth_path`: required OIDC mount path, defaulting to `oidc` during creation;
- `namespace`: optional Vault Enterprise namespace.

Profile names must contain only ASCII letters, digits, hyphens, and underscores, must start with a letter or digit, must not consist entirely of digits, and are case-sensitive. Numeric-only names are invalid because they conflict with numbered profile selection. Secret values, including Vault tokens, must never be stored in profile configuration.

#### Profile commands

```text
vlt profile add NAME --address URL --username USER [--auth-path PATH] [--namespace NAMESPACE]
vlt profile list
vlt profile show NAME
vlt profile update NAME [--address URL] [--username USER] [--auth-path PATH] [--namespace NAMESPACE]
vlt profile remove NAME
```

Behavior:

- `profile add` validates and persists metadata, then immediately performs OIDC authentication. If authentication fails, the incomplete profile and any token are removed.
- Adding a duplicate name fails without modifying the existing profile.
- `profile update` changes only supplied fields. If address, username, auth path, or namespace changes, the existing token is deleted and OIDC authentication runs immediately. Failed reauthentication leaves the original profile and token intact.
- `profile remove` deletes both metadata and its keyring token. Removing the active profile leaves no active profile; another profile is never selected implicitly.
- `profile list` sorts names lexicographically and uses one-based display numbers.
- `profile show` and `profile list` never print token values or keyring identifiers.

#### Active profile commands

```text
vlt switch
vlt switch NAME
vlt switch NUMBER
```

Behavior:

- With no argument, `switch` prints the active profile and the same deterministic, numbered profile list used by `profile list`.
- A name is the stable, script-safe selector.
- A number is an interactive convenience referring to the currently displayed lexicographic order.
- Selecting a profile persists it as the default for later `vlt` invocations.
- Selecting an unknown name or out-of-range number fails without changing the current selection.

### `credential-lifecycle`

#### Credential storage

- Tokens are stored in the operating system's native credential store: Secret Service-compatible keyring on Linux, Keychain on macOS, and Credential Manager on Windows.
- The credential-store entry is namespaced to `vlt` and keyed by profile identity.
- There is no plaintext, environment-file, or config-file fallback.
- An unavailable or locked credential store produces an actionable error and does not trigger insecure storage.
- Replacing or deleting a profile token also replaces or deletes the corresponding keyring entry.

#### OIDC login

Authentication is delegated to the official CLI using behavior equivalent to:

```console
vault login -no-store -format=json -method=oidc -path=AUTH_PATH username=USERNAME
```

The profile address, optional namespace, and any required profile-owned Vault environment variables are provided only to that subprocess. `-no-store` prevents the official CLI from changing its own global token-helper state.

`vlt` must:

1. permit the official CLI to open the browser and complete OIDC;
2. capture and parse the JSON response;
3. extract the client token without displaying or logging it;
4. persist the token directly to the native credential store;
5. discard captured secret material as soon as practical.

Malformed output, a missing client token, cancellation, or non-zero login exit is an authentication failure. Diagnostic output may be forwarded only after token-like values are redacted.

#### Validation and renewal

Before forwarding each Vault operation, `vlt` performs a credential preflight against the selected profile:

1. Load its token from the credential store.
2. If absent, perform OIDC login.
3. Validate the token through the official Vault CLI.
4. If valid, renewable, and within five minutes of expiry, request self-renewal.
5. If Vault identifies it as expired or invalid, perform OIDC login before running the requested operation.
6. If validation fails for another reason, such as network or TLS failure, return the error instead of opening an unnecessary browser login.

If renewal fails while the existing token remains valid, continue with the existing token and emit a concise warning. If no usable token remains, authenticate again before delegation.

Credential preflight occurs before the requested operation. If the delegated operation itself later fails due to authentication, `vlt` returns that failure and does not automatically retry, because the operation may have produced side effects.

### `vault-delegation`

#### Invocation

```text
vlt [--profile NAME] VAULT_ARGUMENT...
```

Examples:

```console
vlt read secret/example
vlt kv get secret/example
vlt write secret/example value=test
vlt --profile team-a read secret/example
```

Behavior:

- `--profile NAME` selects a profile for one invocation and never changes the persisted active profile.
- Without `--profile`, the active profile is used.
- If neither exists, the command fails with instructions to add or switch to a profile.
- All remaining arguments are passed to `vault` unchanged and in the original order.
- Standard input, standard output, and standard error remain connected to the delegated process so interactive Vault operations continue to work.
- `vlt` starts from the caller's environment, overriding `VAULT_ADDR`, `VAULT_TOKEN`, and `VAULT_NAMESPACE` with profile-owned values. If a profile has no namespace, inherited `VAULT_NAMESPACE` is removed to prevent cross-profile leakage.
- Profile selection does not mutate the parent shell and does not affect direct `vault ...` commands.
- `vlt` never writes a selected token to the process-wide parent environment, command arguments, config files, logs, or terminal output.
- The delegated process's exit code is preserved. On platforms supporting process signals, signal termination is reflected using platform-appropriate conventions.

`vlt` reserves `switch` and `profile` as management commands. Every other command name is treated as a Vault command and forwarded after preflight.

#### Dependency discovery

- The `vault` executable must be discoverable through `PATH`.
- If it is absent, `vlt` fails before authentication with an actionable installation/configuration message.
- The initial release does not download, embed, or version-manage Vault.

## Non-Functional Requirements

### Security

- Tokens must be handled as secrets at every boundary.
- Error messages and debug output must be redacted before display.
- Config writes must be atomic to avoid truncation or partial profile state.
- On Unix-like systems, newly created config directories and files use user-only permissions (`0700` directories and `0600` files). On Windows, files rely on the current user's standard profile-directory ACLs.
- Subprocess arguments must be passed as an argument vector, never through shell interpolation.
- URLs and profile inputs must be validated before persistence or subprocess execution.
- Automated tests must prove that normal and failing paths do not print tokens.

### Portability

- Linux, macOS, and Windows are supported design targets.
- Automated tests must run without a live Vault server or desktop keyring by using replaceable process and credential-store boundaries.
- The initial release is manually integration-tested only on Linux.
- Platform-specific behavior must be isolated behind small interfaces and covered by platform-independent contract tests where practical.

### Reliability and usability

- Management failures must not leave partially written config or unintentionally delete a previously valid profile/token.
- Errors identify the failing operation and suggest a corrective action where one is known.
- Routine management output is concise and stable enough for human use; no machine-readable output contract is promised in the initial release.
- The normal success path adds minimal output before handing control to Vault.

## Tech Stack

- **Language:** Go, using the stable version pinned in `go.mod` when implementation begins.
- **CLI parsing:** Go standard library plus a small explicit dispatcher; delegated arguments remain opaque to `vlt`.
- **Configuration:** JSON via Go's standard library, stored under `os.UserConfigDir()` in a `vlt` directory.
- **Credential storage:** A pinned cross-platform Go keyring dependency supporting Linux Secret Service, macOS Keychain, and Windows Credential Manager.
- **Vault integration:** The installed official `vault` executable invoked as a subprocess.
- **Testing:** Go's `testing` package with fakes for subprocesses, clocks, config storage, and keyring access.

Dependency versions must be pinned in `go.mod`/`go.sum`. Adding dependencies beyond the keyring integration requires approval.

## Commands

These commands become valid once the Go module and source tree exist:

```console
# Build
go build ./cmd/vlt

# Run during development
go run ./cmd/vlt --help

# Unit and integration tests (using fakes; no live Vault required)
go test ./...

# Race detection
go test -race ./...

# Static analysis
go vet ./...

# Verify formatting without changing files
test -z "$(gofmt -l .)"

# Apply formatting
gofmt -w .
```

Cross-platform compilation checks should include:

```console
GOOS=linux GOARCH=amd64 go build ./cmd/vlt
GOOS=darwin GOARCH=amd64 go build ./cmd/vlt
GOOS=windows GOARCH=amd64 go build ./cmd/vlt
```

These compilation checks verify code portability, not runtime keyring behavior.

## Project Structure

```text
cmd/vlt/
  main.go                 Application entry point and dependency wiring
internal/cli/
  ...                     Argument dispatch, command behavior, user-facing output
internal/profile/
  ...                     Profile model, validation, selection, and operations
internal/config/
  ...                     Atomic platform-aware configuration persistence
internal/credential/
  ...                     Keyring contract, token preflight, login, and renewal
internal/vaultexec/
  ...                     Vault executable discovery and subprocess execution
```

Go tests live beside the package they test as `*_test.go`. Test fixtures containing fake Vault output live under the owning package's `testdata/` directory. No fixture may contain a real host token or other live credential.

## Code Style

Use idiomatic Go, `gofmt`, explicit error wrapping, dependency injection at external boundaries, and small interfaces defined by the consuming package.

```go
type TokenStore interface {
	Get(ctx context.Context, profileName string) (string, error)
	Set(ctx context.Context, profileName, token string) error
	Delete(ctx context.Context, profileName string) error
}

func (s *Service) Select(ctx context.Context, name string) error {
	if _, err := s.profiles.Find(ctx, name); err != nil {
		return fmt.Errorf("select profile %q: %w", name, err)
	}
	if err := s.config.SetActiveProfile(ctx, name); err != nil {
		return fmt.Errorf("persist active profile %q: %w", name, err)
	}
	return nil
}
```

Conventions:

- Package names are short, lowercase nouns.
- Export only symbols needed across package boundaries.
- Sentinel or typed errors are used only when callers need behavioral branching.
- Errors add operation context without including tokens.
- Avoid global mutable state; time, keyring, filesystem, and process execution are injectable.
- Do not introduce abstractions without at least two meaningful implementations or a test seam at an external boundary.

## Testing Strategy

### Unit tests

Use table-driven tests for:

- profile-name, URL, username, auth-path, and namespace validation;
- deterministic sorting and numeric profile resolution;
- active-profile selection and one-command override precedence;
- Vault environment construction and removal of leaked namespace values;
- token state decisions: absent, valid, near expiry, renewable, expired, and lookup failure;
- command parsing and exact argument forwarding;
- error redaction.

### Component tests

Use temporary directories, a fake keyring, a controllable clock, and a fake `vault` executable/process runner to verify complete flows:

- add → OIDC → token stored;
- switch → delegated command uses active profile;
- `--profile` overrides without changing active profile;
- renewal occurs before delegation near expiry;
- invalid token triggers login before delegation;
- network validation failure does not trigger login;
- delegated authentication failure is not retried;
- failed add/update leaves no partial or regressed state;
- remove deletes metadata and token;
- delegated exit status and standard streams are preserved;
- tokens never appear in output or persisted config.

### Platform checks

- All unit/component tests must pass on supported CI operating systems when CI is introduced.
- Cross-compilation must succeed for Linux, macOS, and Windows.
- Before the initial release, manually verify Linux Secret Service integration and a real OIDC login/delegated read on this development machine.
- Real macOS and Windows keyring/OIDC verification is deferred until those environments are available and must be documented as unverified meanwhile.

No numeric coverage target is imposed initially. Every success criterion and security-sensitive failure path requires a test; deleting or skipping such tests requires approval.

## Boundaries

### Always do

- Use the official `vault` executable for login, token operations, and Vault commands.
- Keep token storage exclusively in the native credential store.
- Use argument-vector subprocess execution without a shell.
- Redact tokens and likely credential values from all diagnostics.
- Preserve delegated arguments, streams, and exit behavior.
- Validate inputs and use atomic config updates.
- Add or update tests for every behavior change.
- Run formatting, tests, race tests, static analysis, and cross-platform compilation before release.

### Ask first

- Add any dependency beyond the cross-platform keyring package.
- Change the profile schema or configuration location after release.
- Add plaintext, encrypted-file, or environment-file credential fallback.
- Add machine-readable output or promise output stability for scripts.
- Introduce shell integration that mutates parent-shell variables.
- Add automatic retry of delegated Vault operations.
- Add support for authentication methods other than OIDC.
- Change reserved management command names.
- Add CI, packaging, installers, auto-update, or telemetry.

### Never do

- Commit, persist, log, print, or place a real Vault token in process arguments.
- Store tokens in the profile JSON or fall back silently when the keyring is unavailable.
- Invoke Vault commands through shell string interpolation.
- Reimplement Vault read/write/kv behavior.
- Mutate the official Vault CLI's token-helper state during `vlt` login.
- Change direct `vault ...` behavior or the parent shell's environment.
- Retry a delegated command after it may have executed.
- Remove or skip a failing security test merely to make checks pass.

## Success Criteria

The initial release is complete when all of the following are demonstrably true:

1. A user can add `team-a` and `team-b` profiles with address, username, and `oidc` auth path; each successful add completes OIDC and stores its token only in the native keyring.
2. `vlt switch` displays the current profile and a deterministic numbered list; switching by name and number persists the same selected profile.
3. After `vlt switch team-a`, `vlt read ...` delegates unchanged arguments to Vault with team-a's address and token.
4. `vlt --profile team-b read ...` uses team-b once and leaves team-a active.
5. A valid token avoids browser authentication; a renewable token within five minutes of expiry is renewed; an invalid or expired token authenticates before delegation.
6. Network/TLS validation failures do not incorrectly trigger OIDC, and a delegated authentication failure is returned without retrying the operation.
7. Direct `vault ...` behavior and the parent shell environment remain unchanged.
8. Profile metadata survives process restarts, while no token appears in the config file, terminal output, logs, or command arguments.
9. Missing Vault, unavailable keyring, malformed login output, invalid profiles, and partial-update failures produce actionable errors without corrupting existing state.
10. Automated tests cover the functional and security flows above and pass under `go test ./...`, `go test -race ./...`, and `go vet ./...`.
11. The program cross-compiles for Linux, macOS, and Windows, and the complete real login/read flow is manually verified on Linux.

## Out of Scope

- Reimplementing any Vault data command or embedding a Vault server/client replacement.
- Changing the behavior of direct `vault ...` commands.
- Parent-shell environment mutation or shell-specific activation scripts.
- Authentication methods other than OIDC.
- Multiple users, shared/team configuration, or profile synchronization between machines.
- Plaintext or file-based token fallback.
- Automatic retries of delegated Vault operations.
- Bundling or downloading the Vault executable.
- Live-Vault automated tests in the initial implementation.
- Initial runtime certification on macOS or Windows.
- Installers, package-manager publication, automatic updates, telemetry, and CI configuration.
- A stable machine-readable output API.

## Open Questions

None currently. Any new requirement changes this specification before implementation proceeds.
