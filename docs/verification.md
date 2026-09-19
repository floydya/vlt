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

## Runtime verification

| Platform | Native credential store | Real OIDC and delegated read | Status |
|---|---|---|---|
| Linux | Passed against the active profile through GNOME Keyring Secret Service | OIDC login and a delegated `sys/health` read passed | Passed |
| macOS | Not run | Not run | Unverified |
| Windows | Not run | Not run | Unverified |

The Linux config directory and file use `0700` and `0600` permissions. The official Vault CLI is installed. The native credential probe read the active profile entry successfully without exposing a profile name, endpoint, token, or keyring value. The initial unavailable-or-locked result came from a sandboxed probe and was discarded. A check outside the sandbox confirmed that Secret Service is available and `vlt` can read its credential.

After VPN connection, the configured Vault health endpoint returned an HTTP response. The active credential was invalid or expired, so `vlt` completed OIDC and replaced the profile token in GNOME Keyring. A delegated `vlt read -format=json sys/health` then succeeded. The read did not change the refreshed token, and the token did not appear in profile configuration, captured standard output, or captured standard error. No raw Vault response or credential was written to this document.

Cross-compilation does not certify macOS or Windows runtime behavior.

The installed Linux `fzf` selected the expected opaque row from synthetic metadata using the same delimiter, display-field, search-field, header, prompt, and layout arguments as `vlt`. The check used no credential or Vault response. Fake-backed delegated `read PATH` and `kv get PATH` checks confirmed the one-command profile override and left the active profile unchanged. No raw secret output was recorded.

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
| 13. Present profile metadata and active state without credentials or unwanted ANSI | `TestProfileHandlerListUsesStableNumberedOrder`, `TestProfileHandlerShowPrintsOnlyProfileMetadata`, `TestProfilePresentationStylesOnlyEligibleTerminals`, and `TestFakeBackedCompletionNoColorAndDelegationStayIndependent` |
| 14. Select profiles interactively on a TTY and write canonical help outside one | `TestFakeBackedGuidedManagementFlowUsesExistingServices`, `TestFakeBackedNonTTYManagementFlowsStopBeforeServices`, `TestDispatchWritesAutomaticManagementHelpToStderr`, and `TestHuhProfileSelectorShowsSortedNamesAndPreselectsActive` |
| 15. Validate interactive add and update fields through the existing services | `TestHuhProfileFormPreservesDefaultsAndCorrectsInvalidFields`, `TestProfileHandlerUpdateUsesPopulatedReadOnlyNameForm`, and `TestFakeBackedGuidedManagementFlowUsesExistingServices` |
| 16. Preserve metadata, active selection, and credentials after cancellation or decline | `TestFakeBackedInteractiveCancellationAndDeclineLeaveStateUnchanged`, `TestProfileHandlerAddCancelAndInterruptDoNotMutate`, `TestProfileHandlerUpdateFormFailureDoesNotMutate`, and `TestProfileHandlerRemoveStopsBeforeMutationWhenNotConfirmed` |
| 17. Generate valid Bash, Zsh, and Fish completion with commands, flags, shells, and stored profile names | `TestCompletionHandlerWritesScripts`, `TestCompletionHandlerEmitsSortedProfileNamesOnly`, `TestBashCompletionProvidesFavoriteCommandsAndValues`, `TestCompletionScriptsDescribeFavoriteCommandsAndValues`, and `TestCompletionScriptsHaveValidInstalledShellSyntax` |
| 18. Keep completion and presentation separate from Vault delegation and credentials | `TestFakeBackedCompletionNoColorAndDelegationStayIndependent`, `TestGuidedOutputsPromptsCompletionAndFailuresDoNotExposeCredentials`, and `TestDelegateUsesActiveProfileAndPreservesOpaqueInvocation` |
| 19. Support explicit and guided favorite CRUD while incomplete non-interactive commands return contextual help | `TestFakeBackedFavoriteExplicitAndGuidedCRUD` and `TestFavoriteIncompleteCommandsDoNotPromptOutsideTerminal` |
| 20. Enforce exact tuple identity, duplicate rejection, stable path-profile-operation numbers, and note-independent ordering | `TestFavoriteSameIdentityUsesExactTupleAndIgnoresNote`, `TestServiceListSortsByPathProfileOperationWithoutMutatingInput`, `TestServiceListIgnoresNotesForOrdering`, and `TestMutationServiceAddRejectsInvalidDuplicateOrMissingProfileWithoutChangingState` |
| 21. Search all visible favorite fields fuzzily and fail actionably when `fzf` is unavailable | `TestFZFFavoriteSelectorUsesDeterministicSearchableRowsAndOpaqueSelection`, `TestFakeBackedFavoriteSelectionStopsBeforeVault`, and the installed Linux `fzf` check above |
| 22. Map `read` and `kv-get` favorites through their stored profile without changing the active profile | `TestFakeBackedFavoriteExecutionMapsReadOperationsWithoutChangingActiveProfile` |
| 23. Preserve delegated output, streams, exit status, preflight, environment isolation, and redacted failures | `TestFakeBackedFavoriteExecutionMapsReadOperationsWithoutChangingActiveProfile`, `TestFavoriteExecutionPreservesDelegatedError`, and `TestFakeBackedDelegatedFailurePreservesStreamsAndExitWithoutRetry` |
| 24. Require approval for linked-profile cascades and preserve all state after rejection or failure | `TestFakeBackedFavoriteCascadeApprovalAndRollback`, `TestProfileRemoveWithLinkedFavoritesPromptsWithExactCount`, and the cascade service rollback suite |
| 25. Persist private atomic favorite metadata without tokens, secret values, or normal profile orphans | `TestStoreAtomicWriteFailurePreservesPriorConfiguration`, `TestStoreCreatesPrivateDirectoryAndFile`, `TestMutationServiceAddRejectsInvalidDuplicateOrMissingProfileWithoutChangingState`, and `TestFavoriteManagementSurfacesDoNotRetainVaultCanaries` |

## Dependency review

The approved direct integrations are `github.com/zalando/go-keyring v0.2.6`, `charm.land/huh/v2 v2.0.3`, and `charm.land/lipgloss/v2 v2.0.6`. The terminal boundary also imports the pinned `colorprofile` and `x/term` support modules, while tests import `x/ansi`; these modules already belong to the approved terminal dependency graph. Bubble Tea remains indirect and application code does not import it. `go mod verify` passed.

## Release approval

Human release approval: Approved by the project owner on 2026-09-19.

Guided CLI readiness approval: Approved by the project owner on 2026-09-19.
