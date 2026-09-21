# Releasing vlt

Release Please maintains the release pull request. Merging that pull request creates the stable tag and GitHub release. The published release then opens a formula update in `floydya/homebrew-tap`.

## Version policy

- `feat:` increments the minor version.
- `fix:` increments the patch version. Use `fix(perf):` for a performance fix.
- `perf:` and `revert:` increment the patch version.
- `type!:` or a `BREAKING CHANGE:` footer increments the major version, including from `0.x` to `1.0.0`.
- Maintenance-only `build`, `chore`, `ci`, `docs`, `refactor`, `style`, and `test` commits do not independently create a release.

The first release starts at `v0.1.0`. Release Please records each published version in the manifest.

## Repository setup

Create two fine-grained personal access tokens. Restrict each token to its named repository.

1. Create the `vlt` release token with Contents, Issues, and Pull requests set to read and write.
2. Store it in the `vlt` repository as `RELEASE_PLEASE_TOKEN`.
3. Create the tap token for `floydya/homebrew-tap` with Contents and Pull requests set to read and write.
4. Store it in the `vlt` repository as `HOMEBREW_TAP_TOKEN`.

Use the repository settings or pipe token values into `gh secret set`. Never put a token on the command line, in documentation, or in shell history.

Protect the `master` branch in `vlt`. Require the `CI / Quality` check and a pull request. Block force pushes and branch deletion.

Protect the `main` branch in `floydya/homebrew-tap`. Require `Homebrew / Apple Silicon` and `Homebrew / Intel` checks. Enable auto-merge, and block force pushes and branch deletion.

## Release flow

1. Merge normal pull requests into `master` with conventional commit titles.
2. Review the Release Please pull request and its generated `CHANGELOG.md`.
   For the stable favorite ID release, state that the first favorite write upgrades `favorites.json` to version 2. A v0.2.x binary can read the file before that write but cannot read it afterward. Include the private backup and rollback limit from the README.
3. Merge the release pull request only after direct release approval.
4. Wait for the Homebrew tap pull request to pass Apple Silicon and Intel checks and auto-merge.
5. Verify the installation on macOS:

   ```console
   brew update
   brew install floydya/tap/vlt
   vlt --help
   vlt completion zsh >/dev/null
   ```

Cross-compilation and formula CI do not prove native Keychain, OIDC, or live Vault behavior. Record those checks separately with non-production credentials.

## Retry and rollback

Run the `Publish Homebrew Formula` workflow manually with the existing stable tag to retry a failed formula publication. The workflow verifies that the tag belongs to a published, non-prerelease GitHub release before changing the tap.

Failed tap checks leave the previous formula active. Fix the packaging and retry the workflow, or close and revert the formula pull request. Do not rewrite or delete a published stable tag. Publish a patch release when released source needs a code fix.
