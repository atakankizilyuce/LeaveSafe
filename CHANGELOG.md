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

- **The connection mode is asked on every start, not only the first.** The
  question comes up whenever LeaveSafe is started in a terminal, with the
  current setting shown as the default so pressing enter keeps it. Asking once
  turned out to mean never asking again for most people: the phone's settings
  screen sends `remote_access` on every save, so saving any unrelated setting
  answered the question by accident and it never returned. A `-headless` start,
  or one with no usable stdin, takes the stored value and says which mode it
  came up in.
- **Remote access is switched on and off while LeaveSafe runs.** Changing it —
  from the phone, from the new `mode` console command, or at startup — takes
  effect immediately instead of asking for a restart. It runs on a second
  listener of its own, so **a phone connected over Wi-Fi stays connected while
  the setting changes**; previously the only listener moved to a new port and
  switched to TLS, dropping every paired phone and forcing a re-pair from the
  QR code. As a consequence the local-network listener is now plain HTTP whether
  remote access is on or off, which also removes the certificate warning that
  used to appear on local connections. See SECURITY.md.
- **The phone reports whether remote access actually works.** The settings
  screen shows the public URL, the certificate fingerprint, and — when the
  router refused an automatic port mapping — the TCP port to forward by hand.
  The switch could be flipped with no way to learn it had achieved nothing.
- **Carrier-grade NAT is detected and named.** When the ISP hands out an address
  in `100.64.0.0/10` it keeps the routable one, and no port forwarding on the
  user's own router can help. LeaveSafe now stops remote access and says so
  rather than leaving a listener up and the user forwarding ports at a problem
  that is not theirs.
- **Resetting every setting closes remote access.** The defaults leave it off,
  but the listener used to stay up — so "reset everything" could leave a port
  open to the internet while the config it had just written asked for nothing of
  the sort.
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

- **The winget submission no longer fails its own validation.** The generated
  installer manifest carried `PortableCommandAlias` on the installer entry, but
  the 1.6.0 schema only defines that field for files nested inside an archive.
  `winget validate` flagged it as an unknown field, the publish workflow treats
  a warning as failure, and the submission to microsoft/winget-pkgs never
  happened — Homebrew and Scoop published while winget silently did not. The
  field was also redundant: a bare portable executable takes its alias from
  `Commands`.
- **Pairing no longer waits for a sensor to work out whether it can run.** The
  reply to a pairing key carries the sensor list, and building it asked every
  sensor whether it can work on this machine. On Windows the lid sensor answers
  by starting PowerShell and querying WMI, under a twenty-second budget — and
  the phone gives up on a pairing reply after ten. Whoever asked first paid, and
  one of the things that asks is a pairing client, so a phone connecting in the
  first seconds after a cold start could be left waiting and then time out
  against a laptop that was working perfectly.

  The answer is now settled once, off any path a client waits on: the probes run
  side by side at startup instead of one after another, and the pairing reply,
  the status broadcast and the dashboard's own repaint read whatever is known
  rather than blocking. A sensor still working it out reads as unavailable for
  the moment, which the next broadcast corrects — under-reporting coverage is
  the safe direction. Arming still waits for a real answer, because starting the
  right set of sensors is worth a pause; it just no longer holds the sensor
  manager's lock while it does, which used to queue every broadcast behind it.
- **Running the test suite no longer destroys your own configuration.** The hub
  tests call `handleUpdateConfig` and friends directly, and those save through
  `config.Save` — which writes to the real config directory. So `go test ./...`
  on a developer's machine overwrote that developer's `config.json` with
  whatever payload the test happened to send. `config.Save` renames a temporary
  file over the original, so there was no backup and nothing to recover: a
  disarm PIN hash and a geolocation API key live nowhere else. CI never noticed,
  because a fresh runner has no configuration to lose. The package now isolates
  the config directory for every test in it, and a test fails if that isolation
  is ever removed.
- **The end-to-end harness waits for an answer, not for an open port.** It
  checked readiness by dialling the TCP port, but the app binds its listener
  early and only begins serving at the very end of startup — after drawing the
  dashboard, rendering a QR code per address and registering the sensors. The
  kernel completes handshakes into the backlog throughout that window, so the
  harness handed tests a server that could not yet reply, and the first test of
  a run could spend its whole ten-second pairing deadline waiting for a greeting
  that was never coming. It showed up as an intermittent "timed out waiting for
  an auth reply" on loaded runners.
- **A corrupt `config.json` is moved aside rather than overwritten.** The
  program ran on with defaults and saved over the file at the first settings
  change, destroying a PIN hash and a geolocation API key that exist nowhere
  else. The backup path is named in the error.

### Security

- **A phone can reconnect as often as it likes.** The cap on sockets that have
  not paired yet counted a peer by its address when it took a slot and by its
  address *and port* when it gave one back, so the per-address count only ever
  went up. Four reconnects from one phone — four screen locks — and the owner
  was refused by their own laptop with "the laptop is busy" until the process
  was restarted. The same asymmetry left an entry behind for every address that
  ever connected, which with remote access on is memory a stranger gets to
  spend.
- **The phone sends nothing until the connection has proved itself.** Inbound
  messages were already held to that and outbound ones were not, so the check
  guarded one direction. The phone reconnects to the same address every three
  seconds forever, so anything that could answer there once the laptop went
  quiet was handed the heartbeat — with the session token on it — the reply to
  "check the connection", which carries the phone's own precise position, and
  the digits typed into a disarm dialog that outlived the socket that raised it.
  The heartbeat is now stopped when its socket closes rather than running on
  across every reconnect, and the PIN dialog is closed with it.
- **A refusal is only acted on if something was asked.** `auth_fail` was
  believed from any peer, and on the resume path that reaches `clearSession()` —
  so anything answering the socket could make the phone throw its stored pairing
  away and leave the owner unpaired from a laptop they are not standing next to.
  It is now ignored unless the pairing key actually went out on that connection.
- **The public address is asked of the internet, not of the router.** With
  remote access on, the address came first from whatever answered an
  unauthenticated UPnP discovery on the local network, was never checked to be
  an IP address at all, and went to the front of the URL list — which is the QR
  code the dashboard displays, with the pairing key in it. A machine on the same
  café Wi-Fi that replied to the discovery first got to choose where the owner's
  phone tried to pair. STUN and the HTTPS lookup are now preferred, and the
  router's claim is refused unless it is a public IP address.
- **The Windows system directory comes from the kernel.** It was read from
  `%SystemRoot%`/`%windir%`, which an ordinary user can set through
  `HKCU\Environment` — so the absolute-path hardening added for `powershell`,
  `netsh` and `schtasks` could be pointed at a directory they own. And when the
  tool was not found there, the lookup fell back to the bare name, handing it
  straight back to `PATH`. Both doors are closed: the directory is asked of
  `GetSystemDirectory`, and the resolved path stays absolute whether or not
  anything is there to run.
- **The macOS LaunchAgent escapes the paths written into it.** A plist is XML
  and the paths are not this program's to choose, so a directory named with `<`
  or `&` could close its own element and add keys of its own —
  `DYLD_INSERT_LIBRARIES` among them — to a file launchd reads as root and acts
  on at every login. The systemd side already refused the equivalent.
- **One client's messages are handled one at a time, whatever the transport.**
  Per-connection state — whether the client has paired, its token, and the two
  meters that bound it — is kept without a lock, on the understanding that a
  WebSocket's read loop provides the order. The BLE backend has no such loop: it
  hands each incoming write to a fresh goroutine, so the pairing-attempt meter
  could be created twice and lose whichever copy had been counting. That meter
  is what keeps refused attempts out of the size-rotated security log, and over
  BLE it is reachable by anything in radio range without a key.
- **DNS rebinding is refused.** LeaveSafe now answers only to requests whose
  `Host` is an IP address (or `localhost`). The WebSocket's Origin check does not
  cover this attack — a rebound page sends the attacker's own domain as both
  Origin and Host, so the two match and the socket opened. Every address
  LeaveSafe hands out is an address literal, so nothing about the documented
  flow changes; reaching the dashboard by a hostname no longer works.
- **A pairing flood can no longer erase the event history.** Every refused
  pairing attempt used to write a record to the size-rotated security log, at
  whatever rate an unauthenticated peer could send them — enough to push out the
  arm, the alert and the disconnect that recorded an actual intrusion. Attempts
  made against an address already serving a lockout are no longer written (the
  lockout itself still is), and pairing now has a rate allowance of its own,
  sized so the lockout is always what refuses a client first.
- **The phone acts on nothing until the connection has proved itself.** A server
  that answered the phone's socket could send `auth_ok` without ever being given
  the pairing key, which opened the panel, and then `pin_required`, which put the
  disarm PIN dialog on screen and collected the code. It could also sound
  spoofed alarms on the lock screen. The certificate check did not stop it: that
  check lives in the greeting handler, and this needed no greeting.
- **A stored pairing session is held to the same standard as a scanned one.** A
  saved fingerprint that is not 64 hex characters is discarded rather than
  silently read as "no certificate to check", and a session with no fingerprint
  recorded is not resumed over HTTPS.
- **A sensor that fails is restarted, and is not reported as watching until it
  is.** Only a panic used to bring a sensor loop back. A driver that returned an
  error logged it and returned normally, which the supervisor read as "finished
  its work" — so the loop was retired, and because its cancel function stayed
  registered every later arm skipped the sensor as already running. One transient
  failure removed it for the life of the process. Worse, nothing said so: the
  dashboard and the phone kept counting it towards "5/5 sensors active" with the
  machine shown as armed. Failed sensors now retry with backoff, and both screens
  show the fault and the reason instead of counting it as cover.
- **Windows system tools are run from an absolute path.** `powershell`, `netsh`
  and `schtasks` were launched by bare name, so Windows searched `PATH` in order.
  A directory some installer added ahead of `System32` that ordinary users can
  write to was enough: whatever was dropped there under the right name would be
  run by LeaveSafe every couple of seconds while armed, in the owner's session —
  and `schtasks`, which `install-service` may run from an elevated prompt, would
  have run as administrator. The arguments were never the risk; which binary
  answered to the name was.
- **The systemd unit quotes and escapes the path to the binary.** `ExecStart`
  was written unquoted, so installing from a path containing a space pointed the
  autostart at the first word — a path any local user could then create and fill,
  to be run as the owner at every login. `%` is now doubled so systemd does not
  expand it, and a path containing a line break is refused rather than written,
  because in a unit file that is not a mangled path but a second directive.
- **The alarm sounds before it touches the volume.** A panic in a platform volume
  backend is recovered rather than fatal, which used to leave the alarm marked as
  sounding with no siren ever started — silent, and refusing to start again.
- Fixed a data race on the alarm's stop channel. A siren that was mid-tone
  through a dismissal and a fresh alarm could read the new run's channel, never
  see its own closed, and sound past the dismissal with nothing able to stop it.
- The event log's owner-only permissions are enforced on a file that already
  exists, not only on one this version creates.
- Release links from the update check are pinned to `github.com` on the phone as
  well as on the laptop.
- A `geolocate_url` or `ip_lookup_url` hand-edited into the config file must be
  HTTPS. The geolocation API key travels in that URL's query string, and the
  phone was already refused a plain-HTTP endpoint; the file was not.
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
