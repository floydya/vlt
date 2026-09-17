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
- Pin exactly one cross-platform native-keyring dependency. Any additional third-party dependency requires approval.

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
