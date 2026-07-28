# Real-Environment Verification — Design

Date: 2026-07-28
Status: approved

## Goal

Every pull request must answer one question with evidence: **does this version of
LeaveSafe actually run, and actually detect hardware changes, on Windows, Linux
and macOS?**

Today nothing answers it. The test suite covers `manager`, `network`, `auth` and
`ws` in isolation; not one line of the 18 platform-specific sensor files is
exercised, and the binary is never started by CI on any operating system. A
release that fails to boot on macOS would ship green.

## Non-goals

- Simulating hardware that a CI machine physically does not have. Where a real
  trigger is impossible, the test is **skipped with a stated reason** — never
  replaced by a fake that reports success.
- Testing the phone-side web UI beyond the WebSocket protocol it speaks.
- BLE transport. Neither GitHub runners nor the Linux VM expose a Bluetooth
  adapter, so BLE stays out of scope and is documented as such.

## Guiding principle

A test either exercises the real thing or declares that it did not. There is no
third category. This is why the coverage matrix below is published in the CI job
summary on every run: the gaps are the point, not an embarrassment to hide.

---

## Removing Docker support

Docker support is removed as part of this work. It is not a casualty of the test
design; it is a feature that measurably does not function.

Container behaviour was measured on the WSL2 kernel — the same kernel Docker
Desktop for Windows runs containers on:

| Sensor  | Reality inside the container                                            | Verdict     |
| ------- | ----------------------------------------------------------------------- | ----------- |
| lid     | `/proc/acpi/button/lid` absent; Docker also masks `/proc/acpi` by default | dead        |
| usb     | `/sys/bus/usb/devices` absent                                            | dead        |
| input   | `/dev/input` absent                                                     | dead        |
| screen  | `xset` absent — the image is `FROM scratch` and contains no binaries      | dead        |
| power   | `/sys/class/power_supply` present, but it is the VM's synthetic battery  | **misleading** |
| network | container network namespace — reports `172.x`, never the LAN address     | meaningless |
| alarm   | `beepTone` writes `\a`; `libasound.so.2` is absent from `scratch`         | silent      |

Five of six sensors are dead, the sixth reports another machine's state, and the
alarm cannot sound. The product's entire value is "guard *this* laptop", and a
container cannot see that laptop.

The documentation also describes a setup that does not exist: `README.md` claims
the container "runs with `privileged: true` and mounts `/sys` and `/proc`", while
`docker-compose.yml` sets neither a privileged flag nor a single volume.

Removed: `Dockerfile`, `docker-compose.yml`, `.dockerignore`, the `docker` CI job
(and its entry in the `ci-success` needs list), the `docker` / `docker-run` Make
targets, the README Docker section and platform-support claim, and the
`isContainer()` branch in `internal/server/server.go` together with the
`CONTAINER` environment variable.

`Server.URLs()` currently prepends `localhost` when `CONTAINER=1`. With Docker
gone that branch is unreachable, so it is deleted rather than left as dead code.

---

## Architecture

Four layers, ordered by how close each sits to a real user's machine.

### Layer 0 — the application runs on all three real operating systems

Runs on `ubuntu-latest`, `windows-latest` and `macos-latest`, on every pull
request. GitHub runners are genuine machines, so this layer needs no simulation
at all: it builds the binary and **starts it**.

A Go test acts as the phone. It drives the real WebSocket protocol against the
real process:

1. Start the binary with an isolated `HOME` / `APPDATA` and a pre-seeded
   `config.json` (`remote_access: false`, so the interactive first-run prompt
   never blocks), and `PORT` set to a free port.
2. Read the pairing key from the process's stdout. The dashboard renders it into
   the status grid, so a `\d{4}-\d{4}-\d{4}-\d{4}` match on the output stream
   recovers it without touching production code.
3. Connect over WebSocket and walk the full user journey:
   - authenticate with a wrong key, five times, and confirm the lockout engages
   - authenticate with the correct key and receive `auth_ok`
   - `arm`, and confirm the broadcast status reflects it
   - `trigger_sensor`, and confirm `alert` then `alarm_active` arrive
   - `disarm` under PIN protection: wrong PIN rejected, correct PIN accepted
   - `get_config` / `update_config` / `reset_config` round-trip
   - open a fourth session and confirm the three-client cap holds
4. Send SIGTERM (`taskkill` on Windows) and assert the process exits cleanly,
   releases the port, and leaves a well-formed `events.jsonl` behind.

This is the layer that catches "the new version does not start on macOS", and it
catches it before merge.

### Layer 1 — Linux full-VM hardware replica

Linux is the one platform where real hardware can be conjured, because its
sensors read files that real kernel modules produce. A container cannot do this
(see above); a VM can.

GitHub's `ubuntu-latest` runners expose `/dev/kvm`, so the job boots a genuine
Ubuntu cloud image under QEMU/KVM. Inside the VM, as root, real devices are
created and the **unmodified production binary** reads them through the real
`/sys` and `/dev`:

| Sensor  | Real trigger                                                        |
| ------- | ------------------------------------------------------------------- |
| power   | `test_power` module → write `ac_online=0` — the charger is genuinely gone |
| usb     | `dummy_hcd` + a gadget → a real entry appears in `/sys/bus/usb/devices` |
| input   | `uinput` virtual keyboard → a real `/dev/input/eventN` with real events |
| screen  | `Xvfb` + `xset dpms force off` → real DPMS state change              |
| network | `ip addr add` → real IP address change                               |
| lid     | **skipped** — QEMU x86 emulates no ACPI lid button                   |

Each scenario arms the app through the WebSocket, performs the hardware change,
and asserts the corresponding alert reaches the client. If a module fails to
load, that scenario skips with the modprobe error as its reason; it never falls
back to a fake.

Mechanics: the scenario suite is compiled on the host
(`go test -c -tags sandbox`) and copied into the VM alongside the binary, so the
guest needs no Go toolchain and boots in seconds rather than minutes. cloud-init
provides the SSH key and package list.

### Layer 2 — Windows and macOS real triggers

Only what can genuinely be triggered on a runner is triggered. Everything else
skips.

| Sensor  | Windows                                | macOS                                     |
| ------- | -------------------------------------- | ----------------------------------------- |
| network | `netsh interface ip add address`       | `ifconfig lo0 alias`                      |
| screen  | skipped — locking the session would break the runner | `pmset displaysleepnow`     |
| input   | `SendInput` — viability measured on first run, skipped if the runner's session rejects it | skipped — CGEvent needs an accessibility grant no runner can give |
| usb     | skipped — no shareable device on the runner | skipped                               |
| power   | skipped — the runner has no battery    | skipped — the runner has no battery       |
| lid     | skipped — the runner has no lid        | skipped — no clamshell                    |

Skips are not silent. Each one prints `SKIP <sensor>: <reason>` and the job
writes the full matrix — triggered, skipped, and why — into
`$GITHUB_STEP_SUMMARY`, so a reader of the PR sees exactly which sensors were
really exercised on which platform.

### Layer 3 — parser unit tests

The one bug class the layers above cannot catch: an OS changes its output format
and the parser silently stops recognising a state. `pmset` renaming
`'AC Power'`, `ioreg` reordering `AppleClamshellState`, `xset` altering its DPMS
line — each turns the alarm into a no-op while every process-level test still
passes.

So the text-interpreting logic is extracted from each sensor into a pure
function and tested against captured real output stored in
`internal/monitor/testdata/`:

| Function                | Fixture source                                    |
| ----------------------- | ------------------------------------------------- |
| `parseACPower`          | `pmset -g batt` (macOS)                           |
| `parseClamshellState`   | `ioreg -r -k AppleClamshellState -d 1` (macOS)    |
| `parseDisplayPowerState`| `ioreg -c IODisplayWrangler` (macOS)              |
| `parseHIDIdleTime`      | `ioreg -c IOHIDSystem -d 4 -S` (macOS)            |
| `parseUSBProfile`       | `system_profiler SPUSBDataType` (macOS)           |
| `parseDPMSState`        | `xset q` (Linux)                                  |
| `parseLidState`         | `/proc/acpi/button/lid/LID0/state` (Linux)        |
| `parseACOnline`         | `/sys/class/power_supply/*/online` and `status` (Linux) |
| `parseLidStatusWMI`     | `MSAcpi_LidStatus` (Windows)                      |
| `parseUSBEventLine`     | the WMI event line the PowerShell helper emits (Windows) |

This is a genuine extraction, not a simulation: the same function the product
calls in production is called with input the OS really produced. Sensor structs
keep their current constructors and behaviour; no `probe` seams, no injected
intervals, no fake sensors.

---

## Coverage, stated honestly

What a green CI run will and will not prove:

- **Proven on all three platforms:** the binary builds, starts, pairs, arms,
  raises and dismisses an alarm, enforces auth lockout and the session cap,
  serves config, and shuts down cleanly.
- **Proven on Linux only:** five of six sensors detect real hardware changes.
- **Proven on Windows:** network detection; input detection if the runner
  permits it.
- **Proven on macOS:** network and screen detection.
- **Never proven by CI:** lid on any platform, power on Windows/macOS, USB on
  Windows/macOS, BLE transport, and audible alarm output. These are recorded in
  `docs/manual-verification.md` as a pre-release checklist to be run on real
  hardware.

## File layout

```
test/
  e2e/                     # Layer 0 — real process, all three OSes
    harness.go             #   build, start, isolate, parse key, terminate
    client.go              #   WebSocket client that plays the phone
    lifecycle_test.go      #   startup, shutdown, port release, event log
    pairing_test.go        #   auth, lockout, session cap
    alarm_test.go          #   arm, trigger, alarm_active, disarm, PIN
    config_test.go         #   get/update/reset round-trip
  sandbox/
    linuxvm/
      run.sh               #   host side: fetch image, cloud-init, boot QEMU
      cloud-init.yaml
      README.md            #   how to run it locally
      scenarios_test.go    #   guest side, build tag `sandbox`
      hardware.go          #   modprobe helpers, skip-with-reason on failure
  realtrigger/             # Layer 2 — native runners
    trigger_windows.go
    trigger_darwin.go
    trigger_linux.go
    realtrigger_test.go
    matrix.go              #   emits the coverage matrix to the job summary
internal/monitor/
  *_parse.go               # Layer 3 — extracted pure parsers
  *_parse_test.go
  testdata/{linux,darwin,windows}/
docs/
  manual-verification.md   # what CI provably cannot cover
```

Build tags keep each layer out of the default `go test ./...` run: `e2e`,
`sandbox` and `realtrigger` respectively. A developer's plain `go test ./...`
stays fast and hardware-free.

## CI wiring

`.github/workflows/ci.yml` gains three jobs and loses one:

- `e2e` — matrix over ubuntu/windows/macos, runs the Layer 0 suite.
- `sandbox-linux` — ubuntu only, boots the QEMU VM and runs Layer 1. Verifies
  `/dev/kvm` is present first and fails loudly if the runner ever loses it,
  rather than quietly degrading.
- `realtrigger` — matrix over all three, runs Layer 2 and writes the coverage
  matrix to the step summary.
- `docker` — removed, along with its entry in `ci-success`.

`ci-success` is updated to `[format, typos, lint, test, e2e, sandbox-linux,
realtrigger, frontend, build, vulncheck]`. Because it already fails on skipped
jobs, none of the new jobs can be silently dropped later.

Timeouts: 15 minutes for `e2e` and `realtrigger`, 30 for `sandbox-linux` (image
download dominates; the image is cached by `actions/cache` keyed on its URL).

Make targets mirror CI: `test-e2e`, `test-sandbox`, `test-realtrigger`, and
`test-all` which runs what the current host supports and reports what it
skipped. The `docker` and `docker-run` targets are deleted.

## Risks

- **`/dev/kvm` availability.** GitHub's Azure-hosted Ubuntu runners currently
  expose it. The job asserts its presence rather than assuming it, so a change
  in runner policy surfaces as a clear failure instead of a silent skip.
- **Kernel module availability.** `test_power`, `uinput` and `dummy_hcd` ship in
  `linux-modules-extra`, which cloud-init installs. Any module that still fails
  to load skips its scenario with the modprobe error attached.
- **Stdout key parsing.** The dashboard's layout could change and break the
  regex. The failure mode is a loud harness error at the first test, not a false
  pass. If it proves brittle in practice, the fallback is a small
  machine-readable line behind a flag.
- **Windows `SendInput` in a runner session.** Unknown until measured; the first
  run decides whether it is a real trigger or a documented skip.
- **Timing flakiness.** Sensors poll on 1–3 second tickers, so assertions use a
  10-second deadline with polling rather than fixed sleeps.

## Out of scope, tracked as follow-ups

- A self-hosted runner on real laptop hardware for lid and charger coverage.
- BLE transport testing, which needs a real Bluetooth adapter.
- Audible alarm verification, which needs an audio device and a listener.
