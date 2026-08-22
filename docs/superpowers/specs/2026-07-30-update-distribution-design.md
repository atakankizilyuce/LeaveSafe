# Update signalling and package-manager distribution — design

**Date:** 2026-07-30
**Repository:** LeaveSafe (`C:\workSpace\LeaveSafe`), branch `main` at `3856210`
**Status:** approved, ready for an implementation plan
**Location note:** this spec sits in the working tree at `docs/superpowers/specs/` but is
**not** in the repository — `.gitignore:429` excludes `docs/superpowers/`, matching the
decision in commit `e5ad12a` ("Stop carrying the plans and specs a session produced"). It
therefore survives between sessions without ever being committed. Do not `git add -f` it.
Written in English to match the code and comments it describes.

---

## 1 · Goal

When a new release tag is published, every installed copy of LeaveSafe should learn about
it and be told how to upgrade — on the terminal dashboard and on the paired phone — and the
Homebrew, Scoop and winget channels should receive the new version without hand-copying
files.

### What this is not

A **push** to installed users is impossible and will not be attempted. LeaveSafe has no
accounts, no server and no telemetry, so there is no list of installations to push to. This
is not a gap to close: every production desktop updater — Sparkle and WinSparkle appcasts,
Chrome's Omaha, Squirrel, the Tauri updater, and package managers themselves — is pull-based,
with the client polling a feed. The target is therefore: *every installed copy checks often
enough, and says so where the user actually looks.*

The binary will not download, verify, replace or restart itself. The position recorded in
`internal/update/update.go:9-12` stands:

> Automatic updates would mean a security program silently replacing itself from the
> network, which is a larger trust decision than the user made when they downloaded a
> single file.

Because nothing is downloaded, artifact attestation verification is out of scope.

---

## 2 · Starting point

| Piece | Location | State today |
|---|---|---|
| GitHub "is there a newer release" query | `internal/update/update.go` | Works, tested, semver-ish comparison, exempts `dev` builds |
| Where it is called | `cmd/leavesafe/main.go:579-581` | **Once at startup only**, when `update_check` is on |
| Where the notice goes | `main.go:722-742` → `sb.writeLine` | A TUI line; in headless mode a `log.Info` into `leavesafe.log` (`main.go:286-292`) |
| Homebrew / Scoop / winget manifests | `packaging/templates/` + `packaging/generate.sh` | Generated on tag, attached to the release; **publishing is manual** (`packaging/README.md:32`) |
| Version on the phone | `ws.NewHub(authMgr, sensorMgr, version)` (`main.go:430`), `app.tsx:355` | Footer already reads `LeaveSafe <version> · no cloud`; no update surface |

### Three gaps

1. **The check runs only at startup.** A copy installed with `leavesafe install-service` and
   running for weeks never asks again — so the longest-running installations, the ones most
   in need of a fix, are the least likely to hear about one.
2. **The notice lands where nobody looks.** In headless mode it is one `log.Info` line in
   `leavesafe.log`. The phone — the only screen the user carries, and one already holding a
   live WebSocket — is never told.
3. **A tag never reaches the package managers.** `brew upgrade` / `scoop status` /
   `winget upgrade` would give both notification and installation for free, but the manifest
   publish is manual, so a new tag does not reach those channels at all.

### The blocking defect

**The update check has never been able to fire.** All five published releases are
prereleases (`v1.0.0-beta` … `v1.0.4-beta`), and they are excluded twice over:

- `update.go:26` uses `/releases/latest`, which by definition never returns a prerelease.
- `update.go:104` — `if rel.Draft || rel.Prerelease || rel.TagName == ""` → empty result.

`update_check` defaults to on and the code is correct and tested, but no user has ever
received a notification, and cutting another `-beta` tag would not change that.
`.github/workflows/release.yml:4-6` triggers on tag push and `softprops/action-gh-release`
marks the release as a prerelease automatically from the tag name — that is the source of
the condition.

Separately, when this design began **neither `atakankizilyuce/homebrew-tap` nor
`atakankizilyuce/scoop-bucket` existed**, so `packaging/README.md` named two conventional
targets that manifests were produced for and had nowhere to go. §6.1 settles where they belong;
`homebrew-tap` has since been created (public, empty) and `scoop-bucket` is no longer wanted.

---

## 3 · Decisions

| # | Decision | Rationale |
|---|---|---|
| D1 | Notify and hand off to the package manager. The binary never replaces itself. | Keeps the `update.go:9-12` position intact; the package manager already solves installation properly. |
| D2 | Notify on **both** the terminal dashboard and the phone. | Headless is the recommended long-term setup and its terminal notice goes to a log file nobody reads. |
| D3 | Detect the install channel at runtime from the executable path, with a generic fallback. | `generate.sh` hashes the *published* release binary (`packaging/README.md:18-22`), so all channels ship a byte-identical file and a link-time channel stamp is impossible without per-channel artifacts. |
| D4 | Add release channels: `stable` (default) and `beta`. | Fixes the blocking defect properly and is what production updaters do. Until a non-prerelease exists, stable-channel users correctly hear nothing. |
| D5 | Publishing stays a deliberate human decision, expressed as merging a pull request. | Preserves the property stated in `release.yml:315-317` — a tag push must not become a publish. See D11 for how the work is automated around that gate. |
| D6 | Phone surface: the existing footer version readout gains a dot and becomes tappable; detail lives in a Settings section. | Never competes with the annunciator panel or the alarm overlay; needs no new top-level component. |
| D7 | Persist check bookkeeping in its own `update.json`, not in `state.json`. | `internal/state` documents itself (`state.go:1-12`) as being about the armed-state gap; update bookkeeping would blur a well-bounded unit. |
| D8 | The release channel is selectable from the phone. | `README.md` promises every setting is changeable from the phone UI; consistency wins over making beta deliberately awkward. |
| D9 | One new repository, `atakankizilyuce/homebrew-tap`, holds both the Homebrew formula and the Scoop manifest. | See §6.1. Homebrew's one-argument `brew tap` requires a `homebrew-` prefixed repo; Scoop imposes no naming rule, so both can share one. |
| D10 | No auto-update Action in that repository. | Scoop's `checkver`/`autoupdate` automation would publish on every stable release without being asked, defeating D5, and would race the publish workflow for the same file. |
| D11 | The publish workflow lives in the tap repository, is triggered by a `repository_dispatch` from `release.yml`, and ends by opening a pull request rather than pushing. | The work becomes automatic while the decision stays a merge button, so D5 survives. Packaging logic sits with packaging, and the file writes use the tap repository's own `GITHUB_TOKEN`. Not a reduction in PAT power — see §6.3. |
| D12 | Submodules are not used. | A submodule pins a commit; it does not trigger, notify or synchronise versions. The manifests need no source code at all — `generate.sh` derives everything from the published release assets — so there is nothing to share. |
| D13 | The tap repository is the single publish control plane, and is designed to serve future applications too. | A tap holds many formulae and a bucket many manifests; users tap once and receive everything ever shipped. See §6.4. |
| D14 | The winget package identifier stays `LeaveSafe.LeaveSafe`. | The author's decision. The `AppName.AppName` shape is well precedented in `winget-pkgs` — `Git.Git`, `7zip.7zip`, `Notepad++.Notepad++` — so no template change is needed. Future applications will each open their own publisher namespace. |

---

## 4 · Architecture

Four units, each independently testable.

### 4.1 · `internal/update/update.go` — the query (existing, extended)

`Checker` gains a `Channel` field. The endpoint changes from `/releases/latest` to
`/repos/atakankizilyuce/LeaveSafe/releases?per_page=30`, because `/releases/latest` cannot
express a channel at all — it never returns a prerelease. The list arrives newest-first;
walk it and take the first release that:

- is not a draft (always skipped), and
- is not a prerelease, unless `Channel == "beta"`, and
- has a `tag_name` passing the validation in §7, and
- compares newer than the running version.

Do not paginate: one page of 30 is more history than a decision about "is there something
newer" needs.

`isRelease` (`update.go:116-125`) is unchanged — builds from source are never told they are
out of date.

**`compareVersions` must be corrected.** `versionParts` (`update.go:157-159`) cuts the
string at `-`, so today `v1.0.4-beta` and `v1.0.4` compare as **equal**. On the beta channel
that is a real defect: stable `v1.0.4` *is* newer than `v1.0.4-beta`, and a beta user would
never be told. Add semver precedence — a version carrying a prerelease suffix sorts
**before** the same numeric version without one. Suffixes are not ordered against each other
beyond that; `-beta` versus `-rc1` at the same numeric version is treated as equal, because
nothing in this design depends on that ordering and an untested subtlety is worse than an
admitted limit. Record that limit in the doc comment, replacing the current note at
`update.go:130-134`, which describes behaviour this change supersedes.

`Result` gains nothing. The command string is not its business (see §4.2).

### 4.2 · `internal/update/channel.go` — install-channel detection (new)

```go
// Method is how this copy of LeaveSafe was installed.
type Method int

const (
    MethodUnknown Method = iota
    MethodHomebrew
    MethodScoop
    MethodWinget
)

// Detect reports the install method implied by an executable path.
func Detect(exePath string) Method

// Command returns the upgrade command for a method, or "" for MethodUnknown.
func (m Method) Command() string
```

`Detect` is **pure path inspection and execs nothing**. A security tool should not shell out
to work out how it was installed, and a pure function is table-testable.

| Method | Path segment matched | Command |
|---|---|---|
| `MethodHomebrew` | `/Cellar/leavesafe/` | `brew upgrade leavesafe` |
| `MethodScoop` | `\scoop\apps\leavesafe\` | `scoop update leavesafe` |
| `MethodWinget` | `\Microsoft\WinGet\Packages\` | `winget upgrade LeaveSafe.LeaveSafe` |
| `MethodUnknown` | — | `""` — caller falls back to the releases URL |

Matching the `Cellar` segment covers all three Homebrew prefixes at once — `/opt/homebrew`,
`/usr/local`, and Linuxbrew's `/home/linuxbrew/.linuxbrew` — without hardcoding any of them.
Comparison is case-insensitive and normalises both separators, so a Windows path found with
forward slashes still matches.

The caller resolves the path before asking:

```go
exe, err := os.Executable()
if err == nil {
    if resolved, err := filepath.EvalSymlinks(exe); err == nil {
        exe = resolved
    }
}
method := update.Detect(exe)
```

`EvalSymlinks` matters for Homebrew, which puts a symlink in `bin/` and the real file under
`Cellar/`. A failure at either step degrades to `MethodUnknown`, which shows the releases
URL — correct rather than wrong.

Installing as a service does not change this: the service entry points at wherever the
binary already lives, so detection still sees the real path.

### 4.3 · `internal/update/ledger.go` — check bookkeeping (new)

A small store over `update.json` in the config directory, following the shape of
`internal/state`:

```go
type Record struct {
    LastCheck      time.Time `json:"last_check"`
    LastSuccess    time.Time `json:"last_success"`
    LastSeenLatest string    `json:"last_seen_latest,omitempty"`
}

type Ledger struct { /* mutex + path */ }

func NewLedger(dir string) *Ledger
func (l *Ledger) Load() (Record, error)
func (l *Ledger) Save(Record) error
```

Persistence exists so that a service copy in a crash loop does not query GitHub on every
start (the unauthenticated API limit is 60 requests/hour/IP) and so restarts do not reset
the schedule.

A missing file means "never checked", exactly as in `state.Load` (`state.go:54-67`). A file
that does not parse is treated as missing rather than as an error: the worst case is one
extra check, and there is nothing here worth preserving by moving the file aside the way
`config` does.

### 4.4 · The scheduler — `cmd/leavesafe/main.go`

Replace the one-shot at `main.go:579-581`:

```go
if cfg.UpdateCheckEnabled() {
    safe.Go("update-check", func() { runUpdateChecks(ctx, sb, hub, cfg, ledger) })
}
```

Behaviour:

- On start, check only if `LastCheck` is older than the configured interval. Otherwise wait
  out the remainder.
- Then a ticker at the configured interval (default 24h).
- A small random jitter (up to 10% of the interval) before the first check, so installations
  do not synchronise on a wall-clock moment. Both Omaha and Sparkle do this.
- On failure, back off to one hour rather than a full interval — a transient GitHub outage
  should not cost a whole day, and a persistent one must not become a hammer. Write
  `LastCheck` on failure too; only success advances `LastSuccess`.
- Report only when the found version differs from `LastSeenLatest`, then record it, so a
  user who has decided to stay on the current version is told once per new release rather
  than once per interval. The TUI line and the phone message both derive from the same
  result, so they never disagree.

`safe.Go` already supervises panics, matching how the sensor loops are run.

The check remains entirely off the path that brings monitoring up: the 6-second request
timeout (`update.go:31`) stays, and nothing here can delay arming.

---

## 5 · Surfaces

### 5.1 · Terminal dashboard

`reportAvailableUpdate` (`main.go:722-742`) keeps its shape and gains the channel-specific
command:

```
  [UPDATE] v1.3.0 is available — you are running v1.2.0
           brew upgrade leavesafe
           Set "update_check": false in the config to stop checking.
```

With `MethodUnknown` the command line is replaced by the release URL, which is what the code
prints today. In headless mode this still goes to `leavesafe.log` via `writeLine`
(`main.go:286-292`) — unchanged, and no longer the only surface.

A new dashboard command `update` checks on demand and prints the result, including "you are
on the newest release" — which the startup path deliberately never says. It joins the
existing table (`test`, `trigger`, `stop`, `history`, `urls`, `qr`, `cert`, `rotate-key`,
`help`) and must be added to `help` and to the command table in `README.md`.

Being explicitly asked, it ignores both gates the scheduled path respects: it queries
regardless of when `LastCheck` last advanced, and it reports regardless of
`LastSeenLatest`. It still writes both fields on completion, so an on-demand check resets
the schedule rather than running alongside it. It does not bypass the failure backoff for
the scheduled loop — a manual check that fails prints the error and leaves the loop's
timing alone.

### 5.2 · Phone

A new server message type, `update_available`:

```go
MsgTypeUpdateAvailable = "update_available"

// UpdatePayload tells the phone about a newer release. The command is built on
// the laptop, which is the only side that knows how this copy was installed.
type UpdatePayload struct {
    Running string `json:"running"`
    Latest  string `json:"latest"`
    URL     string `json:"url"`
    Channel string `json:"channel"`
    Command string `json:"command,omitempty"`
}
```

Added to `ServerMessage` as `Update *UpdatePayload` alongside the existing optional payloads
(`messages.go:99-115`).

Sent in two situations:

1. Immediately after `auth_ok`, when a result is already known — so a phone that pairs hours
   after the check still learns about it.
2. Broadcast to authenticated clients when a periodic check finds something new.

The hub holds the latest result. It already receives the running version
(`ws.NewHub(..., version)`), so `Running` needs no new plumbing.

**The command string is produced on the laptop.** The phone renders it and never derives it,
because the laptop is the only side that knows the install path.

**UI.** The footer at `app.tsx:355` gains a dot and becomes a button that opens
`SettingsSheet` scrolled to a new Updates section:

```
┌──────────────────────────┐
│ LeaveSafe        ⟳   ⚙ │
│                        │
│      STANDBY           │
│  [■][■][■][■][■][■]   │
│                        │
│  LeaveSafe v1.2.0 •    │  ← dot: an update exists
│  no cloud              │     tap → Settings › Updates
└──────────────────────────┘

Settings › Updates
  Channel      stable ▾
  Running      v1.2.0
  Available    v1.3.0
  brew upgrade leavesafe   [copy]
```

The alarm path is untouched: `AlarmOverlay` still sits above everything, the `data-armed`
root attribute driving every colour (`app.tsx:93-95`) is unaffected, and no update state
alters the annunciator. When no update is known the footer renders exactly as it does today.

`Copy` uses `navigator.clipboard` with a graceful no-op when unavailable — the command is
also selectable as text, so failure costs nothing.

### 5.3 · Configuration

Three fields, following the conventions already in `internal/config/config.go`:

| Field | JSON | Type | Default | Notes |
|---|---|---|---|---|
| `UpdateCheck` | `update_check` | `*bool` | nil = on | Exists today (`config.go:91`), unchanged |
| `UpdateChannel` | `update_channel` | `string` | `""` = stable | Validated like `ConnectionMode` (`config.go:254-261`) |
| `UpdateCheckHours` | `update_check_hours` | `int` | `0` = 24 | Clamped only when non-zero, like `Location.PollSeconds` (`config.go:250-252`) |

```go
clampInt("update_check_hours", &c.UpdateCheckHours, 6, 168, 24)  // guarded by != 0
```

A six-hour floor protects GitHub's rate limit from a hand-edited config; a one-week ceiling
keeps the feature meaningful. Both adjustments are reported through the existing `notes`
mechanism, so `Validate` tells the user what it changed.

Accessors mirror `UpdateCheckEnabled` (`config.go:145-150`), so callers never interpret the
zero values themselves:

```go
func (c *Config) UpdateChannelName() string       // "" → "stable"
func (c *Config) UpdateCheckInterval() time.Duration // 0 → 24h
```

An unrecognised `update_channel` is reset to `stable` with a note, rather than refusing to
start — a typo must not stop a security monitor.

All three are added to `ConfigPayload` (`messages.go:118-134`), which carries none of them
today. Without that the phone cannot show or change the channel, and D8 requires it. Writes
arrive over the existing `update_config` message (`messages.go:22`).

Changing the channel takes effect on the next scheduled check; it does not force one. The
`update` dashboard command and re-pairing are the ways to see a result sooner. Changing it
clears `LastSeenLatest`, so switching to beta reports the newest beta even if a stable
result was already reported at that version.

---

## 6 · Publishing

### 6.1 · Where the manifests live

One new repository, `atakankizilyuce/homebrew-tap`, serves both Homebrew and Scoop:

```
atakankizilyuce/homebrew-tap
  Formula/leavesafe.rb          ← Homebrew
  bucket/leavesafe.json         ← Scoop
```

```bash
brew tap atakankizilyuce/tap
brew install leavesafe

scoop bucket add atakankizilyuce https://github.com/atakankizilyuce/homebrew-tap
scoop install leavesafe
```

The constraints behind this, each verified 2026-07-30:

- **winget needs no repository of ours, ever.** Manifests always reach users through a pull
  request to `microsoft/winget-pkgs`.
- **Scoop imposes no naming rule.** Any git repository can be a bucket, with manifests in a
  `bucket/` subdirectory or at the root. So Scoop can share a repository named for Homebrew.
- **Homebrew does impose one.** The one-argument `brew tap user/repo` resolves to
  `github.com/user/homebrew-repo`, so the `homebrew-` prefix is mandatory for the short form.
  The two-argument form `brew tap <name> <URL>` accepts any URL, but it is a form almost
  nobody uses, and putting it in `README.md` would make the install instructions
  unrecognisable.

That asymmetry is why one shared repository must carry Homebrew's required name. The cost is
cosmetic: Scoop users see `homebrew-tap` in the bucket URL. `packaging/README.md:41` already
names `atakankizilyuce/homebrew-tap` as the conventional tap and `README.md` already shows
`brew tap atakankizilyuce/tap`, so neither string changes.

**Why not the main repository.** A tap or bucket is a clone target that every user refreshes
on every `brew update` / `scoop update`. Putting it in `LeaveSafe` would make each user pull
the application's whole history — 14.3 MB today and growing — turn every application commit
into bucket churn, and make the publish workflow commit to `main` during a release. Separate
small repositories are also the dominant industry pattern: GoReleaser, the de facto standard
for releasing Go projects, creates separate `homebrew-tap` and `scoop-bucket` repositories by
default, and ScoopInstaller publishes a `BucketTemplate` repository for exactly this purpose.

**Why not the official channels, which would need no repository at all.** Both are closed for
now. Scoop's `main` bucket requires at least 500 stars and 150 forks alongside being a
non-GUI developer tool; LeaveSafe meets the second condition and not the first. `homebrew-core`
has the notability bar already recorded in `packaging/README.md:48-50` plus a stable, versioned
release history. winget has no such bar and is reachable today. Revisit both later; reaching
either removes work rather than adding it.

**No auto-update Action in the tap repository.** Scoop's `checkver`/`autoupdate` automation
(historically the Excavator container, now a GitHub Action from `BucketTemplate`) would bump
the manifest whenever a new stable release appeared, publishing without anyone asking — which
is precisely what D5 exists to prevent — and would race `publish.yml` for the same file.
Two mechanisms writing one manifest is a defect waiting to happen. The `checkver` and
`autoupdate` blocks stay in `packaging/templates/leavesafe.scoop.json.in:22-34` because they
are inert without such an Action, cost nothing, and are what Scoop's own official automation
would use if the manifest ever moves into `main`.

### 6.2 · How a publish is driven

The publish workflow lives in the **tap repository**, not in `LeaveSafe`, and the merge of a
pull request — not the running of a workflow — is the moment of publication.

```
LeaveSafe: git push --tags
  └─ release.yml   (existing: CI gate, 5 targets, signing, attestation, SBOM, release,
     │              packaging job attaching manifests)
     └─ new final job: repository_dispatch → homebrew-tap        (stable tags only)
                                        │
                                        ▼
homebrew-tap: publish.yml
  ├─ check out atakankizilyuce/LeaveSafe at the tag   (public; no token needed)
  ├─ packaging/generate.sh <tag> dist-manifests       (downloads assets, hashes what it got)
  ├─ write Formula/leavesafe.rb and bucket/leavesafe.json
  └─ open a pull request                          ◄── merging this is the publish decision
       └─ on push to main: wingetcreate ... --submit
```

**In `LeaveSafe`,** one job is added to the end of `release.yml`: after the release and the
existing `packaging` job succeed, it sends a `repository_dispatch` carrying the tag. It is
guarded twice — it runs only for tag refs, and only when the version has no `-` suffix, so a
rehearsal (`release.yml:13`) and a prerelease both stop here. A beta must never become what
`brew install leavesafe` produces.

This does modify `release.yml`, which an earlier draft of this design said it would not. The
property that mattered is nevertheless kept: `release.yml:315-317` asserts that a tag push must
not become a publish, and it still does not — the dispatch produces a pull request awaiting a
human, which is what D5 now says in those terms. The comment at `release.yml:312-321` must be
updated to describe this, since it currently states that nothing is pushed anywhere.

**In the tap repository,** `publish.yml` accepts both `repository_dispatch` and
`workflow_dispatch` with a `tag` input, so a publish can also be started by hand when a
dispatch was missed or a manifest needs regenerating. It re-checks the prerelease guard rather
than trusting the caller: a `workflow_dispatch` has no upstream guard behind it, and a
`repository_dispatch` payload is only as trustworthy as whoever holds the PAT.

`generate.sh` stays in `LeaveSafe`, where it is documented and reviewed, and is reached by
checking that repository out. It remains the single source of truth: it downloads the published
assets and hashes what it actually got, so the manifests describe the files users will fetch
and the shipped checksums get re-verified in passing.

The pull request is opened with the tap repository's own `GITHUB_TOKEN`. Because both manifests
land in one commit, Homebrew and Scoop can never drift to different versions — nothing can
update one without the other.

**winget** runs on `push:` to the tap repository's `main`, i.e. after the merge:
`wingetcreate update LeaveSafe.LeaveSafe --version <v> --urls <assets> --submit`, which forks
`microsoft/winget-pkgs` and opens the pull request there. winget cannot be pushed to directly;
a PR is the only route. Hanging it off the merge makes one action the go-ahead for all three
channels.

**Rehearsal.** `workflow_dispatch` carries a `dry_run` input: manifests are generated, printed
and diffed against what is in the repository, and no pull request is opened. This is the
rehearsal philosophy of `release.yml:7-12` applied to the one step that could not previously be
rehearsed at all. Run it once against a real tag before the first live publish.

### 6.4 · Room for a second application

A Homebrew tap holds many formulae and a Scoop bucket many manifests — `homebrew-core` holds
thousands in one repository. So the tap serves every application published from this account,
and a user who runs `brew tap atakankizilyuce/tap` once receives all of them:

```
homebrew-tap/
  Formula/leavesafe.rb      Formula/otherapp.rb
  bucket/leavesafe.json     bucket/otherapp.json
```

The repository name is deliberately application-neutral, which is what makes this work.

The layout needs nothing to accommodate a second application; the **workflow** does. Rather
than build a generic pipeline now for one application — which would be speculative — write
`publish.yml` for LeaveSafe, but define the dispatch payload as `{app, repo, tag}` from the
outset. The payload is the contract between two repositories and is the one part that is
expensive to change later; the workflow's internals are not. A second application then becomes
an edit inside one workflow instead of a redesign of the interface.

The implied contract, which LeaveSafe already satisfies: *any repository dispatching here
provides `packaging/generate.sh <tag> <outdir>`, emitting one `.rb` and one `.json`.* Record it
in the tap repository's `README.md` so the next application has something to conform to.

Note that each application opens its own winget publisher namespace under D14
(`LeaveSafe.LeaveSafe`, then `OtherApp.OtherApp`), so winget package identifiers are per
application and carry no shared prefix. Nothing in this design depends on them being related.

**Protect `main` in the tap repository** and disallow direct pushes. Since publication is
defined as merging a pull request, an unprotected `main` leaves that a habit rather than a
rule — and `publish.yml` opens pull requests regardless, so protection costs nothing.

### 6.3 · Prerequisites, outside the code change

- ~~Create `atakankizilyuce/homebrew-tap`~~ — **done 2026-07-30**: exists, public, empty, no
  default branch yet. Still needs its first commit: `Formula/` and `bucket/` directories, and a
  `README.md` covering that it serves both Homebrew and Scoop (a Scoop user landing on a repo
  named `homebrew-tap` deserves an explanation on arrival) and the §6.4 contract.
- Protect `main` there once it exists: no direct pushes, merges via pull request.

  It has to be public because `brew tap` and `scoop bucket add` run an anonymous `git clone`
  on the user's machine: this is a distribution endpoint, not an artifact store. Nothing in it
  is sensitive — a formula and a manifest carry only the version, download URLs and SHA-256s
  already public on the releases page. The one consequence to respect is that **workflow logs
  in a public repository are public**, so `publish.yml` must never echo a token; pass the
  winget PAT to `wingetcreate` through the environment, not as a command-line argument.
- Allow GitHub Actions to open pull requests in that repository (Settings → Actions → Workflow
  permissions), so `publish.yml` can do so with `GITHUB_TOKEN`.
- `TAP_DISPATCH_TOKEN` on `LeaveSafe`: a fine-grained PAT scoped to `homebrew-tap` alone, with
  **Contents: Read and write**, for the `repository_dispatch` call.
- `WINGET_TOKEN` on the tap repository: a classic PAT with the `public_repo` scope, which is
  what `wingetcreate` needs to fork `microsoft/winget-pkgs` and push to that fork.

Both are set with `gh secret set <NAME> --repo <owner/repo>` and no value argument, so the
token is typed into a hidden prompt and never reaches a file, a shell history entry or a
command line. Workflows must read them through `env:` rather than interpolating them into a
`run:` string, and must never echo them — see §7 on public workflow logs.

Note that no secrets are configured on `LeaveSafe` today, so the signing steps take their
documented unsigned path (`release.yml:124-132`) and every published binary is currently
unsigned. That is graceful, not broken, but it bears on winget: `packaging/README.md:76-79`
records that an unsigned installer can be held up in winget review by SmartScreen. Signing is
out of scope here and worth settling before the first winget submission.

**On the PAT.** `POST /repos/{owner}/{repo}/dispatches` requires Contents: write on the target
— the same permission a direct push would need. Moving the workflow into the tap repository
therefore does *not* reduce the power of the token `LeaveSafe` holds; it narrows what that
token is used *for* (triggering, not writing) and keeps the file writes under a scoped
`GITHUB_TOKEN`. The organisational gain is real; the permission gain is not, and this design
does not claim it.

A `schedule:` cron in the tap repository would need no PAT at all, but GitHub disables
scheduled workflows in repositories with 60 days of no commit activity — which is exactly the
state a tap repository sits in between releases. Dispatch is the reliable choice.

`packaging/README.md` is updated: the "Publishing is deliberately manual" section becomes
"Publishing is deliberately merged", keeping the reasoning and replacing the copy-by-hand
instructions with the dispatch-to-pull-request flow, while keeping the manual steps documented
as the fallback for when the automation is unavailable. Its Homebrew and Scoop subsections are
rewritten for the shared repository, replacing the reference to a separate
`atakankizilyuce/scoop-bucket` at `packaging/README.md:52`.

`README.md` gains an install section for both package managers, using the commands in §6.1.

---

## 7 · Security

Three points that are real and need naming.

1. **Text from GitHub is rendered on the phone.** `tag_name` and `html_url` are untrusted
   input. Validate the tag against `^v?\d+(\.\d+){0,3}(-[0-9A-Za-z.-]+)?$` and reject the
   release outright if it fails. Accept `html_url` only when it parses as `https` with host
   exactly `github.com`; otherwise substitute the known releases URL. Without this a
   malformed or tampered API response injects text into the phone UI.
2. **The upgrade command is never built from network data.** It comes from the fixed table
   in §4.2. Reading a command out of release notes would let whoever writes those notes hand
   a command to every user.
3. **The response body is bounded.** `json.NewDecoder(resp.Body)` (`update.go:101`) is
   unbounded today, and the list endpoint returns a substantially larger payload than
   `/releases/latest`. Wrap in `io.LimitReader` — 1 MiB is far above a 30-release page and
   far below anything that matters.

The honest cost: the check now happens daily rather than once per start, so "a LeaveSafe of
version X is running at this IP" reaches GitHub more often. Nothing else is sent — no
identifier, no configuration, no sensor data — and the `User-Agent` remains what
`update.go:88` sets. `update_check: false` remains the way out. Add a short paragraph to
`SECURITY.md` stating the frequency, what is disclosed, and how to switch it off.

---

## 8 · Error handling

| Condition | Behaviour |
|---|---|
| GitHub unreachable | `log.Debug` and nothing more, as today (`main.go:727-730`). GitHub's reachability says nothing about this machine, so it is not warning-worthy. |
| 403 / 429 (rate limited) | Treated as a failure; triggers the one-hour backoff. |
| Malformed JSON, or a tag failing validation | Release skipped; if no release in the page qualifies, the result is "nothing newer". |
| `update.json` missing or unparseable | Treated as never-checked. |
| `os.Executable` or `EvalSymlinks` fails | `MethodUnknown`; the releases URL is shown instead of a command. |
| Running a `dev` build | No check performed at all (`isRelease`). |
| Beta channel, no prereleases exist | Falls through to the newest stable, which is correct: beta means "also betas", not "only betas". |
| Stable channel, only prereleases exist | Nothing reported. This is today's repository state and is correct behaviour, not a bug. |

---

## 9 · Testing

**`internal/update` — query.** Table tests against `httptest.Server` serving fixture release
lists: stable only; prerelease only; mixed; a prerelease newer than the newest stable (on
both channels); a stable newer than a running prerelease; empty list; malformed tag;
injection attempts in `tag_name` and `html_url`; `html_url` on a non-github.com host; 403;
500; a body larger than the limit. Follows the existing `update_test.go` shape.

**Version comparison.** Direct tests for prerelease precedence: `v1.0.4-beta` < `v1.0.4`,
`v1.0.4` < `v1.0.5-beta`, `v1.0.4-beta` == `v1.0.4-rc1` (the admitted limit), and the
existing cases still passing.

**`channel.go`.** Table test over literal path strings for all four methods, with both
separators, mixed case, and paths that merely resemble a match (`/Cellar/other/`,
`\scoop\apps\somethingelse\`).

**`ledger.go`.** Round-trip, missing file, unparseable file, concurrent access — using
`t.TempDir()`.

**Scheduler.** The interval and a clock are injected, so the 24-hour logic is tested in
milliseconds. Cases: due at start; not due at start; failure backoff; `LastSeenLatest`
suppressing a repeat; a new version defeating the suppression.

**`internal/config`.** Clamp and validation cases for the two new fields, asserting the
`notes` text, plus the accessors' zero-value handling.

**`internal/ws`.** `messages_test.go` covers the new type and payload; a hub test asserts the
message is sent after `auth_ok` and on broadcast, and never to an unauthenticated client.

**Web.** `make web-lint` (Biome + `tsc`), and `web/dist` rebuilt and committed — CI fails on
drift (`README.md:555`).

**Workflows.** The publish path cannot be tested against the real tap repository and winget
fork, which is what `dry_run` is for: run `publish.yml` once in dry-run against a real tag
before the first live publish. On the `LeaveSafe` side, the dispatch job's guards are the part
worth exercising deliberately — confirm that a rehearsal run (`workflow_dispatch`, no tag) and
a prerelease tag both skip it, since a dispatch fired by mistake is the one failure here that
reaches users.

Everything runs under `make check`.

---

## 10 · Documentation

- `README.md`: `update_channel` and `update_check_hours` in the configuration table; `update`
  in the dashboard command table; a short note under Features that the check is periodic and
  reaches the phone.
- `SECURITY.md`: the disclosure paragraph from §7.
- `packaging/README.md`: the publishing section rewritten per §6.
- `CHANGELOG.md`: an entry under Unreleased.

---

## 11 · The bootstrap reality

This determines when "everyone installed" actually happens, so it must be stated plainly.

**Copies installed today can never use this mechanism.** They run `v1.0.x-beta` binaries with
no channel support, no periodic check and no phone notification. This code can only notify
people who have already moved once to a release carrying it. That is a structural property of
every updater, Sparkle and Omaha included — not a flaw in this design.

In practice: **a stable release has to be cut.** Without one, nobody on the stable channel
hears anything, and that is the correct behaviour rather than a bug. The first stable
announcement is made once by hand through the channels that exist — README, the GitHub
releases page, the package managers — and from then on the mechanism carries itself.

---

## 12 · The release process once this ships

1. Add a line to `CHANGELOG.md` under Unreleased; merge to `main`.
2. `git tag v1.3.0 && git push --tags` → `release.yml`: CI gate, then five build targets with
   signing, provenance attestation, `.sha256` files and the SBOM, and the GitHub Release is
   created.
3. The `packaging` job generates the manifests and attaches them to the release *(existing
   behaviour, unchanged)*.
4. For a stable tag, `release.yml` dispatches to `homebrew-tap`, whose `publish.yml`
   regenerates the manifests and **opens a pull request** there. Nothing is published yet.
5. Review that pull request and **merge it** — this is the publish. Homebrew and Scoop users
   have the new version at that moment, and the merge triggers the winget pull request against
   `microsoft/winget-pkgs`, which Microsoft reviews on their own schedule.
5. Installed copies check within 24 hours, or at the next start if an interval has passed →
   everyone on the stable channel sees `v1.3.0` → a line on the dashboard, a dot in the
   phone footer, and the command for their own install channel under Settings › Updates.
6. Homebrew, Scoop and winget users additionally see it through their own package manager.

**Propagation time:** 0–24 hours, plus whenever step 4 is triggered for package-manager
users.

---

## 13 · Scope — two pull requests

Independent: no shared files, no shared state.

**PR 1 — in-app signalling**
`internal/update/` (channel support, semver precedence, `channel.go`, `ledger.go`), the
scheduler in `cmd/leavesafe/main.go`, the `update` dashboard command, the `ws` message type,
the three config fields and their payload entries, the phone footer and Settings section,
all tests, rebuilt `web/dist`, and the `README.md` / `SECURITY.md` / `CHANGELOG.md` updates.

**PR 2 — distribution channel**
In `LeaveSafe`: the dispatch job appended to `release.yml` with its two guards, the corrected
comment at `release.yml:312-321`, the `packaging/README.md` rewrite, and the new install
section in `README.md`.
In `atakankizilyuce/homebrew-tap`: `publish.yml`, the directory layout, and the repository's
own `README.md`.
Blocked on the prerequisites in §6.3 — creating the tap repository, allowing Actions to open
pull requests in it, and adding the two secrets — which are account actions, not code.

Note that PR 2 spans two repositories, so it is two pull requests in practice. Land the tap
repository's `publish.yml` first: until it exists, a dispatch from `release.yml` goes nowhere,
which fails quietly and in the harmless direction.

`release.yml` is untouched by both.
