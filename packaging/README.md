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
| `leavesafe.rb` | A Homebrew tap |
| `leavesafe.json` | A Scoop bucket |
| `LeaveSafe.LeaveSafe.yaml` + `.installer.yaml` + `.locale.en-US.yaml` | `microsoft/winget-pkgs` |

## Publishing is deliberately manual

Nothing here pushes to a tap, a bucket or winget. Publishing to a package
manager is a decision with its own review — and wiring it into the tag push
would make every tag, including a tag pushed by mistake, a publish.

### Homebrew

Needs a tap repository, conventionally `atakankizilyuce/homebrew-tap`. Copy
`leavesafe.rb` into its `Formula/` directory and push. Users then run:

```bash
brew tap atakankizilyuce/tap
brew install leavesafe
```

Submitting to homebrew-core instead has a notability bar (a rough guide: 30
forks, 30 watchers, 75 stars) and requires a stable, versioned release history.
A tap works from day one.

### Scoop

Needs a bucket repository, conventionally `atakankizilyuce/scoop-bucket`. Copy
`leavesafe.json` into its `bucket/` directory and push. Users then run:

```powershell
scoop bucket add leavesafe https://github.com/atakankizilyuce/scoop-bucket
scoop install leavesafe
```

The manifest carries `checkver` and `autoupdate`, so once the bucket exists
Scoop's own tooling can raise the version bumps.

### winget

Fork `microsoft/winget-pkgs`, put the three YAML files under
`manifests/l/LeaveSafe/LeaveSafe/<version>/`, and open a pull request. Validate
first:

```powershell
winget validate --manifest manifests/l/LeaveSafe/LeaveSafe/1.2.0
winget install --manifest manifests/l/LeaveSafe/LeaveSafe/1.2.0
```

Their automated checks run SmartScreen against the installer URL, so an unsigned
binary can be held up in review. See [`docs/release-signing.md`](../docs/release-signing.md).

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
