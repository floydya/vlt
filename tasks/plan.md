# Implementation Plan: `vlt` Vault Profile CLI

## Overview

Build the approved `vlt` specification as thin, testable Go slices. The sequence establishes secure profile persistence first, then isolated Vault/keyring boundaries, credential login and preflight, transactional profile lifecycle commands, and finally transparent Vault delegation. Tests use fakes and temporary directories; no automated test requires a live Vault server or desktop keyring.

## Tracking

Tasks and checkpoints are tracked in beads. Run `bd ready` for the next unblocked issue and `bd show <id>` for its acceptance criteria, verification steps, likely files, and scope. Do not create `tasks/todo.md`.

## Architecture Decisions

- Keep profile metadata and active selection in one versioned JSON configuration under `os.UserConfigDir()/vlt`, with atomic replacement and Unix user-only permissions.
- Define narrow interfaces in consuming packages for filesystem/config, keyring, clock, executable discovery, and process execution so tests require neither Vault nor a desktop keyring.
- Keep subprocess construction centralized: argument vectors only, explicit per-profile environment overlay, attached streams for delegated operations, and captured/redacted output only for machine-readable credential operations.
- Model add and update as staged transactions: authenticate and persist the replacement token before committing metadata, with compensating keyring operations on failure so existing state is retained.
- Treat Vault token lookup JSON as the source of validity, renewability, and expiry decisions; classify only explicit invalid/expired responses as reauthentication triggers.
- Pin exactly one cross-platform native-keyring dependency for the original release. Add later dependencies only through an approved feature plan.

## Dependency Graph

```text
vlt-vfk  module skeleton
 ├─ vlt-8vx  secure config/profile repository
 │   └─ vlt-4pv  profile queries and selection
 │       └─ vlt-9vq  profile foundation checkpoint
 └─ vlt-ho7  safe Vault process boundary

vlt-9vq ── vlt-4q0  native keyring adapter
vlt-ho7 + vlt-4q0 ── vlt-1c0  OIDC login
vlt-4pv + vlt-ho7 + vlt-1c0 ── vlt-oah  credential preflight
vlt-ho7 + vlt-4q0 + vlt-1c0 + vlt-oah ── vlt-olr  credential checkpoint

vlt-olr + profile/credential foundations ── vlt-x7x  transactional profile mutations
vlt-4pv + vlt-x7x ── vlt-z7g  management CLI
vlt-olr + vlt-4pv + vlt-ho7 + vlt-oah ── vlt-5ec  transparent delegation
vlt-x7x + vlt-z7g + vlt-5ec ── vlt-bcb  end-to-end checkpoint

vlt-bcb + vlt-z7g + vlt-5ec ── vlt-0xm  component/security regression flows
vlt-0xm ── vlt-3dc  portability and Linux verification
vlt-3dc ── vlt-ei1  release-readiness checkpoint
```

## Task List

### Phase 1: Foundations and profile metadata

1. `vlt-vfk` — Initialize Go module and dependency-wiring skeleton
2. `vlt-8vx` — Persist validated profile configuration atomically
3. `vlt-4pv` — Implement deterministic profile lookup and active selection
4. `vlt-9vq` — Checkpoint: profile foundation

### Phase 2: Vault and credential lifecycle

5. `vlt-ho7` — Add the safe Vault process boundary
6. `vlt-4q0` — Integrate the native credential store
7. `vlt-1c0` — Implement secure OIDC login through Vault
8. `vlt-oah` — Implement token validation, renewal, and reauthentication decisions
9. `vlt-olr` — Checkpoint: credential lifecycle

### Phase 3: Complete user-facing slices

10. `vlt-x7x` — Implement transactional profile add, update, and remove
11. `vlt-z7g` — Expose profile management and switch commands
12. `vlt-5ec` — Delegate opaque Vault commands with profile preflight
13. `vlt-bcb` — Checkpoint: end-to-end fake-backed behavior

### Phase 4: Security, portability, and release verification

14. `vlt-0xm` — Add complete component and security regression flows
15. `vlt-3dc` — Verify cross-platform builds and Linux native integration
16. `vlt-ei1` — Checkpoint: initial release readiness

## Parallelization

- After `vlt-vfk`, secure profile persistence (`vlt-8vx`) and the safe Vault process boundary (`vlt-ho7`) can proceed in parallel.
- Credential-store work waits for the profile-foundation checkpoint; OIDC then waits for both keyring and process boundaries.
- After the credential checkpoint, transactional management (`vlt-x7x`) and delegation (`vlt-5ec`) can proceed in parallel, coordinating changes to shared CLI wiring.
- Final component/security testing and release verification remain sequential because their evidence depends on completed behavior.

## Risks and Mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| Keyring library differs by platform or pulls unexpected dependencies | High | Pin one approved library early, isolate it behind a contract, and cross-compile before CLI completion. |
| Update rollback cannot atomically span config and native keyring | High | Stage new authentication first, snapshot old state, define compensating operations, and inject failures at every boundary. |
| Vault CLI JSON/error shapes cause incorrect reauthentication | High | Fixture explicit valid/invalid/network responses and default unknown failures to no-login. |
| Secret material leaks through subprocess diagnostics | High | Centralize redaction and run synthetic canary-token assertions over args, output, errors, and config. |
| Signal/exit behavior varies across operating systems | Medium | Keep translation in `vaultexec`, test portable exit codes, and use platform-specific files only where necessary. |
| Numeric profile selectors race with concurrent config changes | Low | Resolve from one freshly loaded, sorted snapshot and atomically persist only the resolved stable name. |
| Manual Linux verification exposes a real token in artifacts | High | Never capture raw login output, use normal command paths, inspect only metadata/output, and record sanitized results. |

## Open Questions

None. Requirement changes must update `SPEC.md` before implementation.

## Approved CLI Experience Extension

### Overview

Implement the approved `cli-presentation`, `interactive-profile-workflows`, and `shell-completion` modules as small vertical slices. Preserve explicit command behavior and opaque Vault delegation. Beads feature `vlt-a9w` owns this extension, and its child issues are the task list target.

### Architecture Decisions

- Pin `charm.land/huh/v2` v2.0.3 for focused inputs, selectors, confirmations, validation, custom I/O, and accessible rendering. Do not import Bubble Tea directly.
- Pin `charm.land/lipgloss/v2` v2.0.6 for profile tables and restrained styling. Keep semantic output separate so plain output has exact tests.
- Put terminal detection behind an injected CLI boundary. Require both input and display streams to be terminals before prompting.
- Keep the explicit dispatcher. Reserve only the documented `completion` namespace and keep unknown top-level names opaque to Vault.
- Generate completion scripts in `vlt`. Use an internal mode under `completion` for name candidates instead of parsing human output or reading profile JSON from shell code.
- Reuse existing profile services and mutation transactions. Interactive workflows collect or select values but do not duplicate persistence, authentication, or rollback logic.
- Keep this change in one final commit. Obtain approval for the proposed wording before committing.

### Dependency Graph

```text
vlt-a9w.1 -> vlt-a9w.2 -> vlt-a9w.3 -> vlt-a9w.4

vlt-a9w.4 -> vlt-a9w.5
vlt-a9w.4 -> vlt-a9w.6
vlt-a9w.5 + vlt-a9w.6 -> vlt-a9w.7

vlt-a9w.4 -> vlt-a9w.8
vlt-a9w.5 + vlt-a9w.8 -> vlt-a9w.9 -> vlt-a9w.10 -> vlt-a9w.11

vlt-a9w.7 -> vlt-a9w.12 -> vlt-a9w.13 -> vlt-a9w.14
vlt-a9w.14 -> vlt-a9w.15
vlt-a9w.14 -> vlt-a9w.16
vlt-a9w.14 -> vlt-a9w.17
vlt-a9w.15 + vlt-a9w.16 + vlt-a9w.17 -> vlt-a9w.18

vlt-a9w.11 + vlt-a9w.18 -> vlt-a9w.19 -> vlt-a9w.20
```

### Task List

#### Phase 5: Terminal UX foundation

1. `vlt-a9w.2` — Pin focused terminal UI dependencies
2. `vlt-a9w.3` — Add an injectable terminal capability boundary
3. `vlt-a9w.4` — Checkpoint: terminal UX foundation

#### Phase 6: Contextual CLI presentation

4. `vlt-a9w.5` — Add contextual management help and typo guidance
5. `vlt-a9w.6` — Render clear profile lists and details
6. `vlt-a9w.7` — Checkpoint: contextual CLI presentation

#### Phase 7: Shell completion

7. `vlt-a9w.8` — Generate Bash, Zsh, and Fish completion scripts
8. `vlt-a9w.9` — Route and wire completion commands
9. `vlt-a9w.10` — Complete stored profile names safely
10. `vlt-a9w.11` — Checkpoint: shell completion

#### Phase 8: Interactive selection

11. `vlt-a9w.12` — Select a profile interactively for switch
12. `vlt-a9w.13` — Select a profile interactively for show
13. `vlt-a9w.14` — Checkpoint: interactive profile selection

#### Phase 9: Guided mutation workflows

14. `vlt-a9w.15` — Guide profile creation with an interactive form
15. `vlt-a9w.16` — Edit profile metadata with an interactive form
16. `vlt-a9w.17` — Confirm interactive profile removal
17. `vlt-a9w.18` — Checkpoint: guided profile workflows

#### Phase 10: Integrated verification

18. `vlt-a9w.19` — Verify the guided CLI experience end to end
19. `vlt-a9w.20` — Checkpoint: CLI experience readiness

### Parallelization

- After `vlt-a9w.4`, contextual help, profile presentation, and pure completion-script generation can proceed independently.
- Completion routing waits for contextual help and script generation. Dynamic profile candidates then follow routing.
- Interactive switch waits for the presentation checkpoint because it establishes the shared profile display contract.
- After the selection checkpoint, add, update, and remove workflows can proceed independently if changes to shared interactive helpers are coordinated.
- End-to-end verification waits for both completion and guided-workflow checkpoints.

### Risks and Mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| Terminal detection behaves differently for injected streams or supported operating systems | High | Isolate detection, test fake capabilities, and cross-build immediately after the foundation slice. |
| Huh cancellation or custom I/O bypasses state-safety guarantees | High | Wrap Huh behind a narrow adapter and assert no service calls for cancel, interrupt, and validation failure. |
| Shell quoting differs across Bash, Zsh, and Fish | High | Keep separate deterministic templates and run syntax checks whenever each shell is installed. |
| Styling leaks ANSI into redirects or `NO_COLOR` output | Medium | Render semantic output first and test exact unstyled bytes for both suppression paths. |
| Dynamic completion exposes errors or metadata in the shell prompt | High | Emit sorted names only, suppress load diagnostics in completion mode, and use token-canary tests. |
| New reserved routing captures a Vault command accidentally | Medium | Reserve only `completion`; keep all other unknown top-level names covered by opaque-delegation tests. |
| Interactive tests become PTY-dependent and flaky | Medium | Unit-test handlers with fake selectors/forms; keep real terminal behavior in focused adapter tests. |

### Open Questions

None. Requirement changes must update `SPEC.md` before implementation.

## Automatic Help Revision

**Status:** Approved

### Overview

Implement the approved automatic-help revision for the `cli-presentation` module. Missing required input prints the relevant canonical help to standard error and returns non-zero. Explicit help, invalid-input diagnostics, interactive profile workflows, completion behavior, and opaque Vault delegation keep their existing contracts. Beads feature `vlt-51r` owns this revision and its child issues are the task list target.

### Architecture Decisions

- Represent automatic help as one typed CLI result carrying canonical help text. Do not add a second copy of any help body.
- Render automatic help verbatim at the process dispatch boundary so handlers do not write error-triggered help to standard output and the normal `vlt:` error prefix is not added.
- Keep explicit `-h` and `--help` on the existing standard-output success path. Compare explicit and automatic help as exact bytes in tests.
- Treat a specified TTY form or selector as a valid invocation. Use automatic help only when required input is missing and the interactive workflow cannot run.
- Keep invalid options, unexpected arguments, invalid values, typo suggestions, redaction, and unknown top-level Vault routing on their existing diagnostic paths.
- Add no dependency. Implement each behavior slice with RED/GREEN TDD and retain the evidence in Beads.
- Keep this revision in one final commit. Obtain approval for the plan, readiness, and proposed commit wording at their defined gates.

### Dependency Graph

```text
vlt-mvb -> vlt-51r.1 -> vlt-51r.2

vlt-51r.2 -> vlt-51r.3
vlt-51r.2 -> vlt-51r.4

vlt-51r.3 + vlt-51r.4 -> vlt-51r.5
vlt-51r.5 -> vlt-51r.6 -> vlt-51r.7
```

### Task List

Planning gate: `vlt-51r.1` — Plan automatic help implementation

#### Phase 11: Automatic help foundation and command slices

1. `vlt-51r.2` — Add the automatic help result and root behavior
2. `vlt-51r.3` — Return automatic help for incomplete profile workflows
3. `vlt-51r.4` — Return automatic help for incomplete completion commands
4. `vlt-51r.5` — Checkpoint: automatic help command coverage

#### Phase 12: Integrated evidence

5. `vlt-51r.6` — Verify automatic help end to end
6. `vlt-51r.7` — Checkpoint: automatic help readiness

### Parallelization

- `vlt-51r.2` is sequential because it establishes the shared result and process-boundary rendering contract.
- After `vlt-51r.2`, profile workflows (`vlt-51r.3`) and completion (`vlt-51r.4`) can proceed independently if they do not change the shared result contract.
- The command-coverage checkpoint waits for both command-family slices. Integrated evidence and final readiness remain sequential.

### Risks and Mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| Automatic and explicit help text drift apart | High | Carry the existing canonical help body and assert byte-for-byte parity for every command level. |
| The normal error renderer adds `vlt:` or a missing-input sentence | High | Detect automatic help at the process boundary and assert exact stderr output. |
| Missing-input detection replaces an interactive form or selector | High | Test TTY and non-TTY variants together and assert service calls and persistent state. |
| Invalid input is misclassified as missing input | High | Retain dedicated option, value, typo, and unexpected-argument regression cases. |
| Bare `vlt` changes from success to failure unexpectedly for callers | Medium | Cover status, streams, handler calls, and explicit root help at both CLI and process boundaries. |
| Completion internals or delegated Vault routing are captured | Medium | Keep `__profiles` and unknown top-level command regression tests in the checkpoint matrix. |

### Open Questions

None. The project owner approved this plan on 2026-09-19.

## Favorite Shortcuts Revision

**Status:** Approved

### Overview

Implement the approved `favorite-workflows` capability as small, testable slices. Store personal favorite targets locally, manage them through explicit and guided commands, search them with `fzf`, delegate the selected `read` or `kv get` operation through the existing Vault boundary, and require approval before profile removal deletes linked favorites. Beads feature `vlt-47y` owns this revision and its child issues are the task list target.

### Architecture Decisions

- Store favorite metadata in a separate `favorites.json` file under the existing `vlt` user configuration directory. Keep the current profile schema unchanged.
- Put favorite validation, exact tuple identity, path-profile-operation ordering, persistence, and mutations in `internal/favorite`.
- Use the user-installed `fzf` executable for the read selector. The pinned Huh v2.0.3 selector performs case-insensitive substring filtering, so it does not meet the approved fuzzy-search contract.
- Pass searchable rows to `fzf` without a shell and map its opaque selected value back to a loaded favorite. Never parse profile, path, operation, or note text to recover identity.
- Reuse Huh for guided add, update, and removal workflows. These workflows need forms and ordinary selection, not fuzzy search.
- Send selected favorite operations through the existing Vault handler as `--profile NAME read PATH` or `--profile NAME kv get PATH`. Keep preflight, environment isolation, streams, exit status, and redaction in the existing boundary.
- Coordinate profile and favorite removal with a snapshot and compensating restoration because the metadata lives in two atomic files.
- Implement behavior changes with RED/GREEN TDD. Keep the feature in one final commit and obtain current-message authority before committing.

### Dependency Graph

```text
vlt-47y.1 -> vlt-47y.2 -> vlt-47y.3 -> vlt-47y.4

vlt-47y.4 -> vlt-47y.5
vlt-47y.4 -> vlt-47y.6
vlt-47y.5 + vlt-47y.6 -> vlt-47y.7 -> vlt-47y.8

vlt-47y.8 -> vlt-47y.9 -> vlt-47y.10
vlt-47y.4 -> vlt-47y.11
vlt-47y.10 + vlt-47y.11 -> vlt-47y.12 -> vlt-47y.13

vlt-47y.8 -> vlt-47y.14
vlt-47y.13 + vlt-47y.14 -> vlt-47y.15 -> vlt-47y.16
```

### Task List

Planning gate: `vlt-47y.1` — Plan favorite shortcuts implementation

#### Phase 13: Favorite storage foundation

1. `vlt-47y.2` — Persist and query favorite metadata
2. `vlt-47y.3` — Mutate favorites against stored profiles
3. `vlt-47y.4` — Checkpoint: favorite storage foundation

#### Phase 14: Explicit commands and fuzzy selector

4. `vlt-47y.5` — Select favorites through `fzf`
5. `vlt-47y.6` — Expose explicit favorite management commands
6. `vlt-47y.7` — Route and wire the favorite command
7. `vlt-47y.8` — Checkpoint: explicit favorite CLI

#### Phase 15: Guided execution and safe profile removal

8. `vlt-47y.9` — Guide favorite add, update, and removal
9. `vlt-47y.10` — Execute the selected favorite once
10. `vlt-47y.11` — Coordinate profile and favorite cascade removal
11. `vlt-47y.12` — Require approval for linked profile removal
12. `vlt-47y.13` — Checkpoint: guided favorite workflows

#### Phase 16: Completion and integrated verification

13. `vlt-47y.14` — Complete favorite commands and values in shells
14. `vlt-47y.15` — Verify favorite workflows end to end
15. `vlt-47y.16` — Checkpoint: favorite workflow readiness

### Parallelization

- After the storage checkpoint, `fzf` selection (`vlt-47y.5`), explicit command handling (`vlt-47y.6`), and cascade coordination (`vlt-47y.11`) can proceed independently.
- Application wiring waits for both the selector and explicit handler. The explicit CLI checkpoint gates every user-facing workflow.
- After that checkpoint, guided management (`vlt-47y.9`) and shell completion (`vlt-47y.14`) can proceed independently.
- One-shot execution follows guided management to avoid concurrent edits to the favorite handler. Profile-removal wiring then waits for execution and the independent cascade coordinator.
- Integrated verification waits for both the guided-workflow checkpoint and shell completion. Final readiness remains sequential.

### Risks and Mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| Visible `fzf` rows are ambiguous or contain user-controlled separators | High | Map a selected opaque value to the loaded snapshot and sanitize display-only rows; never reconstruct identity from visible text. |
| Missing or cancelled `fzf` starts credential work or Vault execution | High | Resolve the selector result before delegation and assert zero preflight and process calls on every early exit. |
| Separate profile and favorite files leave a partial cascade | High | Snapshot linked favorites, use compensating restoration, inject failure at each persistence and credential boundary, and stop at the cascade checkpoint if rollback is not proven. |
| Numeric selectors resolve against a different order during mutation | Medium | Resolve and mutate from one freshly loaded snapshot inside the favorite service. |
| Guided and explicit paths drift into separate behavior | Medium | Keep validation and persistence in shared favorite services; UI adapters only collect values and selectors. |
| New routing captures opaque Vault arguments | High | Reserve only the documented `favorite` namespace and keep unknown-command delegation regression tests. |
| Paths, notes, tokens, or Vault output leak through diagnostics or fixtures | High | Use synthetic canaries, central redaction, sanitized manual evidence, and no raw secret output in tests or documentation. |
| `fzf` process behavior differs across supported systems | Medium | Inject discovery and execution, cross-build all targets, use a fake executable in automation, and perform sanitized Linux runtime verification. |

### Open Questions

None. The project owner approved this plan on 2026-09-19.

## Security Hardening Review Follow-up

**Status:** Approved on 2026-09-20

### Overview

Address the three repository-wide findings from the five-axis and security review as separate Beads tasks. Keep HTTPS as the default, allow HTTP only through a persisted per-profile opt-in, reject untrusted configuration paths before they can influence Vault execution, and serialize state-changing workflows across processes.

### Architecture Decisions

- Store an explicit per-profile insecure-transport choice and expose it as `--allow-insecure`. HTTPS needs no opt-in. HTTP remains available for local test Vaults.
- Validate the address and insecure choice together at the profile boundary. Show the insecure state in profile output and support enabling or clearing it through explicit and interactive update flows.
- Apply one shared secure-file policy to profile and favorite metadata. Reject unsafe existing state instead of silently changing ownership or permissions.
- Use one application-wide advisory lock in the private `vlt` configuration directory. Acquire it once around complete mutations and compensating rollback so cascade workflows cannot deadlock.
- Implement each task with RED/GREEN TDD. Keep each task independently reviewable and run its focused and repository-wide verification before closure.

### Dependency Graph

```text
vlt-8xr  explicit insecure HTTP opt-in

vlt-quo  trusted configuration paths
   └─ vlt-op6  cross-process mutation lock
```

`vlt-8xr` and `vlt-quo` can proceed independently. `vlt-op6` waits for `vlt-quo` because its lock file must live in a trusted private directory.

### Task List

1. `vlt-8xr` — Require explicit opt-in for insecure Vault HTTP
2. `vlt-quo` — Reject unsafe configuration paths and permissions
3. `vlt-op6` — Serialize cross-process state mutations

### Verification Checkpoint

- Run focused tests for each task before its full suite.
- Run `just check`, `just build-all`, `golangci-lint run --build-tags=gms_pure_go ./...`, `go mod verify`, and `git diff --check`.
- Run `govulncheck ./...` and the Nix flake checks before final readiness.
- Review configuration migration, secret-safe diagnostics, HTTP opt-in visibility, and deterministic concurrency evidence.
- Obtain approval before committing or pushing.

### Risks and Mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| Existing profiles become unreadable after adding the opt-in field | High | Keep the field backward-compatible and default it to secure HTTPS-only behavior. |
| A configuration attacker enables insecure transport | High | Complete trusted-path enforcement independently and before the locking task. |
| Holding the state lock through OIDC blocks another mutation | Medium | Lock only state-changing workflows, honor cancellation while waiting, and keep read-only and delegated commands concurrent. |
| File ownership and locking differ across operating systems | High | Isolate platform-specific code, test supported behavior, and cross-build Linux, macOS, and Windows. |

### Open Questions

None. The proposed CLI contract is a persisted per-profile `--allow-insecure` opt-in that can be cleared during profile update.
