# Initial release verification

Verification date: 2026-09-19

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
| 17. Generate valid Bash, Zsh, and Fish completion with commands, flags, shells, and stored profile names | `TestCompletionHandlerWritesScripts`, `TestCompletionHandlerEmitsSortedProfileNamesOnly`, `TestBashCompletionProvidesFavoriteCommandsAndValues`, `TestCompletionScriptsDescribeFavoriteCommandsAndValues`, and `TestCompletionScriptsHaveValidInstalledShellSyntax` |
| 18. Keep completion and presentation separate from Vault delegation and credentials | `TestFakeBackedCompletionNoColorAndDelegationStayIndependent`, `TestGuidedOutputsPromptsCompletionAndFailuresDoNotExposeCredentials`, `TestManagementDiagnosticRedactsCredentialAndStripsANSI`, `TestFavoriteExecutionOutputContainsOnlyDelegatedVaultBytes`, `TestDelegateUsesActiveProfileAndPreservesOpaqueInvocation`, and `TestFakeBackedDelegatedFailurePreservesStreamsAndExitWithoutRetry` |
| 19. Support explicit and guided favorite CRUD while incomplete non-interactive commands return contextual help | `TestFakeBackedFavoriteExplicitAndGuidedCRUD` and `TestFavoriteIncompleteCommandsDoNotPromptOutsideTerminal` |
| 20. Enforce exact tuple identity, duplicate rejection, stable path-profile-operation numbers, and note-independent ordering | `TestFavoriteSameIdentityUsesExactTupleAndIgnoresNote`, `TestServiceListSortsByPathProfileOperationWithoutMutatingInput`, `TestServiceListIgnoresNotesForOrdering`, and `TestMutationServiceAddRejectsInvalidDuplicateOrMissingProfileWithoutChangingState` |
| 21. Search all visible favorite fields fuzzily without an external selector executable | `TestSharedSelectorFiltersCaseInsensitiveFuzzyAcrossCompleteSearchText`, `TestSharedFavoriteSelectorUsesDeterministicSearchableRowsAndOpaqueSelection`, `TestSharedFavoriteSelectorMapsCancellationAndContext`, `TestFakeBackedFavoriteSelectionStopsBeforeVault`, the absence of runtime `fzf` references under `cmd` and `internal`, and the sanitized Linux `bt` fuzzy-filter check above |
| 22. Map `read` and `kv-get` favorites through their stored profile without changing the active profile | `TestFakeBackedFavoriteExecutionMapsReadOperationsWithoutChangingActiveProfile` |
| 23. Preserve delegated output, streams, exit status, preflight, environment isolation, and redacted failures | `TestFakeBackedFavoriteExecutionMapsReadOperationsWithoutChangingActiveProfile`, `TestFavoriteExecutionPreservesDelegatedError`, and `TestFakeBackedDelegatedFailurePreservesStreamsAndExitWithoutRetry` |
| 24. Require approval for linked-profile cascades and preserve all state after rejection or failure | `TestFakeBackedFavoriteCascadeApprovalAndRollback`, `TestProfileRemoveWithLinkedFavoritesPromptsWithExactCount`, and the cascade service rollback suite |
| 25. Persist private atomic favorite metadata without tokens, secret values, or normal profile orphans | `TestStoreAtomicWriteFailurePreservesPriorConfiguration`, `TestStoreCreatesPrivateDirectoryAndFile`, `TestMutationServiceAddRejectsInvalidDuplicateOrMissingProfileWithoutChangingState`, and `TestFavoriteManagementSurfacesDoNotRetainVaultCanaries` |
| 26. Require a persisted per-profile opt-in before any HTTP login, preflight, or delegated execution | `TestProfileValidation`, `TestStoreLoadsLegacyHTTPSProfileWithoutInsecureOptIn`, `TestMutationServiceUpdateCanEnableAndClearHTTPOptIn`, `TestAuthenticatorLoginRejectsHTTPWithoutOptInBeforeVaultOrStore`, `TestPreflightRejectsHTTPWithoutOptInBeforeCredentialOrVaultAccess`, `TestDelegateRejectsHTTPWithoutOptInBeforeVaultDiscoveryOrCredentialAccess`, and `TestHuhProfileFormCanEnableAndClearInsecureHTTPOptIn` |

## Dependency review

The approved direct integrations are `github.com/zalando/go-keyring v0.2.6`, `charm.land/huh/v2 v2.0.3`, `charm.land/lipgloss/v2 v2.0.6`, `charm.land/bubbles/v2 v2.0.0`, and `charm.land/bubbletea/v2 v2.0.2`. The terminal boundary also imports the pinned `colorprofile`, `x/ansi`, and `x/term` support modules. The in-process selector uses Bubble Tea for the inline event loop and the Bubbles fuzzy matcher. It does not invoke or require `fzf`. `go mod verify` passed.

## Release approval

Human release approval: Approved by the project owner on 2026-09-19.

Guided CLI readiness approval: Approved by the project owner on 2026-09-19.
