# Spec: `vlt` Vault Profile CLI

**Status:** Approved

**Automatic-help revision:** Approved

**Scope:** Initial cross-platform release with guided terminal workflows

**Primary verification platform:** Linux

## Objective

Build `vlt`, a cross-platform command-line profile manager and transparent launcher for the official HashiCorp Vault CLI. It lets a user switch frequently between Vault hosts without repeatedly completing OIDC authentication or manually changing `VAULT_ADDR` and `VAULT_TOKEN`. Its own commands must be self-explanatory during interactive terminal use, while explicit commands remain fast and predictable for automation.

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
| `cli-presentation` | Contextual help, errors, profile tables, active markers, and restrained terminal styling | `profile-management` |
| `interactive-profile-workflows` | TTY-only profile forms, selectors, validation, confirmation, and cancellation | `profile-management`, `cli-presentation` |
| `shell-completion` | Bash, Zsh, and Fish completion for `vlt` commands, flags, and profile names | `profile-management` |

Build order:

- `profile-management` → `credential-lifecycle` → `vault-delegation`;
- `profile-management` → `cli-presentation` → `interactive-profile-workflows`;
- `profile-management` → `shell-completion`.

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

- With no argument in an interactive terminal, `switch` opens the profile selector defined by `interactive-profile-workflows`.
- With no argument outside an interactive terminal, `switch` writes its full standard help to standard error and exits non-zero without a separate missing-selection diagnostic.
- A name is the stable, script-safe selector.
- A number is an interactive convenience referring to the currently displayed lexicographic order.
- Selecting a profile persists it as the default for later `vlt` invocations.
- Selecting an unknown name or out-of-range number fails without changing the current selection.

### `cli-presentation`

#### Contextual help and errors

Every `vlt` management command and subcommand supports `-h` and `--help`. Explicit help writes to standard output and exits successfully. Help contains the command's purpose, full usage, available subcommands or options, and one or two representative examples.

When an invocation cannot continue because a required command, positional argument, or flag is missing, `vlt` writes that command's full standard help to standard error and exits non-zero. The help text must match the corresponding explicit `--help` output. Only the output stream and exit status differ. `vlt` must not prepend or append a separate sentence such as `command is required`, `argument is required`, or an instruction to rerun with `--help`.

This rule applies at every management-command level:

- bare `vlt` writes the top-level help;
- `vlt profile` writes the profile help;
- an incomplete profile subcommand writes that subcommand's help;
- an incomplete `switch` or `completion` command writes that command's help.

An omission that starts a specified interactive form or selector is not an error while the terminal requirements in `interactive-profile-workflows` are met. If the same invocation cannot start that workflow outside an interactive terminal, the missing-input help rule applies. No prompt or selector may start in a non-interactive invocation.

Invalid options, unexpected arguments, invalid values, and unknown profile subcommands keep a concise diagnostic on standard error and exit non-zero. The diagnostic must:

- identify the invalid value;
- show the relevant command usage rather than the full global help;
- suggest the next valid action;
- suggest at most one subcommand when a typo has one clear match;
- avoid stack traces, internal dependency errors, and credentials.

For example, `vlt profile` prints only the profile help for `add`, `list`, `show`, `update`, and `remove`. A typo such as `vlt profile udpate` still reports the unknown command and suggests `update`. Unknown top-level command names remain Vault arguments and are not intercepted for typo correction.

#### Profile output

`profile list` displays one lexicographically sorted row per profile with these columns:

```text
#  ACTIVE  NAME    ADDRESS                    NAMESPACE
1  *       team-a  https://vault.example.com  platform
2          team-b  https://vault.example.net  -
```

The one-based number remains a valid `switch` selector. `*` marks the active profile. Empty namespaces display as `-`. The table never includes usernames, credentials, keyring identifiers, or token-derived state.

`profile show` displays aligned labels for name, address, username, auth path, namespace, and active status. Add, update, remove, and switch continue to print one concise success message after the state change succeeds. No success message may precede persistence or authentication success.

Human-readable output is not a stable machine-readable interface. No JSON or other structured output mode is included in this scope.

#### Terminal styling

- Use restrained color for headings, labels, selections, and success or error states only.
- Enable color only when the destination is an interactive terminal that supports it.
- Disable color when output is redirected or `NO_COLOR` is set to a non-empty value.
- Keep spacing, markers, and wording understandable without color.
- Use ASCII markers and do not require patched fonts or icon glyphs.
- Keep delegated Vault output byte-for-byte under Vault's control; `vlt` must not restyle it.

### `interactive-profile-workflows`

Interactive workflows run only when both the input and display streams are attached to a terminal. Supplying enough explicit arguments to perform an operation bypasses prompts. A non-interactive invocation with missing required input writes the relevant full help to standard error, exits non-zero, and never waits for input.

#### Profile selection

The following incomplete commands open a lexicographically sorted profile selector in an interactive terminal:

```text
vlt switch
vlt profile show
vlt profile update
vlt profile remove
```

The selector shows the same number, active marker, name, address, and namespace information as `profile list`. It preselects the active profile when one exists. Selecting an entry supplies its stable profile name to the existing operation. If no profiles exist, the command does not open an empty selector; it explains how to run `vlt profile add`.

Cancelling with the interface's cancel action or an interrupt changes no configuration or credential state, prints a concise cancellation message, and exits non-zero.

#### Add form

`profile add` preserves supplied values and prompts only for missing required input in an interactive terminal:

- name: required and validated with the existing profile-name rules;
- address: required and validated as an absolute HTTP or HTTPS Vault URL;
- username: required and non-blank;
- auth path: optional input defaulting to `oidc`;
- namespace: optional and empty by default.

Each invalid answer is explained next to its field and can be corrected without restarting the command. When name, address, and username are supplied explicitly, the command runs directly with the existing auth-path and namespace defaults. The form submits through the same mutation and authentication behavior as the explicit command.

#### Update form

`profile update NAME` without change flags opens a form populated with the current address, username, auth path, and namespace. The stable profile name is displayed but cannot be changed. The form validates changed values before submission and writes nothing until the complete form is valid.

Supplying `NAME` and one or more update flags remains a direct partial update and does not open the form. This preserves the existing script-safe command contract. Running `profile update` without `NAME` first opens the selector and then the populated form.

#### Removal confirmation

`profile remove` without `NAME` opens the selector and then asks for confirmation. The confirmation names the selected profile and states when removing it will leave no active profile. Declining or cancelling changes nothing.

`profile remove NAME` remains an explicit, immediate operation without an additional prompt. Automation must not gain a prompt after supplying the required argument.

### `shell-completion`

```text
vlt completion bash
vlt completion zsh
vlt completion fish
```

Each command writes a sourceable completion script to standard output and writes diagnostics only to standard error. `vlt` never edits shell startup files or installs the script automatically. Unsupported shell names exit non-zero with contextual usage.

Completion covers:

- `vlt` management commands and the `completion` command;
- profile subcommands;
- management and global flags;
- valid values for the shell argument;
- stored profile names for `--profile`, `switch`, `profile show`, `profile update`, and `profile remove`.

Completion does not suggest existing names for `profile add`. After an invocation enters delegated Vault arguments, `vlt` supplies no Vault command, flag, path, or secret completion and does not invoke the Vault executable. Failure to load profile configuration returns no dynamic candidates and does not corrupt the user's shell prompt with diagnostics.

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

`vlt` reserves `switch`, `profile`, and `completion` as management commands. Every other command name is treated as a Vault command and forwarded after preflight. This preserves transparent delegation, so unknown top-level names are not treated as `vlt` typos.

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
- Explicit complete commands remain non-interactive and retain their existing behavior.
- Interactive workflows never start unless their input and display streams are terminals.
- Prompt cancellation and validation failure leave persistent state unchanged.
- Help, tables, forms, selectors, and completion remain usable without color.
- The normal success path adds minimal output before handing control to Vault.

## Tech Stack

- **Language:** Go 1.26, as pinned in `go.mod`.
- **CLI parsing:** Go standard library plus a small explicit dispatcher; delegated arguments remain opaque to `vlt`.
- **Terminal UX:** Standard-library formatting plus the smallest pinned Charm-family component set needed for focused forms, selectors, and styling. The interface remains inline and task-focused rather than a persistent full-screen application.
- **Shell completion:** Scripts generated by `vlt`; no external completion framework or Vault command introspection is required.
- **Configuration:** JSON via Go's standard library, stored under `os.UserConfigDir()` in a `vlt` directory.
- **Credential storage:** `github.com/zalando/go-keyring` v0.2.6, supporting Linux Secret Service, macOS Keychain, and Windows Credential Manager.
- **Vault integration:** The installed official `vault` executable invoked as a subprocess.
- **Testing:** Go's `testing` package with fakes for subprocesses, clocks, config storage, and keyring access.

Dependency versions must be pinned in `go.mod`/`go.sum`. The implementation plan must name and justify each terminal UI package before it is added. Any unrelated dependency requires separate approval.

## Commands

```console
# Apply canonical formatting
just fmt

# Run formatting, tests, race detection, vet, and the local build
just check

# Verify Linux, macOS, and Windows builds
just build-all

# Run during development
go run ./cmd/vlt --help
go run ./cmd/vlt completion bash
go run ./cmd/vlt completion zsh
go run ./cmd/vlt completion fish
```

`just build-all` verifies code portability, not runtime keyring or shell behavior.

## Project Structure

```text
cmd/vlt/
  main.go                 Application entry point and dependency wiring
internal/cli/
  ...                     Dispatch, command behavior, help, output, prompts, and completion
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
- error redaction;
- exact explicit and automatic help text, command-specific output streams and exit status, absence of duplicate missing-input diagnostics, and unambiguous subcommand suggestions;
- profile table ordering, columns, active markers, and empty namespace display;
- color enablement for terminals and suppression for redirection or `NO_COLOR`;
- interactive versus non-interactive dispatch at injected terminal boundaries;
- add and update form defaults, in-place validation, and cancellation;
- selector ordering, active preselection, empty-profile handling, and cancellation;
- removal confirmation and unchanged state after decline or cancellation;
- Bash, Zsh, and Fish script generation and completion candidates;
- silent dynamic-completion failure when profile configuration cannot load.

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
- bare `vlt` and missing management input outside a terminal return non-zero, write only the relevant full help to standard error, and do not read input;
- explicit `-h` and `--help` write the same command-specific text to standard output and return zero;
- interactive add, update, show, switch, and remove reach the same services as their explicit forms;
- cancelled or declined interactive operations do not change metadata, active selection, or credentials;
- generated completion scripts expose `vlt` commands, flags, shells, and stored profile names;
- completion never invokes Vault or exposes credentials;
- delegated exit status and standard streams are preserved;
- tokens never appear in output or persisted config.

### Platform checks

- All unit/component tests must pass on supported CI operating systems when CI is introduced.
- Cross-compilation must succeed for Linux, macOS, and Windows.
- Generated completion scripts receive syntax or smoke checks with Bash, Zsh, and Fish when those shell executables are available. Unit tests remain the portable requirement when a shell is unavailable.
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
- Keep explicit complete commands non-interactive.
- Detect terminal capabilities through injectable boundaries.
- Respect `NO_COLOR` and keep all management workflows understandable without styling.
- Keep completion limited to non-secret `vlt` metadata and command structure.
- Add or update tests for every behavior change.
- Run formatting, tests, race tests, static analysis, and cross-platform compilation before release.

### Ask first

- Add terminal UI dependencies not named and justified in the approved implementation plan.
- Add any dependency unrelated to the approved keyring or terminal UI integrations.
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
- Prompt for input when either the input or display stream is not a terminal.
- Add ANSI styling to redirected output or when `NO_COLOR` is set.
- Restyle, parse for presentation, or complete delegated Vault arguments.
- Modify shell startup files or install completion scripts automatically.
- Retry a delegated command after it may have executed.
- Remove or skip a failing security test merely to make checks pass.

## Success Criteria

The initial release is complete when all of the following are demonstrably true:

1. A user can add `team-a` and `team-b` profiles with address, username, and `oidc` auth path; each successful add completes OIDC and stores its token only in the native keyring.
2. `profile list` displays the deterministic numbered profile table, and switching by explicit name or number persists the same selected profile.
3. After `vlt switch team-a`, `vlt read ...` delegates unchanged arguments to Vault with team-a's address and token.
4. `vlt --profile team-b read ...` uses team-b once and leaves team-a active.
5. A valid token avoids browser authentication; a renewable token within five minutes of expiry is renewed; an invalid or expired token authenticates before delegation.
6. Network/TLS validation failures do not incorrectly trigger OIDC, and a delegated authentication failure is returned without retrying the operation.
7. Direct `vault ...` behavior and the parent shell environment remain unchanged.
8. Profile metadata survives process restarts, while no token appears in the config file, terminal output, logs, or command arguments.
9. Missing Vault, unavailable keyring, malformed login output, invalid profiles, and partial-update failures produce actionable errors without corrupting existing state.
10. Automated tests cover the functional and security flows above and pass under `just check`.
11. The program cross-compiles for Linux, macOS, and Windows, and the complete real login/read flow is manually verified on Linux.
12. Bare `vlt` and every missing required management command, positional argument, or flag write only the relevant full help to standard error and return non-zero. The text matches explicit `--help`, which writes to standard output and returns zero. Invalid input retains its specific diagnostic, and clear profile-subcommand typos receive one suggestion.
13. `profile list` and `profile show` expose the specified metadata and active status without credentials, work without color, and emit no ANSI sequences when redirected or when `NO_COLOR` is set.
14. In an interactive terminal, missing profile selectors open focused selection workflows for `switch`, `show`, `update`, and `remove`; the same invocations outside a terminal fail promptly with the relevant standard help and no separate missing-input diagnostic.
15. Interactive add and update validate fields in place, use the specified defaults, and reach the same persistence and authentication behavior as explicit commands.
16. Cancelling or declining an interactive operation leaves profile metadata, active selection, and credential entries unchanged.
17. `vlt completion bash`, `vlt completion zsh`, and `vlt completion fish` generate sourceable scripts that complete `vlt` commands, flags, supported shells, and stored profile names.
18. Completion and presentation changes neither invoke nor restyle delegated Vault operations and never expose credentials.

## Out of Scope

- Reimplementing any Vault data command or embedding a Vault server/client replacement.
- Changing the behavior of direct `vault ...` commands.
- Parent-shell environment mutation or profile activation scripts.
- Authentication methods other than OIDC.
- Multiple users, shared/team configuration, or profile synchronization between machines.
- Plaintext or file-based token fallback.
- Automatic retries of delegated Vault operations.
- Bundling or downloading the Vault executable.
- Live-Vault automated tests in the initial implementation.
- Initial runtime certification on macOS or Windows.
- Installers, package-manager publication, automatic updates, telemetry, and CI configuration.
- A stable machine-readable output API.
- JSON, YAML, or other structured management output.
- A persistent full-screen profile-management TUI.
- Completion for delegated Vault commands, flags, paths, or secrets.
- Automatic installation of completion scripts or edits to shell startup files.
- Suppressing `exit status 1` or other status text emitted by `go run` rather than by `vlt`.

## Open Questions

None currently. Any new requirement changes this specification before implementation proceeds.
