# Initial release verification

Verification date: 2026-09-19

CLI revision verification date: 2026-09-21

### Terminal UX and stable favorite ID revision

The `feat/vlt-ux-revision` worktree passed `just check` with `GOFLAGS=-buildvcs=false`, `just build-all`, `go mod verify`, `go mod tidy -diff`, direct `golangci-lint run --build-tags=gms_pure_go ./...` with `0 issues`, and `git diff --check`. The first plain `just check` passed formatting, tests, race tests, and vet, then stopped at the build because the linked worktree could not provide Go VCS status. Disabling build stamping allowed the same build to pass. `bd preflight --check` reported a lint failure without diagnostics, while its lint command passed directly; `bd preflight --check --skip-lint` passed the other checks. The cross-build compiled Linux amd64, macOS amd64 and arm64, and Windows amd64. Native keyring, real OIDC, delegated Vault execution, and macOS or Windows runtime behavior were not rerun.

Focused tests cover version 1 favorite migration without a read-time write, stable IDs across edits, rejected stale guided actions, responsive list and picker rendering, conditional HTTP prompts, the active profile default, `profile current`, and ID-only shell completion.

An isolated v0.2.0 fixture was read successfully by both the released and new builds. A favorite update with the new build wrote version 2 metadata; the v0.2.0 build then rejected it with `decode favorites: unknown field`. The v0.2.1 source still uses version 1 favorite metadata, so it has the same rollback limit. Both tested builds also read the current local profile and favorite metadata without errors. No current local metadata was changed. Rolling back after a favorite write requires restoring a private pre-upgrade copy of `favorites.json` and loses later favorite changes.

The color-scope correction passed `just check`, `just build-all`, and direct `golangci-lint` with `0 issues`. Its new regression tests first failed on shared headings, form focus styles, list markers, inactive detail status, and selectors, then passed after the active-profile accent wiring was removed. The obsolete active-accent tests were replaced with semantic-color and identity-scope assertions. A synthetic terminal run showed the active list profile color only on its name and `*`, and the other profile color only on its name. An inactive profile detail view showed only that profile's name color. No real profile values were captured.

## Automated checks

The following checks passed from a clean local clone with isolated writable Go build and module caches at commit `172e90e`:

| Check | Result |
|---|---|
| Formatting, unit tests, race tests, vet, and local build (`just check`) | Passed |
| Linux amd64 build | Passed |
| macOS amd64 build | Passed |
| Windows amd64 build | Passed |
| Pinned module verification (`go mod verify`) | Passed |

The clean module cache resolved only the versions pinned by `go.mod` and `go.sum`.

The release-checkpoint refinements also passed `just check`, `just build-all`, `go mod verify`, and `golangci-lint run --build-tags=gms_pure_go ./...` in the current worktree. The guided CLI regression suite covers the complete interactive, presentation, and completion boundaries without a live Vault or desktop keyring. Bash and Zsh completion passed installed-shell syntax checks. Fish was not installed, so its generated script remains covered by exact-output and semantic unit tests rather than a local Fish parser.

The favorite-workflow increment passed `just check`, `just build-all`, and `go mod verify`. Fake-backed component tests cover explicit and guided favorite CRUD, both delegated read mappings, selector cancellation and absence, cascade approval, and cascade rollback. Security canaries confirm that favorite configuration and management output do not retain keyring tokens or delegated secret values.

The unified terminal UX increment passed the focused presentation, selector, dispatch, component, and security suites at commit `e32e941`. It also passed `just check`, `just build-all`, `go mod tidy -diff`, `go mod verify`, and `git diff --check`. The cross-build gate compiled Linux, macOS, and Windows amd64 targets. macOS and Windows runtime behavior remains unverified.

The insecure-transport opt-in increment passed focused domain, persistence, mutation, credential, delegation, CLI, completion, and interactive-form tests. It also passed `just check`, `just build-all`, `go mod verify`, `golangci-lint run --build-tags=gms_pure_go ./...`, and `git diff --check`. `govulncheck` was not installed, so that optional final security scan did not run. Tests cover HTTPS defaults, remote and loopback HTTP rejection, persisted opt-in compatibility, enable and clear updates, secret-safe diagnostics, and login, preflight, and delegated execution boundaries.

The secure metadata-path increment passed focused profile, favorite, and shared filesystem tests. It also passed `just check`, `just build-all`, `go mod verify`, `golangci-lint run --build-tags=gms_pure_go ./...`, and `git diff --check`. Tests cover private creation, Unix ownership and permission checks, symbolic links, non-regular files, held-directory replacement, atomic target replacement, store parity, and path redaction. Linux behavior ran locally. macOS and Windows compiled, but their runtime behavior remains unverified.

The cross-process mutation-lock increment passed focused lock and deterministic concurrency tests. It also passed `just check`, `just build-all`, `go mod verify`, `golangci-lint run --build-tags=gms_pure_go ./...`, and `git diff --check`. Tests cover independent profile and favorite service instances, active selection racing with profile creation, every mutation route, unlocked read-only routes, cancellation, private lock-file creation, unsafe lock paths, and error redaction. Linux locking ran locally. macOS and Windows compiled, but their lock runtime behavior remains unverified.

### Favorite usage, profile color, and Zsh completion revision

Each implementation branch passed focused regression tests and `just check`. The favorite and color checkpoints also passed combined tests in temporary, uncommitted merge worktrees. The final interactive color merge passed `just check` and `just build-all` with `GOFLAGS=-buildvcs=false` because Go could not read VCS status during a pending merge. Normal `just check` passed on each source branch. The cross-build compiled Linux amd64, macOS amd64 and arm64, and Windows amd64; it did not run those targets natively.

The favorite picker, profile picker, color form, color completion, and Zsh fix branch heads merged cleanly in one temporary worktree. That combined tree passed `just check`, `just build-all`, `go mod verify`, `git diff --check`, and direct `golangci-lint` with `0 issues`. `GOFLAGS=-buildvcs=false` was needed only for builds in the uncommitted merge worktree.

Installed Bash and Zsh passed generated-script syntax tests. Fish was unavailable, so its generated script passed content tests but no local Fish parser or runtime check. A live Zsh session inserted `completion` without `=` and completed `--color` for `profile add`. A synthetic Linux terminal check showed a saved profile color without `NO_COLOR`; its transcript had 28 ANSI escapes. The same check with `NO_COLOR=1` had zero ANSI escapes and readable rows. No real Vault, OIDC, or native credential operation was rerun for this revision.

### Selector color and heading follow-up

Focused CLI tests and `just check` passed after the selector change. The temporary worktree required `GOFLAGS=-buildvcs=false` for the build step because Go could not read VCS status. The first `just check` passed tests, race tests, and vet before that build error. `just build-all` passed for Linux amd64, macOS amd64 and arm64, and Windows amd64 with the same flag. Direct `golangci-lint` reported `0 issues`, and `git diff --check` passed. `bd preflight --check` passed its tests and repository checks but reported a lint failure without diagnostics. `bd preflight --check --skip-lint` passed the remaining checks; Beads' own version-sync check was unavailable in this repository.

A synthetic Linux terminal check used two profiles with `.invalid` addresses and no credentials. Both `switch` and `favorite` showed aligned headings below search and each row in its linked profile's color. With `NO_COLOR=1`, the favorite picker showed the same headings and rows without color codes. Escape canceled each selector before any Vault or keyring operation.

The documentation branch passed `just check`, `just build-all`, `go mod verify`, and `git diff --check`. `bd preflight --check` passed its tests and repository checks but reported a lint failure without diagnostic output. Its exact lint command, `golangci-lint run --build-tags=gms_pure_go ./...`, returned `0 issues` directly. `bd preflight --check --skip-lint` passed the remaining checks. The Beads version-sync check was skipped because this repository does not contain Beads' own version file.

## Runtime verification

| Platform | Native credential store | Real OIDC and delegated read | Status |
|---|---|---|---|
| Linux | Passed against the active profile through GNOME Keyring Secret Service | OIDC login and a delegated `sys/health` read passed | Passed |
| macOS | Not run | Not run | Unverified |
| Windows | Not run | Not run | Unverified |

The Linux config directory and file use `0700` and `0600` permissions. The official Vault CLI is installed. The native credential probe read the active profile entry successfully without exposing a profile name, endpoint, token, or keyring value. The initial unavailable-or-locked result came from a sandboxed probe and was discarded. A check outside the sandbox confirmed that Secret Service is available and `vlt` can read its credential.

After VPN connection, the configured Vault health endpoint returned an HTTP response. The active credential was invalid or expired, so `vlt` completed OIDC and replaced the profile token in GNOME Keyring. A delegated `vlt read -format=json sys/health` then succeeded. The read did not change the refreshed token, and the token did not appear in profile configuration, captured standard output, or captured standard error. No raw Vault response or credential was written to this document.

Cross-compilation does not certify macOS or Windows runtime behavior.

A sanitized Linux PTY check used three synthetic profiles with `.invalid` addresses and no credential or Vault access. The inline selector showed three compact rows with the active marker. Typing `bt` immediately reduced the list to `beta`; this query is not a substring of `beta`, so the result also confirmed fuzzy matching. Enter selected `beta` and printed one success sentence. A second run preselected `beta`; Escape exited with status 1 and the single `operation canceled` diagnostic. A following profile list confirmed that cancellation left `beta` active.

The same synthetic profile list was captured through redirected output and contained deterministic plain rows with no ANSI sequence. A PTY run with `NO_COLOR=1` produced the same unstyled table. The transcript contained only synthetic profile names, `.invalid` addresses, and the fixed status text. It contained no token, keyring value, Vault value, host-specific resource name, or delegated secret output.

Fake-backed delegated `read PATH` and `kv get PATH` checks confirmed the one-command profile override and left the active profile unchanged. No raw secret output was recorded.

## Success criteria

| Criterion | Evidence |
|---|---|
| 1. Add two profiles through OIDC and store tokens only in the native keyring | `TestFakeBackedProfileFlowUsesActiveAndOneCommandProfiles`, `TestAuthenticatorLoginInvokesVaultAndStoresClientToken`, and the Linux OIDC/keyring check above |
| 2. Display deterministic profile choices and persist name or number selection | `TestProfileHandlerListUsesStableNumberedOrder`, `TestSwitchHandlerSelectsNamesAndNumbers`, `TestServiceListReturnsLexicographicallySortedProfiles`, and `TestStoreSetActiveProfilePersistsResolvedSelection` |
| 3. Delegate unchanged arguments with the active profile | `TestFakeBackedProfileFlowUsesActiveAndOneCommandProfiles` and `TestDelegateUsesActiveProfileAndPreservesOpaqueInvocation` |
| 4. Apply a one-command override without changing the active profile | `TestFakeBackedProfileFlowUsesActiveAndOneCommandProfiles` and `TestDelegateProfileOverrideAppliesOnceWithoutChangingActiveSelection` |
| 5. Reuse, renew, or replace credentials according to token state | `TestPreflightMakesCredentialLifecycleDecisions`, `TestFakeBackedPreflightRenewsBeforeDelegation`, and `TestFakeBackedPreflightAuthenticatesInvalidCredentialBeforeDelegation` |
| 6. Avoid OIDC on validation transport failures and avoid delegated retries | `TestPreflightNetworkValidationFailureDoesNotAuthenticate`, `TestFakeBackedPreflightStopsOnTransportFailureWithoutLogin`, and `TestFakeBackedDelegatedFailurePreservesStreamsAndExitWithoutRetry` |
| 7. Leave direct Vault behavior and the parent environment unchanged | `TestExecutorPreservesArgumentVectorAndProfileEnvironment`, `TestLoginEnvironmentSetsProfileValuesAndRemovesInheritedCredentials`, and `TestLoginEnvironmentRemovesInheritedNamespaceWhenProfileHasNone` |
| 8. Persist metadata without exposing tokens | `TestStoreRoundTrip`, `TestFakeBackedProfileFlowUsesActiveAndOneCommandProfiles`, the component security tests, and the Linux leakage check above |
| 9. Fail safely for missing dependencies, invalid data, and partial updates | `TestExecutorFailsActionablyBeforeStartingWhenVaultIsMissing`, `TestStoreContractReturnsActionableRedactedBackendFailures`, `TestAuthenticatorLoginRejectsMalformedOrMissingClientToken`, the profile validation tests, and the mutation rollback tests |
| 10. Pass automated functional and security gates | The clean-clone `just check` result above covers formatting, unit and component tests, race tests, vet, and the local build |
| 11. Cross-compile and complete a real Linux login/read flow | The clean-clone `just build-all` result and Linux runtime check above |
| 12. Write canonical automatic help for missing management input while preserving explicit help and invalid-input guidance | `TestRunRootHelpDoesNotRequireConfigDirectory`, `TestDispatchWritesAutomaticManagementHelpToStderr`, `TestProfileHandlerDisplaysContextualHelpWithoutCallingServices`, `TestProfileHandlerSuggestsOneClearSubcommandTypo`, and `TestDispatchWritesProfileGuidanceToStderr` |
| 13. Present all vlt-owned output through one deterministic plain and styled contract | `TestPresentationPlainPrimitivesAreExactAndANSIFree`, `TestPresentationStyledPrimitivesStripExactlyToPlain`, `TestProfileListPresentationPlainAndStyledAreExact`, `TestFavoriteListPresentationPlainAndStyledAreExact`, `TestProfileMutationSuccessOutputUsesSharedStatusPresentation`, `TestFavoriteMutationSuccessOutputUsesSharedStatusPresentation`, `TestDispatchWritesOneANSIFreeDiagnosticPrefix`, `TestNewDispatcherRedirectedOutputIsPlainAndDeterministic`, `TestTerminalDisablesColorForAnyNonEmptyNoColorValue`, and the sanitized Linux redirection and `NO_COLOR` checks above |
| 14. Use one compact in-process selector for every interactive choice with deterministic fuzzy behavior and safe cancellation | `TestSharedSelectorEmptyQueryPreservesSourceOrder`, `TestSharedSelectorTypingFiltersImmediatelyAndReturnsStableIdentity`, `TestSharedSelectorNavigationStartsAtPreselectedIdentity`, `TestSharedSelectorHandlesPasteCancellationAndInterrupt`, `TestSharedSelectorRendersCompactInlinePlainView`, `TestSharedProfileSelectorBuildsDeterministicSearchRowsAndPreselectsActive`, `TestFavoriteManagementUsesSharedSelectorRowsAndStableIdentity`, `TestFavoriteFormUsesSharedProfileAndOperationSelectorsWithDefaults`, and the sanitized Linux PTY check above |
| 15. Validate interactive add and update fields through the existing services | `TestHuhProfileFormPreservesDefaultsAndCorrectsInvalidFields`, `TestProfileHandlerUpdateUsesPopulatedReadOnlyNameForm`, and `TestFakeBackedGuidedManagementFlowUsesExistingServices` |
| 16. Preserve metadata, active selection, and credentials after cancellation or decline | `TestFakeBackedInteractiveCancellationAndDeclineLeaveStateUnchanged`, `TestProfileHandlerAddCancelAndInterruptDoNotMutate`, `TestProfileHandlerUpdateFormFailureDoesNotMutate`, and `TestProfileHandlerRemoveStopsBeforeMutationWhenNotConfirmed` |
| 17. Generate valid Bash, Zsh, and Fish completion with commands, flags, shells, and stored profile names; omit the Zsh root `=` suffix | `TestCompletionHandlerWritesScripts`, `TestCompletionHandlerEmitsSortedProfileNamesOnly`, `TestCompletionScriptsSuggestProfileColorForAddAndUpdate`, `TestZshRootCompletionInsertsOnlyCommandNames`, `TestCompletionScriptsHaveValidInstalledShellSyntax`, and the live Zsh check above |
| 18. Keep completion and presentation separate from Vault delegation and credentials | `TestFakeBackedCompletionNoColorAndDelegationStayIndependent`, `TestGuidedOutputsPromptsCompletionAndFailuresDoNotExposeCredentials`, `TestManagementDiagnosticRedactsCredentialAndStripsANSI`, `TestFavoriteExecutionOutputContainsOnlyDelegatedVaultBytes`, `TestDelegateUsesActiveProfileAndPreservesOpaqueInvocation`, and `TestFakeBackedDelegatedFailurePreservesStreamsAndExitWithoutRetry` |
| 19. Support explicit and guided favorite CRUD while incomplete non-interactive commands return contextual help | `TestFakeBackedFavoriteExplicitAndGuidedCRUD` and `TestFavoriteIncompleteCommandsDoNotPromptOutsideTerminal` |
| 20. Enforce exact tuple identity, duplicate rejection, count-first numbers with path-profile-operation ties, and note-independent ordering | `TestFavoriteSameIdentityUsesExactTupleAndIgnoresNote`, `TestServiceListSortsByRunCountBeforeIdentity`, `TestMutationServiceUpdateResetsRunCountOnlyWhenCommandChanges`, `TestFavoriteListDisplaysStableNumberedColumns`, and `TestSharedFavoriteSelectorShowsCountFirstRowsAndSearchesCounts` |
| 21. Search visible favorite fields, including run count, without an external selector executable | `TestSharedSelectorFiltersCaseInsensitiveFuzzyAcrossCompleteSearchText`, `TestSharedFavoriteSelectorUsesDeterministicSearchableRowsAndOpaqueSelection`, `TestSharedFavoriteSelectorShowsCountFirstRowsAndSearchesCounts`, `TestSharedFavoriteSelectorMapsCancellationAndContext`, `TestFakeBackedFavoriteSelectionStopsBeforeVault`, the absence of runtime `fzf` references under `cmd` and `internal`, and the sanitized Linux `bt` fuzzy-filter check above |
| 22. Map `read` and `kv-get` favorites through their stored profile without changing the active profile | `TestFakeBackedFavoriteExecutionMapsReadOperationsWithoutChangingActiveProfile` |
| 23. Preserve delegated output, streams, exit status, preflight, environment isolation, and redacted failures | `TestFakeBackedFavoriteExecutionMapsReadOperationsWithoutChangingActiveProfile`, `TestFavoriteExecutionPreservesDelegatedError`, and `TestFakeBackedDelegatedFailurePreservesStreamsAndExitWithoutRetry` |
| 24. Require approval for linked-profile cascades and preserve all state after rejection or failure | `TestFakeBackedFavoriteCascadeApprovalAndRollback`, `TestProfileRemoveWithLinkedFavoritesPromptsWithExactCount`, and the cascade service rollback suite |
| 25. Persist private atomic favorite metadata without tokens, secret values, or normal profile orphans | `TestStoreAtomicWriteFailurePreservesPriorConfiguration`, `TestStoreCreatesPrivateDirectoryAndFile`, `TestMutationServiceAddRejectsInvalidDuplicateOrMissingProfileWithoutChangingState`, and `TestFavoriteManagementSurfacesDoNotRetainVaultCanaries` |
| 26. Require a persisted per-profile opt-in before any HTTP login, preflight, or delegated execution | `TestProfileValidation`, `TestStoreLoadsLegacyHTTPSProfileWithoutInsecureOptIn`, `TestMutationServiceUpdateCanEnableAndClearHTTPOptIn`, `TestAuthenticatorLoginRejectsHTTPWithoutOptInBeforeVaultOrStore`, `TestPreflightRejectsHTTPWithoutOptInBeforeCredentialOrVaultAccess`, `TestDelegateRejectsHTTPWithoutOptInBeforeVaultDiscoveryOrCredentialAccess`, and `TestHuhProfileFormCanEnableAndClearInsecureHTTPOptIn` |
| 27. Set, clear, and validate a persisted profile color without unwanted authentication | `TestProfileHandlerUpdateClearsColorExplicitly`, `TestProfileHandlerRejectsInvalidColorBeforeMutation`, `TestHuhProfileFormValidatesAndEditsColor`, `TestMutationServiceUpdateColorWithoutCredentialAccess`, and `TestMutationServiceUpdateColorFailurePreservesProfileAndCredential` |
| 28. Scope profile colors to identity and linked favorite rows while keeping shared UI semantic | `TestProfileListPresentationPlainAndStyledAreExact`, `TestProfileShowPresentationPlainAndStyledAreExact`, `TestSavedProfileColorDoesNotTintSharedUI`, `TestSavedProfileColorOnlyMarksIdentityInListAndDetails`, `TestSavedProfileColorOnlyMarksIdentityInPickers`, `TestFavoriteListColorsRowsByTheirOwnProfiles`, `TestSharedFavoriteSelectorColorsEachRowFromItsOwnProfile`, `TestNarrowFavoriteColorStaysOnLinkedValues`, `TestSharedSelectorShowsCompactHeaderBelowSearch`, `TestInteractiveFormsAndConfirmationsUseSemanticSelection`, and `TestTerminalDisablesColorForAnyNonEmptyNoColorValue` |
| 29. Persist favorite counts and show count-first list and picker order | `TestMutationServiceRecordUseReloadsAndIncrementsCurrentFavorite`, `TestFavoriteExecutionRecordsUseAfterSuccessfulVaultRun`, `TestFavoriteExecutionSkipsUseAfterVaultFailure`, `TestConcurrentFavoriteExecutionsRecordEverySuccessfulRun`, `TestFavoriteListDisplaysStableNumberedColumns`, and `TestSharedFavoriteSelectorShowsCountFirstRowsAndSearchesCounts` |
| 30. Keep Vault output and status unchanged when a count save fails | `TestMutationServiceRecordUseRestoresStateAfterSaveFailure`, `TestFavoriteExecutionIgnoresCountSaveFailureAndPreservesStreams`, and `TestFavoriteExecutionOutputContainsOnlyDelegatedVaultBytes` |

### Bash Vault completion checkpoint

On 2026-09-21, focused completion tests, `bash -n` on the generated script, and `just check` passed. An isolated Bash smoke check with Vault v2.0.3 completed root `kv`, nested `get`, flag `-mount`, and `json` after `--profile team-a kv get -format=j`. It used no token or live Vault server.

The root and argument tests first failed before their handlers were added. `TestBashCompletionSurvivesErrexitWhenOnlyVaultMatches` then failed with exit status 1 and passed after the root helper handled `compgen` returning no management match. This checkpoint did not run a live server, native credential store, or Zsh and Fish completion.

### Local Vault completion checkpoint

On 2026-09-21, `just check` and `just build-all` passed with Fish 4.9.3 available to the test suite. Generated Bash, Zsh, and Fish scripts passed their installed-shell syntax checks. Isolated shell smoke checks used Vault v2.0.3 and returned `kv`, nested `get`, both `-m` flag matches, and `json` for `-format=j` after `--profile demo`. Fish was fetched into `/tmp` for this check; no project dependency changed.

`TestOSCompletionExecutorClearsInheritedVaultEnvironment` checks that local completion removes inherited `VAULT_*` settings, passes no command arguments, and uses an invalid address. A process trace with a fake inherited token and remote address showed no IP connection during argument completion. It showed one local `/dev/log` connection. No live Vault server, native credential store, or native macOS or Windows shell ran in this checkpoint.

### KV path filtering checkpoint

On 2026-09-21, focused profile, KV v1, KV v2, and security tests passed, followed by `just check`. In-memory Vault API checks covered active and explicit profile selection, missing tokens and keyring errors, HTTP opt-in, readable and denied leaves, and both `kv get` path forms. The checkpoint test read one existing token and made only a mount lookup, a LIST request, and a batched capability request. The token stayed out of URLs, request bodies, and candidates; no secret value endpoint, OIDC flow, token renewal, or keyring write ran.

At this checkpoint, shell path completion was not connected yet. Vault's mount lookup endpoint has no backward-compatibility guarantee, so the representative-host check remains open. No live Vault host or native keyring was used for this checkpoint.

### Vault-aware path completion checkpoint

On 2026-09-21, focused completion and security tests, `just check`, `just build-all`, direct `golangci-lint` with zero issues, and Beads preflight passed. Generated Bash, Zsh, and Fish scripts passed syntax and smoke tests with the installed shells. Fish 4.9.3 came from a temporary tool path; no project dependency changed.

Fake-backed handler tests covered `read`, KV v1 and KV v2 `kv get`, active and explicit profiles, denied leaves, readable folders, and timeout cleanup. They confirmed that completion makes only mount lookup, LIST, and capability requests. Tokens stayed out of URLs, request bodies, candidate text, and diagnostics. Shell tests confirmed that spaces and shell metacharacters remain quoted without execution. No live Vault host, native keyring, OIDC flow, or native macOS or Windows shell ran in this checkpoint. The representative-host latency and permission check remains open.

## Dependency review

The approved direct integrations are `github.com/zalando/go-keyring v0.2.6`, `charm.land/huh/v2 v2.0.3`, `charm.land/lipgloss/v2 v2.0.6`, `charm.land/bubbles/v2 v2.0.0`, and `charm.land/bubbletea/v2 v2.0.2`. The terminal boundary also imports the pinned `colorprofile`, `x/ansi`, and `x/term` support modules. The in-process selector uses Bubble Tea for the inline event loop and the Bubbles fuzzy matcher. It does not invoke or require `fzf`. `go mod verify` passed.

## Release approval

Human release approval: Approved by the project owner on 2026-09-19.

Guided CLI readiness approval: Approved by the project owner on 2026-09-19.
