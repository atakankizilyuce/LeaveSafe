# Manual verification

CI proves a great deal, but some hardware simply does not exist on a hosted
runner or inside a VM. This checklist covers the remainder. Run it on real
hardware before tagging a release and record the result in the release notes.

A green pipeline is not the same as full coverage, and this page is the
difference. Every gap listed here also appears in the coverage matrix each CI
run publishes to its job summary.

## What CI already proves, so you do not have to

- The binary builds, starts, pairs, arms, raises and clears an alarm, enforces
  the auth lockout and the three-session cap, serves and persists config, and
  shuts down cleanly, releasing its port — on Windows, Linux and macOS.
- In the Linux VM sandbox, real hardware changes are detected: charger removal
  through the `test_power` module, keyboard activity through `uinput`, USB
  attachment through `dummy_hcd`, screen blanking through a real X server, and
  IP changes.
- On Windows, real pointer activity moves `GetLastInputInfo` and the input
  sensor fires. Real IP changes are detected.
- On macOS, real display sleep and real IP changes are detected.
- Every OS-output parser is tested against output the operating systems really
  produced. See `internal/monitor/testdata/PROVENANCE.md`.

## What only you can check

| # | Check | Platform | Expected |
| - | ----- | -------- | -------- |
| 1 | Arm, then unplug the charger | Windows | Phone alerts within ~2 s; laptop alarm sounds |
| 2 | Arm, then unplug the charger | macOS | Phone alerts within ~2 s; laptop alarm sounds |
| 3 | Arm, then close the lid | Windows | Phone alerts within ~2 s |
| 4 | Arm, then close the lid | macOS | Phone alerts within ~2 s |
| 5 | Arm, then close the lid | Linux | Phone alerts within ~2 s |
| 6 | Arm, then unplug a USB stick | Windows | Phone alerts within ~1 s |
| 7 | Arm, then unplug a USB stick | macOS | Phone alerts within ~3 s |
| 8 | Arm, then lock the screen | Windows | Phone alerts; with auto-arm on, the system arms itself |
| 9 | Trigger an alarm and let it escalate | any | Volume rises through the configured levels and is audible |
| 10 | Pair over Bluetooth instead of Wi-Fi | any | Pairing succeeds and alerts arrive over BLE |
| 11 | Arm, then carry the phone out of Wi-Fi range | any | Alarm fires after the disconnect grace period |

## Why each of these is absent from CI

- **Charger.** Neither hosted runner has a battery: `(Get-WmiObject Win32_Battery).Count`
  returns 0 on the Windows runner, and `pmset -g batt` on the macOS runner never
  leaves AC power. The Linux VM covers this case through `test_power`.
- **Lid.** No runner has one, and QEMU x86 emulates no ACPI lid button, so even
  the VM cannot produce it. `ioreg -r -k AppleClamshellState` on the macOS
  runner returns nothing at all. This is the one sensor with no automated
  coverage on any platform.
- **USB on Windows and macOS.** No attachable device exists on a hosted runner;
  the captured `system_profiler SPUSBDataType` output is empty. The Linux VM
  covers this case through `dummy_hcd`.
- **Screen lock on Windows.** Locking or blanking the session would cut off the
  session the CI job itself runs in.
- **Audible alarm and volume escalation.** Runners have no audio device, and
  nothing could listen to it if they did.
- **Bluetooth.** No runner exposes a Bluetooth adapter, so the BLE transport is
  entirely unexercised by CI.
- **Disconnect grace period.** Needs a second physical device that can leave the
  network.

## Closing the gaps

The charger and lid cases could be automated with a self-hosted runner on a real
laptop, triggered manually before a release. That is deliberately not set up
here: it needs hardware only the maintainer has, and a half-configured runner
that silently skips would be worse than this checklist.
