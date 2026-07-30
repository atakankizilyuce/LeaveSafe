# Packaging

Manifests for the three package managers people are most likely to reach for on
each platform: Homebrew on macOS and Linux, Scoop and winget on Windows.

They are **generated, not hand-written**. Each carries a SHA-256 of a published
binary, and a hand-written checksum is a checksum that is wrong on the second
release.

## Generating them

The release workflow runs this on every tag and attaches the results to the
release. To do it by hand:

```bash
packaging/generate.sh v1.2.0 dist-manifests
```

It downloads each published asset, hashes what it actually got, and fills in the
templates under `templates/`. Downloading rather than hashing a local build is
the point: the manifests then describe the files users will fetch, not the ones
someone meant to publish.

Output:

| File | Goes to |
|------|---------|
| `leavesafe.rb` | `Formula/` in [`atakankizilyuce/homebrew-tap`](https://github.com/atakankizilyuce/homebrew-tap) |
| `leavesafe.json` | `bucket/` in the same repository |
| `LeaveSafe.LeaveSafe.yaml` + `.installer.yaml` + `.locale.en-US.yaml` | `microsoft/winget-pkgs`, by pull request |

## Publishing is deliberately merged

A tag push does not publish to a package manager. It asks.
[`docs/releasing.md`](../docs/releasing.md) walks the whole sequence, including
what to check at each step.

```
git push --tags
  └─ release.yml: build, sign, attest, SBOM, release, these manifests
       └─ dispatch-packages → atakankizilyuce/homebrew-tap   (stable tags only)
                                   ↓
            publish.yml there regenerates the manifests and opens a PR
                                   ↓
                  you merge it  ◄── this is the publish
                                   ↓
                      winget.yml submits to microsoft/winget-pkgs
```

**Merging that pull request is the publish.** Homebrew and Scoop users have the
new version at that moment. The tap repository's `main` is protected, so nothing
reaches users without someone reading a diff — which is the property this section
used to get by doing everything by hand.

Prereleases never reach a package manager. `dispatch-packages` skips them, and
the tap repository refuses them again rather than trusting the request.

### One repository serves Homebrew and Scoop

[`atakankizilyuce/homebrew-tap`](https://github.com/atakankizilyuce/homebrew-tap)
holds `Formula/leavesafe.rb` and `bucket/leavesafe.json`.

Homebrew requires the `homebrew-` prefix for the short `brew tap` form; Scoop
imposes no naming rule at all, so it can share a repository named for Homebrew.
That asymmetry is the whole reason for one repository rather than two.

```bash
brew tap atakankizilyuce/tap
brew install leavesafe
```

```powershell
scoop bucket add atakankizilyuce https://github.com/atakankizilyuce/homebrew-tap
scoop install leavesafe
```

Both manifests land in one commit, so the two channels cannot drift to different
versions.

### winget

winget manifests always reach users through a pull request against
`microsoft/winget-pkgs`. The merge in the tap repository starts it, submitting the
YAML files generated here — the ones the pull request was reviewed as, rather than
manifests rebuilt from scratch afterwards.

Their automated checks run SmartScreen against the installer URL, so an unsigned
binary can be held up in review. Signing is configured through the repository
secrets read by the signing steps in
[`.github/workflows/release.yml`](../.github/workflows/release.yml).

### The official channels, later

Both would remove work rather than add it, and both are currently out of reach:

- **homebrew-core** has a notability bar (a rough guide: 30 forks, 30 watchers,
  75 stars) and wants a stable, versioned release history.
- **Scoop's `main` bucket** asks for at least 500 stars and 150 forks, alongside
  being a non-GUI developer tool.

Worth revisiting. A tap works from day one.

### Doing it by hand

Still possible, and the fallback when the automation is unavailable. Run
`packaging/generate.sh <tag> dist-manifests`, then copy `leavesafe.rb` into the
tap's `Formula/`, `leavesafe.json` into its `bucket/`, and open a pull request
there. For winget, validate first:

```powershell
winget validate --manifest <dir>
winget install --manifest <dir>
```

`publish.yml` in the tap repository also takes a tag by hand, with a `dry_run`
option that generates and diffs without opening anything — which is the way to
rehearse a change to the pipeline itself.

## Changing a manifest

Edit the template in `templates/`, never the generated file — the next release
overwrites it. The placeholders are:

| Placeholder | Value |
|-------------|-------|
| `@VERSION@` | Version without the leading `v`, e.g. `1.2.0` |
| `@TAG@` | The git tag, e.g. `v1.2.0` |
| `@BASE@` | Release download URL prefix |
| `@RELEASE_DATE@` | `YYYY-MM-DD`, today unless `LEAVESAFE_RELEASE_DATE` is set |
| `@SHA_DARWIN_ARM64@` and friends | SHA-256 of each published binary |
