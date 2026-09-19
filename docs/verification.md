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

## Runtime verification

| Platform | Native credential store | Real OIDC and delegated read | Status |
|---|---|---|---|
| Linux | Passed against the active profile through GNOME Keyring Secret Service | OIDC login and a delegated `sys/health` read passed | Passed |
| macOS | Not run | Not run | Unverified |
| Windows | Not run | Not run | Unverified |

The Linux config directory and file use `0700` and `0600` permissions. The official Vault CLI is installed. The native credential probe read the active profile entry successfully without exposing a profile name, endpoint, token, or keyring value. The initial unavailable-or-locked result came from a sandboxed probe and was discarded. A check outside the sandbox confirmed that Secret Service is available and `vlt` can read its credential.

After VPN connection, the configured Vault health endpoint returned an HTTP response. The active credential was invalid or expired, so `vlt` completed OIDC and replaced the profile token in GNOME Keyring. A delegated `vlt read -format=json sys/health` then succeeded. The read did not change the refreshed token, and the token did not appear in profile configuration, captured standard output, or captured standard error. No raw Vault response or credential was written to this document.

Cross-compilation does not certify macOS or Windows runtime behavior.

## Success criteria

| Criterion | Evidence |
|---|---|
| 1. Add two profiles through OIDC and store tokens only in the native keyring | `TestFakeBackedProfileFlowUsesActiveAndOneCommandProfiles`, `TestAuthenticatorLoginInvokesVaultAndStoresClientToken`, and the Linux OIDC/keyring check above |
| 2. Display deterministic profile choices and persist name or number selection | `TestSwitchHandlerDisplaysActiveProfileAndProfileList`, `TestSwitchHandlerSelectsNamesAndNumbers`, `TestServiceListReturnsLexicographicallySortedProfiles`, and `TestStoreSetActiveProfilePersistsResolvedSelection` |
| 3. Delegate unchanged arguments with the active profile | `TestFakeBackedProfileFlowUsesActiveAndOneCommandProfiles` and `TestDelegateUsesActiveProfileAndPreservesOpaqueInvocation` |
| 4. Apply a one-command override without changing the active profile | `TestFakeBackedProfileFlowUsesActiveAndOneCommandProfiles` and `TestDelegateProfileOverrideAppliesOnceWithoutChangingActiveSelection` |
| 5. Reuse, renew, or replace credentials according to token state | `TestPreflightMakesCredentialLifecycleDecisions`, `TestFakeBackedPreflightRenewsBeforeDelegation`, and `TestFakeBackedPreflightAuthenticatesInvalidCredentialBeforeDelegation` |
| 6. Avoid OIDC on validation transport failures and avoid delegated retries | `TestPreflightNetworkValidationFailureDoesNotAuthenticate`, `TestFakeBackedPreflightStopsOnTransportFailureWithoutLogin`, and `TestFakeBackedDelegatedFailurePreservesStreamsAndExitWithoutRetry` |
| 7. Leave direct Vault behavior and the parent environment unchanged | `TestExecutorPreservesArgumentVectorAndProfileEnvironment`, `TestLoginEnvironmentSetsProfileValuesAndRemovesInheritedCredentials`, and `TestLoginEnvironmentRemovesInheritedNamespaceWhenProfileHasNone` |
| 8. Persist metadata without exposing tokens | `TestStoreRoundTrip`, `TestFakeBackedProfileFlowUsesActiveAndOneCommandProfiles`, the component security tests, and the Linux leakage check above |
| 9. Fail safely for missing dependencies, invalid data, and partial updates | `TestExecutorFailsActionablyBeforeStartingWhenVaultIsMissing`, `TestStoreContractReturnsActionableRedactedBackendFailures`, `TestAuthenticatorLoginRejectsMalformedOrMissingClientToken`, the profile validation tests, and the mutation rollback tests |
| 10. Pass automated functional and security gates | The clean-clone `just check` result above covers formatting, unit and component tests, race tests, vet, and the local build |
| 11. Cross-compile and complete a real Linux login/read flow | The clean-clone `just build-all` result and Linux runtime check above |
| 12. Provide contextual management guidance and one clear typo suggestion | `TestProfileHandlerDisplaysContextualHelpWithoutCallingServices`, `TestProfileHandlerSuggestsOneClearSubcommandTypo`, `TestManagementErrorsIncludeContextualGuidance`, and `TestDispatchWritesProfileGuidanceToStderr` |
| 13. Present profile metadata and active state without credentials or unwanted ANSI | `TestProfileHandlerListUsesStableNumberedOrder`, `TestProfileHandlerShowPrintsOnlyProfileMetadata`, `TestProfilePresentationStylesOnlyEligibleTerminals`, and `TestFakeBackedCompletionNoColorAndDelegationStayIndependent` |
| 14. Select profiles interactively on a TTY and fail promptly outside one | `TestFakeBackedGuidedManagementFlowUsesExistingServices`, `TestFakeBackedNonTTYManagementFlowsStopBeforeServices`, and `TestHuhProfileSelectorShowsSortedNamesAndPreselectsActive` |
| 15. Validate interactive add and update fields through the existing services | `TestHuhProfileFormPreservesDefaultsAndCorrectsInvalidFields`, `TestProfileHandlerUpdateUsesPopulatedReadOnlyNameForm`, and `TestFakeBackedGuidedManagementFlowUsesExistingServices` |
| 16. Preserve metadata, active selection, and credentials after cancellation or decline | `TestFakeBackedInteractiveCancellationAndDeclineLeaveStateUnchanged`, `TestProfileHandlerAddCancelAndInterruptDoNotMutate`, `TestProfileHandlerUpdateFormFailureDoesNotMutate`, and `TestProfileHandlerRemoveStopsBeforeMutationWhenNotConfirmed` |
| 17. Generate valid Bash, Zsh, and Fish completion with stored profile names | `TestCompletionHandlerWritesScripts`, `TestCompletionHandlerEmitsSortedProfileNamesOnly`, `TestCompletionScriptsDescribeOnlyVLTCommands`, and `TestCompletionScriptsHaveValidInstalledShellSyntax` |
| 18. Keep completion and presentation separate from Vault delegation and credentials | `TestFakeBackedCompletionNoColorAndDelegationStayIndependent`, `TestGuidedOutputsPromptsCompletionAndFailuresDoNotExposeCredentials`, and `TestDelegateUsesActiveProfileAndPreservesOpaqueInvocation` |

## Dependency review

The approved direct integrations are `github.com/zalando/go-keyring v0.2.6`, `charm.land/huh/v2 v2.0.3`, and `charm.land/lipgloss/v2 v2.0.6`. The terminal boundary also imports the pinned `colorprofile` and `x/term` support modules, while tests import `x/ansi`; these modules already belong to the approved terminal dependency graph. Bubble Tea remains indirect and application code does not import it. `go mod verify` passed.

## Release approval

Human release approval: Approved by the project owner on 2026-09-19.
