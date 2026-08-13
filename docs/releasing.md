# Releasing

A tag is a one-way door. This page is the order to go through it in, and what to
look at on the way — written down so the next release does not depend on
remembering how the last one went.

Nothing here is secret. The workflows, the guards and the names of the secrets
are all public by design, and this page only describes them in one place. **No
secret value appears in this repository, in a workflow log, or here.** If a
procedure below ever seems to need one written down, it is the procedure that is
wrong.

## What publishing actually is

A tag push does not publish to a package manager. It asks.

```
git push --tags
  └─ release.yml: CI gate, 5 targets, signing, attestation, SBOM, GitHub Release
       └─ packaging: manifests generated from the published assets
            └─ dispatch-packages → homebrew-tap          (tags only, stable only)
                                        ↓
                 publish.yml there regenerates them and opens a pull request
                                        ↓
                       you merge it  ◄── this is the publish
                                        ↓
                           winget.yml submits to microsoft/winget-pkgs
```

Homebrew and Scoop users get the new version when that pull request is **merged**,
not when the tag is pushed. The tap repository's `main` is protected, so nothing
can skip the review.

Installed copies hear about the release separately, through their own update
check — see [The update check](#the-update-check) below.

## Before tagging

1. **Run the hardware checklist** in [manual-verification.md](manual-verification.md)
   on real hardware, and keep the result for the release notes.

2. **Rehearse the release workflow without a tag.** Actions → Release → *Run
   workflow*, on `main`. It builds all five targets, runs the signing steps —
   which skip cleanly when no certificate is configured — and produces the
   attestation and the SBOM, uploading everything as workflow artifacts instead of
   publishing. Download at least one binary and run it.

   Two jobs cannot be rehearsed: `packaging` and `dispatch-packages` are both
   tag-gated, and `packaging` genuinely cannot run without a release to download
   the published assets from.

3. **Turn `[Unreleased]` in [CHANGELOG.md](../CHANGELOG.md) into the version
   heading.**

4. **Decide whether this is a prerelease.** A tag carrying a suffix — `v1.1.0-rc1`,
   `v1.1.0-beta` — is treated as one everywhere: `softprops/action-gh-release`
   marks the GitHub release as a prerelease, `dispatch-packages` skips itself, and
   the tap repository would refuse the request anyway. Only users who have opted
   into `"update_channel": "beta"` are told about it.

   A beta must never become what `brew install leavesafe` produces, which is why
   this is enforced in three places rather than remembered in one.

## Tagging

```bash
git tag v1.1.0
git push --tags
```

Then watch the run:

| Watch | Because |
| ----- | ------- |
| The CI gate | It runs first; nothing is published if it fails |
| The signing steps | With no certificate configured they write an **Unsigned build** warning to the job summary and carry on. That is by design, but it is worth knowing which releases are unsigned |
| `packaging` | It downloads every published asset and hashes what it actually got. A failure here usually means it ran before an upload finished; it retries three times |
| **`dispatch-packages`** | **A missing or expired `TAP_DISPATCH_TOKEN` makes this warn and exit 0 rather than fail.** A release should not be marked broken because a downstream publish could not be *started* — but it means the step can pass quietly without having done anything. Read its log |

If the dispatch did not fire, run `publish.yml` in the tap repository by hand with
the tag. Nothing is lost.

## Publishing

1. A pull request appears in
   [atakankizilyuce/homebrew-tap](https://github.com/atakankizilyuce/homebrew-tap).
   **Read the diff.** Check that the version matches the tag, that the download
   URLs point at this release, and that the checksums were produced by this run
   rather than carried over from the last one.

2. Merge it. Homebrew and Scoop users have the new version at that moment, and the
   merge starts the winget submission against `microsoft/winget-pkgs`, which
   Microsoft reviews on their own schedule.

   Their automated checks run SmartScreen against the installer URL, so an
   unsigned build can be held up in review.

3. **Confirm the packages install**, which is the only check that proves the
   manifests are right:

   ```bash
   brew tap atakankizilyuce/tap
   brew install leavesafe
   ```

   ```powershell
   scoop bucket add atakankizilyuce https://github.com/atakankizilyuce/homebrew-tap
   scoop install leavesafe
   ```

   Without a Mac to hand, `brew style` and `brew audit --strict` on the formula
   catch most of what would go wrong.

## The update check

Installed copies ask GitHub whether a newer release exists — once a day, on the
channel they are configured for — and report it on the dashboard and to the paired
phone, with the upgrade command for however that copy was installed. Nothing is
downloaded and nothing is replaced. [SECURITY.md](../SECURITY.md) sets out exactly
what the check discloses.

Two consequences worth holding on to:

- **A prerelease notifies nobody on the stable channel.** That is correct
  behaviour, not a bug, and it is why the stable tag matters.
- **A copy can only be notified by a version that already carries this code.**
  Anyone running an older build hears nothing through this mechanism, however many
  releases are published. The first announcement to those users has to be made by
  hand, through the releases page and the README. Every updater works this way.

### Testing the update path without publishing anything

The whole mechanism can be exercised against releases that already exist. Build a
binary stamped with an older version and put it on the beta channel:

```bash
go build -ldflags "-X main.version=v1.0.0" -o leavesafe-test ./cmd/leavesafe
```

> **A binary stamped with a version it is not must never be distributed.** It
> would tell its own user the wrong thing about what they are running, and that is
> the one claim this program has to get right. Build it, test with it, delete it.

Point a throwaway config directory at the beta channel — `APPDATA` on Windows,
`HOME` elsewhere — with:

```json
{ "update_channel": "beta" }
```

Run it and type `update` at the dashboard. Expect:

```
Checking for updates on the beta channel…
[UPDATE] v1.0.4-beta is available — you are running v1.0.0
```

That one command exercises the GitHub query, the channel filter, the version
comparison, the validation of everything the endpoint returned, the write to
`update.json`, and the message sent to any paired phone.

Two things that will waste your afternoon otherwise:

- Leaving `language` out of the config makes the first run ask about it on
  standard input, and whatever you typed becomes the answer to that question.
- `Set-Content -Encoding utf8` in Windows PowerShell writes a byte-order mark, and
  the config then fails to parse. Use `[System.IO.File]::WriteAllText`.

**Install-channel detection** can be checked without installing anything. Copy the
binary into a path shaped like a package manager's and run it from there:

```powershell
mkdir "$env:TEMP\scoop\apps\leavesafe\current"
copy leavesafe-test.exe "$env:TEMP\scoop\apps\leavesafe\current\"
& "$env:TEMP\scoop\apps\leavesafe\current\leavesafe-test.exe"
```

`update` should now offer `scoop update leavesafe` instead of the releases URL.

Finally, set `"update_channel": "stable"` and check the same binary is told
nothing while only prereleases exist — the silence is the feature working.

## What this pipeline does not protect against

Worth being clear about, so nobody relies on a guarantee that is not here:

- **It does not prove the release is good.** It proves the artifacts came from this
  workflow at this commit. Whether the code works is what CI and
  [manual-verification.md](manual-verification.md) are for.
- **An unsigned build is unsigned.** The attestation says which workflow built the
  file; it is not a code-signing certificate and Gatekeeper and SmartScreen do not
  read it.
- **Someone who can push a tag can start a release.** The gate is on publishing to
  a package manager, not on tagging. Branch protection on `main` is what stands
  between a bad commit and a release.
- **Deleting a published release does not unpublish it.** Anyone who already
  fetched the binary has it, and a package manager that already recorded the
  version keeps the URL. Yanking is a new release, not a deletion.
