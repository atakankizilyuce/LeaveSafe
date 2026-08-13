# Changelog

All notable changes to LeaveSafe are recorded here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and
versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

For a security tool, "notable" is read generously: anything that changes what
LeaveSafe watches, what it reports, or what it trusts belongs here even when the
diff is small.

## [Unreleased]

### Added

- **A language for the startup questions.** The first start in a terminal asks
  Turkish or English and remembers the answer. It used to carry both languages
  on every line, which read as neither. The choice reaches the startup questions
  and stops there — the dashboard, the log and the phone are in English, and
  translating the first screen alone while implying more would be worse than not
  asking. `lang` in the console changes it for the next start.

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
- **A run without the dashboard.** `-plain` prints the QR code, the address and
  the pairing key once and then logs, leaving the terminal exactly as it was
  found — for anyone who would rather keep using the window they typed into.
  Commands still work; only the full-screen layout is gone. Redirecting output
  to a file or a pipe selects it automatically, which is also a fix: the
  dashboard used to write its cursor escapes into the file and lay itself out
  against a window size it had invented.

### Removed

- **Remote access, and everything that served it.** LeaveSafe no longer asks the
  router for a port mapping, looks up a public address, or publishes a listener
  beyond the local network. The first-run connection question, the `mode` and
  `cert` console commands, the phone's remote-access panel, `remote_access` and
  `remote_port`, and the UPnP dependency are all gone with it.

  The reason is that the feature could not tell the two failures apart. Behind
  carrier-grade NAT the public address belongs to the internet provider and is
  shared between subscribers, so nothing arriving at it can be attributed to one
  machine — and from inside, that looks exactly like a second router the owner
  could configure. So the program sent people to forward a port on a box they do
  not own and cannot reach. An instruction that sounds actionable and is not is
  worse than saying nothing.

  **The local network is unchanged.** The listener has always been plain HTTP on
  the LAN, the certificate belonged only to the internet-facing port, and the
  fingerprint was never in a local QR code. Pairing, arming, disarming and the
  alarm work exactly as they did.

- **The TLS certificate and the fingerprint check.** Both existed for the
  internet-facing listener and went with it. `cert` is gone from the console, and
  the pairing screen no longer shows a fingerprint to compare — over plain HTTP
  there is no certificate for a phone to check. See `SECURITY.md` for what that
  means for pairing on an untrusted network.

### Changed

- **Armed is no longer red.** Being covered is good news, and painting it red
  told the owner something had gone wrong at the exact moment nothing had — and
  spent the alarm colour on a state that is not an alarm, leaving the page
  nowhere louder to go when something did fire. Standby is now an almost
  colourless near-black, arming climbs towards armed a third at a time so the
  colour of the page *is* the countdown, and armed is a calm blue. Red appears on
  exactly two surfaces in the whole product: the alert, and the one sensor that
  fired. Blue rather than green, because green and red are the pair one man in
  twelve cannot tell apart.

- **The sensors are an orbit around the thing that watches with them.** Six
  tiles changing colour was a legend; arming now pulls every sensor that is
  actually covering you into the middle, and the ring closes into a shield
  around them. The ones that are not covering you travel the other way, into a
  region of their own with a stated reason each — `no sensor on this machine`,
  `you switched it off`, `its driver stopped answering` — because dimming them
  where they stood read as "these are somehow still involved". A sensor that has
  tripped goes nowhere: it stays on the ring in red, since it is the thing you
  came to look at. Each station is still its own switch, and each carries a
  badge whose *shape* says what it is doing, so the state survives a reader who
  cannot see the colour.

- **Buttons say what they do.** Every action carries an icon beside its label —
  a padlock closing and opening for arm and disarm, a key for pairing, a
  crossed-out bell for dismissing an alarm. Beside the words, never instead of
  them.

- **What a sensor watches, and its self-test, moved to one disclosure under the
  ring** rather than an "i" in the corner of each of six tiles. The ring has no
  corner to put six of those in, and the question it answers is asked once, on
  the first visit, about all of them at the same time.

- **The README is half the length it was.** Location, autostart, configuration
  and development each moved to a page of their own under `docs/`,
  and every screenshot, the demo GIF and both animated SVGs were recaptured
  against the new design. `SECURITY.md` is shorter too — every limitation is
  still there, with the reasoning tightened around it.

- **The release attestation runs on a newer action.** `attest-build-provenance`
  moved from v3 to v4.1.1. It is what lets `gh attestation verify` prove a
  downloaded binary came out of this repository's release run, so it is worth
  keeping current even when nothing is wrong with it.

- **A sensor tile on the phone is now the switch.** Tapping one turns that
  sensor on or off there and then, and the tile carries a switch showing which
  way the next tap will go. It used to open a panel first, and the switch lived
  inside that — so the tile that was the only thing on the screen saying whether
  a sensor was watching was not the thing that changed it, and turning one off
  took three taps. What two words cannot say — what the sensor actually watches,
  why it is unavailable on this machine, and the self-test — moved to an **i**
  in the tile's corner.

- **PIN hashing moved to scrypt.** Existing SHA-256 hashes still verify and are
  rewritten on the next successful disarm — the only moment the PIN is in hand.
- **`ReadLast` tails the event log** instead of reading the whole file, so
  showing twenty entries touches kilobytes rather than months of history.
- **Config from a client is validated.** Values that would break the program —
  a zero heartbeat, a year-long lockout — are clamped and the adjustment is
  logged, rather than obeyed.
- **`PORT` parse failures are reported** rather than silently falling back to
  the configured port.
- **Response headers**: a `Permissions-Policy` denies every capability the UI
  does not use.

### Added

- **The alarm reaches your phone even when nothing can connect to the laptop.**
  Every alert used to travel over an open WebSocket, so the one alarm that
  matters most — the laptop is being carried out of the building, you are
  somewhere else — was the one your phone could not be told about: behind
  carrier-grade NAT or a hotspot there is no address for it to connect to. The
  laptop shrieked in the room it was in and said nothing to anybody else.
  LeaveSafe now takes a web push subscription when a phone pairs, and delivers
  the alarm through the phone's own browser push service over an outbound
  request that works from behind any NAT that lets anything out at all.

  **It does not currently deliver.** A push subscription needs a service worker,
  a service worker needs a secure context, and the plain-HTTP address LeaveSafe
  serves on a local network is not one. The only secure context this program
  offered was the internet-facing listener, which has been removed — so the
  machinery is present and nothing reaches it. Giving a phone an alert when it
  is not connected needs a secure context first.

  No relay of ours, no tunnel, no account, and nothing new to trust. The payload
  is encrypted to a key only the subscribing phone holds (RFC 8291), so what the
  push service carries and stores is ciphertext it has no key for. What it does
  learn is that a message was sent and when — unavoidable when somebody else
  carries the message, and why the payload says an alarm fired rather than what
  tripped it or where the laptop is. The encryption is checked against the
  specification's own worked example, byte for byte.

  It is an addition rather than a replacement: a connected phone is still told
  over its connection, immediately. Subscribing needs notification permission,
  which is asked for once, after pairing, and never again if refused. On iPhone
  the web app has to be added to the home screen before iOS will allow push at
  all — that is Apple's rule, not this program's.

### Fixed

- **Starting the program checks for a new release, and says so.** The check ran
  on a daily schedule that survived restarts, so a release cut this afternoon
  was not noticed until tomorrow — and restarting to see whether anything had
  changed did nothing, because the schedule is deliberately not reset by a
  restart. A start now looks straight away unless a check ran in the last
  fifteen minutes, which is the only case the daily interval was really
  protecting against: a copy restarted every few seconds by a service manager.

  A start also repeats a release this installation has already found, without
  asking the network anything. It used to be announced once, into a log that has
  since scrolled, on a run that has since ended — and the silence afterwards read
  exactly like there being nothing to say. It stops once that release is the one
  running, so upgrading ends the notice rather than anybody having to clear it.

  And a development build now says why it will never check. `Checker.Check`
  returns before it reaches the network when the running version is not a
  release, which is correct — there is nothing to compare against — but saying
  nothing about it made a development build look like a copy that was up to date.
- **The laptop makes the noise now, on all three.** The alarm on the machine being
  carried away was `kernel32.Beep` — a single square tone, silent between one
  call and the next. The phone shrieked and the laptop chirped, which is
  backwards: the laptop is the thing in the room somebody needs to look at.
  LeaveSafe now generates a siren sample by sample and plays it through the
  machine's own audio output: two dissonant pairs of notes slamming back and
  forth five times a second, roughened by an amplitude wobble fast enough to be
  heard as harshness, and driven into a soft clipper so it carries through a
  closed door. Deliberately not a sweep — a tone gliding up and down is what
  every emergency vehicle does, and a laptop making that sound reads as an
  ambulance outside rather than as this machine, in this room, now. Dismissing it
  stops the sound within a twentieth of a second. Where there is no audio
  device — no sound card, a driver that will not open, headphones pulled out
  mid-alarm — it falls back to the beeps, because silence is the one outcome that
  helps nobody.

  The way to the speaker is different on each platform and the siren is not:
  winmm on Windows, ALSA on Linux — which is the layer PulseAudio and PipeWire
  both sit on, so this reaches the mixer you are actually listening to — and an
  audio queue on macOS. A machine with no sound library on it at all says so and
  falls back to the beeps.
- **The phone stops shouting over the laptop.** The phone played a square wave
  at full scale with a second one at half on top of it, which is more than a
  speaker can produce: what came out of a pocket was the phone's limiter rather
  than an alarm. It now plays the same siren the laptop does — the same notes,
  the same roughness, the same edge — at half scale, so it is recognisable as
  one alarm speaking in two places and the speaker has somewhere left to go. The
  laptop is the one that has to be heard across a room; the phone is a few
  inches from the person who needs telling, and the buzzing does as much of that
  work as the noise. It also loops in the audio thread rather than on a timer,
  which matters for the one stretch that counts: a phone with the screen off
  throttles a timer to once a minute.
- **Typing has a line of its own.** The terminal echoed what you typed wherever
  the cursor happened to be, which was inside the scrolling log — so a sensor
  reporting itself while you were half-way through `disarm` wrote its message
  across the word, and the word stayed there with the log wrapped around it. On a
  dashboard LeaveSafe now reads the keyboard a keystroke at a time and draws the
  typing itself, on a row at the foot of the window that the log cannot reach. A
  log line arriving mid-command is a repaint rather than a collision. Backspace,
  Home, End, Delete, the cursor keys, Ctrl+U and Ctrl+W work on that line, ↑ and
  ↓ walk through what was typed before, and Ctrl+C and Ctrl+Z do what they always
  did — they arrive as keystrokes now rather than as signals, so the program
  answers them itself. Input that is not a terminal is read as whole lines, as
  before.
- **The QR box follows the code on screen.** It used to be sized to the largest
  code on offer, so a window with room for the one somebody was actually looking
  at was told it had none — and a default 120×30 terminal could end up showing no
  code at all. The box now takes the size of the code being drawn, and `qr <n>`
  moves the layout with it.
- **The code outlasts the decoration.** A window too small for everything gave
  up the QR code while keeping the block letters and the command list. It is the
  one part of that screen that is there to be used rather than read, so it is
  the last thing to go: the banner shrinks first, then the command list, then
  the shape changes, and only then is the code dropped — with a line saying so.
  A default 120×30 terminal used to show no code at all; it now shows one.
- **A small window keeps the address instead of the command list.** In a window
  with no room for the QR code, the status grid was clipped where the log
  started — but the command footer is drawn at fixed rows above the log, so it
  was printed straight through the middle of the grid. Around 80×24 that meant a
  list of commands across the address and the pairing key. The footer now gives
  way instead: `help` says everything it says, and nothing else says the address.
- **A PIN typed to disarm is no longer shown.** It was echoed in the clear onto
  the screen of the laptop somebody had just picked up, and left in the terminal
  scrollback afterwards. It is drawn as asterisks and never remembered in the
  command history.
- **One status grid, not two.** The dashboard drew at absolute rows worked out
  once at startup, and two ordinary things moved those rows out from under it:
  a longer address producing a bigger QR code, and the window being resized. Each
  left the old drawing on screen with the new one painted somewhere else. Worse,
  the layout was worked out for a window of 120×40 whenever the real one was
  smaller than 80×20 — so the log's scrolling region was pinned across the middle
  of the status grid, and every line written scrolled part of the dashboard away
  for the next repaint to draw again lower down. The layout is now one value
  computed from the window as it is, and every repaint checks it still describes
  the screen before painting a piece of it.
- **The dashboard fits the window it is in.** A QR code carrying a pairing key is
  around twenty-eight rows and a default terminal is thirty, so there was no
  window in which the old layout fitted; it drew the code and then pinned the log
  over the bottom of it. Now the block-letter banner gives way first — it is
  decoration, and the code is what the program is for — then the code moves above
  the status grid in a window too narrow for both, and only in a window with room
  for neither is it dropped, with a line saying so where it would have been. Long
  lines are cut to the window instead of wrapping, which was its own way of
  pushing everything below them down a row.
- **A resized window is drawn for again.** Immediately where the platform says so,
  and within five seconds everywhere else.

- **The terminal is given back.** The dashboard cleared the screen, drew at
  absolute positions and pinned a scrolling region under its header — on the
  window the user had typed the command into. That took their scrollback with
  it, left them unable to scroll, and the scrolling region was never reset, so
  the shell inherited a window that could only scroll its bottom few rows long
  after LeaveSafe had exited. It now draws on the alternate screen buffer, the
  one terminals keep for full-screen programs, and hands the original back
  whole on every way out: Ctrl+C, a failed start, and a fatal error from inside
  the logger, which used to exit without undoing anything at all.
- **Ctrl+Z works.** Suspending gave the shell back a terminal still on the
  alternate screen with a scrolling region across it, so the prompt landed on
  top of a QR code and scrolling up showed nothing. Which is why the program
  looked as though it could not be put in the background. The terminal is now
  handed back before the process stops and the dashboard is redrawn when it is
  brought forward.
- **The console window is no longer maximized out from under you.** On Windows
  LeaveSafe asked for the console to fill the screen, which is right for the
  window Windows opens when the executable is double-clicked and wrong for a
  terminal somebody was already working in. It now checks which one it is in.

- **A fresh installation now watches something.** Sensors are registered
  switched off and turned on from what the config recorded — and a config
  written by a first run records nothing at all, so every sensor stayed off.
  Arming started no watchers: the dashboard read "0 / 6 active", the phone read
  "0 sensors ready", and anyone who did not stop to read either walked away from
  a laptop guarding itself against nothing. Every sensor is now on unless the
  config says otherwise, and a sensor switched off is switched off at startup
  rather than merely not switched on — so the preference survives a restart in
  both directions. "Reset to defaults" now reaches the sensors too, instead of
  writing a config that says every sensor watches while one of them does not.

- **A second alarm sounds.** The hub suppresses further sensor events while an
  alarm is active, and several things left that state standing with nobody able
  to clear it — so the first alarm was the last one, on the phone and on the
  laptop alike. A phone that paired while one was sounding was told nothing
  about it: its screen had locked, the page behind it was thrown away, and it
  came back to a calm panel with nothing to dismiss while the laptop screamed.
  It is now told, with the same words the phones already connected were shown.

- **Answering an alarm reaches every device.** Dismissing from one phone reached
  no other phone, and disarming reached none of them: the laptop went quiet and
  every phone kept sounding at an alarm the machine had already stopped having,
  its overlay offering to pause a sensor that was no longer alarming. Every path
  that clears the alarm — the phone, the console's `stop`, disarming — now says
  so to all of them.

- **The phone's siren can sound more than once.** Its audio context was built
  for each alarm and closed on dismissal. A phone caps how many a page may open
  and only lets one start off the back of a gesture, so the one built for the
  second alarm — minutes later, with the phone in a pocket — stayed suspended
  and played nothing. There is one context now, opened inside the tap on Arm and
  resumed rather than replaced.

- **Saving the settings sheet no longer sets the alarm off.** The laptop uses
  the alert channel to say things about itself as well — a setting that needs a
  restart, a geolocation endpoint it refused, a sensor change it would not make
  while armed — under the reserved sensor name `system`. The phone treated all
  of them as intrusions: full-screen overlay, siren, vibration, lock-screen
  notification, for pressing Save while sitting next to the machine. Worse, the
  overlay's other two answers are "pause this sensor" and "stop using this
  sensor", and there is no sensor called `system` to do either to. Notices are
  now shown as notices, and the alarm is kept for the thing it is for.

- **"Pause this sensor" answers a self-test.** Firing a sensor by hand — from
  the phone's "Test it" button or `trigger <sensor>` on the dashboard — raised
  the alarm without recording which sensor raised it. The overlay's pause and
  disable buttons act on the recorded sensor, never on the name the message
  carried, so both silently did nothing and the only trace was a debug line on
  a laptop nobody was standing next to.

- **Quitting is no longer reported as a crash.** Ctrl+C shut the server down and
  then treated the shutdown it had just asked for as a fatal server error: a red
  FATAL line and an exit status of 1, racing the orderly exit the signal handler
  was already running. A listener that dies for any other reason still stops the
  program.

- **Two data races on values the user reads off the screen.** The pairing key
  and the dashboard's address list were read without the lock that guards them
  while `rotate-key` and the reachability probe were replacing them. What that
  risked was the two things the user is asked to scan or type.

- **The settings sheet closes when it is pulled down.** It had always looked
  like something you could push out of the way — it sits on the bottom edge with
  a grip drawn across the top — and dragging it did nothing. What closed it was
  a thumb landing on the dimmed area above it, which is why it seemed to work
  some of the time and not others: where the gesture started was being read, not
  the gesture. The sheet now follows the finger and goes away when let go past
  roughly a third of the way down, or on a flick. A drag that begins partway
  down the list still scrolls the list, and a scroll that runs off the end of it
  no longer bounces the page behind. The grip and the title stay on screen while
  the list scrolls under them, so *Done* is never something to scroll back up
  and find.
- **Closing the settings sheet says when it has thrown edits away.** It always
  discarded unsaved changes, which mattered less when closing took a deliberate
  press of *Done*; now that a thumb can flick the sheet shut, it says so instead
  of letting the switches quietly snap back.
- **Press animations work again.** Everything that animates into place on the
  phone — sensor tiles, the log, the alert overlay — did so with an animation
  filling forwards, and a filled animation keeps applying its own `transform`
  afterwards, beating the `:active` rule underneath it. Every one of those
  elements had a press animation written for it and none of them had played
  since.
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

- **The WebSocket library is one that is still maintained.** Every byte a phone
  sends reaches LeaveSafe through this package, and the one in use —
  `nhooyr.io/websocket` — has been abandoned: upstream renamed the repository to
  `websocket-old` and published nothing since August 2024. A parser for untrusted
  network input that nobody will patch is a hole waiting for its advisory, and no
  advisory would ever arrive to warn about it, because there is no longer anyone
  to file one. LeaveSafe now uses `github.com/coder/websocket`, the maintained
  continuation by the same author under a new home. It is the same package at the
  same version line, so nothing about the protocol or the socket behaviour
  changes — only whether a fix can be expected to exist.
- **The phone interface's dependencies are watched.** Dependabot covered Go
  modules and the workflow actions but not `web/`, so the 138 npm packages behind
  the phone screen were the one part of the supply chain nobody was told about.
  The build output of those packages is committed and embedded in the binary, so
  a bad package there ships to users exactly like a bad Go one. They are now on
  the same weekly schedule as the rest.
- **A phone can reconnect as often as it likes.** The cap on sockets that have
  not paired yet counted a peer by its address when it took a slot and by its
  address *and port* when it gave one back, so the per-address count only ever
  went up. Four reconnects from one phone — four screen locks — and the owner
  was refused by their own laptop with "the laptop is busy" until the process
  was restarted. The same asymmetry left an entry behind for every address that
  ever connected, which is memory a stranger gets to spend.
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
- **The Host header can no longer write part of the policy it is answered with.**
  The address a request asked for is named in the `connect-src` of that response,
  which is the directive that keeps script on the page from opening a socket to
  anywhere but this machine. It used to be copied out of the header as it
  arrived: the address half was checked and everything after the colon was not,
  because splitting a host from a port hands back whatever followed it without
  looking. A semicolon is legal in a `Host` and is the separator between
  directives in a policy. The port is now held to being a port, and what the
  server says about itself is rebuilt from the parts that passed rather than
  echoed.
- **A position source cannot report a place that is not one.** Coordinates from
  the Wi-Fi and IP lookups went to the phone unchecked, while the same values
  from the phone were held to the globe — so a point outside it reached the
  distance-moved figure and settled there, and the panel whose whole job is to
  say where the machine is would be stating something nobody measured. One rule
  now covers all three. The place name that comes back with an IP fix is filtered
  and bounded before it is repeated onto the owner's screen.
- **The geolocation API key is attached as a parameter rather than pasted on.**
  It travels in the endpoint's query string, and it was concatenated after a `?`
  — which assumed the key needed no escaping and that the endpoint carried no
  query string of its own. Neither holds: a `#` in a key truncates it, an `&`
  splits it into a second parameter the service logs rather than reads, and an
  endpoint that already had a query string got a second `?`. The key is a secret
  and the endpoint is configurable from the phone, so neither was this code's to
  assume.
- **The laptop will not fetch a geolocation endpoint that is not public.** Both
  location endpoints are configurable — the Wi-Fi one from the phone's settings
  screen — and the laptop is the one that fetches them, so a paired phone could
  point `geolocate_url` at an address on the laptop's own network and use the
  laptop as a blind probe of hosts it cannot reach itself. The https-only rule
  bounded the protocol but not the host. The two location clients now dial
  through a guard that refuses any resolved address that is not public unicast —
  loopback, private, link-local, multicast and the unspecified address are all
  turned away. The check runs at connect time, per resolved address, so a
  hostname that resolves to an internal address is caught where it matters and a
  redirect is re-checked on every hop.
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
