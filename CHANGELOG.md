# Changelog

All notable changes to LeaveSafe are recorded here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and
versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

For a security tool, "notable" is read generously: anything that changes what
LeaveSafe watches, what it reports, or what it trusts belongs here even when the
diff is small.

## [Unreleased]

### Added

- **Autostart.** `leavesafe install-service` registers LeaveSafe to start at
  login — a systemd user unit on Linux, a LaunchAgent on macOS, a Scheduled Task
  on Windows. `uninstall-service` and `service-status` go with it. A reboot used
  to end monitoring with nothing said about it.
- **Headless mode.** `-headless` runs without the terminal dashboard, for
  autostart. Since there is no screen to show a QR code on, it reuses a pairing
  key stored owner-only in the config directory, which is what lets a phone
  reconnect after a restart.
- **Panic recovery.** Every long-lived loop — sensors, the alert dispatcher, the
  heartbeat, BLE, the location tracker — is supervised and restarts after a
  panic instead of taking the process down. Recovered panics are logged, written
  to the event history and shown on the dashboard.
- **Interrupted-monitoring warning.** The armed state is recorded to disk, so a
  start after a crash, a flat battery or a reboot says the machine was armed when
  LeaveSafe last stopped, and when. Re-arming automatically is opt-in via
  `restore_armed_state`.
- **Session expiry.** Session tokens now have an absolute lifetime
  (`session_ttl_minutes`, default 24 hours) and an idle timeout
  (`session_idle_minutes`, default 8 hours). Either can be switched off with `0`.
- **Certificate check at pairing.** The QR code carries the TLS certificate
  fingerprint, the server names its certificate before it is asked for anything,
  and the phone refuses to send the pairing key if the two disagree. The
  fingerprint is shown on the pairing screen so it can be compared against the
  browser's warning. See SECURITY.md for what this does and does not catch.
- **Update check.** LeaveSafe asks GitHub whether a newer release exists and
  reports it on the dashboard **and to the paired phone**, with the upgrade command
  for however this copy was installed — Homebrew, Scoop, winget, or the releases
  page. It asks once a day rather than once per start, because a copy installed as
  a service runs for weeks and the installations most in need of a fix were the
  least likely to hear about one. The schedule survives restarts, so a crash loop
  cannot turn into a flood of requests.

  `"update_channel": "beta"` opts into prereleases; `stable` is the default and
  hears about full releases only. `"update_check_hours"` changes the interval.
  `update` on the dashboard checks on demand and answers either way. Everything is
  changeable from the phone's settings screen.

  The first check is prompt: a few minutes of random delay spreads a reboot's
  worth of installations without making someone who just launched the program wait
  hours to hear that a fix exists.

  Nothing is downloaded and nothing is replaced. Switch the whole thing off with
  `"update_check": false`. What the check discloses is set out in SECURITY.md.
- **Log rotation.** `events.jsonl` and the new application log
  (`leavesafe.log`) rotate on size and keep a fixed number of generations, so a
  machine running for months no longer leaks disk.
- **Application log file.** The terminal log is mirrored to `leavesafe.log` in
  the config directory, which is what makes "why did nothing happen last Tuesday"
  answerable after the window is closed.
- **CLI surface.** `-version`, real `-help`, and `version` / `help`
  subcommands.
- **PWA manifest and icons.** The phone UI installs to the home screen and runs
  standalone. A page kept in a tab is a page the phone may discard, and a
  discarded page is not there when the alarm fires.
- **Build provenance and SBOM.** Every release artifact carries a signed
  attestation naming the workflow and commit that produced it, and a CycloneDX
  SBOM is published alongside. Verify with
  `gh attestation verify <file> --repo atakankizilyuce/LeaveSafe`.
- **Code signing pipeline.** The release workflow signs and notarizes macOS
  builds and Authenticode-signs Windows builds when the certificates are
  configured, and skips itself cleanly when they are not. The secrets it reads
  are the `env` keys of the signing steps in
  `.github/workflows/release.yml`.
- **Package manager manifests.** Homebrew, Scoop and winget manifests are
  generated from the published artifacts on each release. See `packaging/`.
- **Install from a package manager.** `brew install leavesafe`,
  `scoop install leavesafe` and `winget install LeaveSafe.LeaveSafe` now have
  somewhere to come from. A stable tag asks
  [`atakankizilyuce/homebrew-tap`](https://github.com/atakankizilyuce/homebrew-tap)
  to publish, and a pull request opens there; **merging it is the publish**, which
  also submits the winget manifests to `microsoft/winget-pkgs`. A tag push still
  publishes nothing on its own, and prereleases never reach a package manager.
- **SECURITY.md.** A vulnerability disclosure policy, a supported-versions
  statement, and an honest account of what is deliberately out of scope.
- **Tests** for `config`, `eventlog`, `network`, `qr`, `safe`, `rotate`, `state`
  and `update`, which had none.

### Changed

- **PIN hashing moved to scrypt.** Existing SHA-256 hashes still verify and are
  rewritten on the next successful disarm — the only moment the PIN is in hand.
- **`ReadLast` tails the event log** instead of reading the whole file, so
  showing twenty entries touches kilobytes rather than months of history.
- **Config from a client is validated.** Values that would break the program —
  a zero heartbeat, a year-long lockout — are clamped and the adjustment is
  logged, rather than obeyed.
- **`PORT` parse failures are reported** rather than silently falling back to
  the configured port.
- **Response headers**: HSTS is sent when serving HTTPS, and a
  `Permissions-Policy` denies every capability the UI does not use.

### Fixed

- **A corrupt `config.json` is moved aside rather than overwritten.** The
  program ran on with defaults and saved over the file at the first settings
  change, destroying a PIN hash and a geolocation API key that exist nowhere
  else. The backup path is named in the error.

### Security

- Session tokens no longer live until the process restarts.
- The pairing key is withheld when the server presents a certificate other than
  the one the scanned code named.
- Disarm PINs are hashed with scrypt rather than a single round of SHA-256.

---

## How to read this file

Entries are grouped by what they mean for someone running LeaveSafe:

- **Added** — something is there that was not.
- **Changed** — something behaves differently. Anything requiring action is said
  outright.
- **Deprecated** — still works, will not forever.
- **Removed** — gone.
- **Fixed** — it was broken.
- **Security** — a flaw closed, or a defence strengthened. Read this section
  even when you skip the rest.

[Unreleased]: https://github.com/atakankizilyuce/LeaveSafe/commits/main
