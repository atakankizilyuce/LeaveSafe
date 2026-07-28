# Fixture provenance

These files are the input to the sensor output parsers. A parser test is only
worth something if its input is what the operating system really produces, so
every file here records where it came from.

Files are byte-exact: no header, no marker, nothing a parser would have to strip.
Provenance lives in this table instead.

Refresh the captured ones with the **Capture Fixtures** workflow
(`.github/workflows/capture-fixtures.yml`), then download its artifacts.

## Captured from a real machine

| File | Source | Captured on |
| --- | --- | --- |
| `linux/ac_online_1.txt` | `/sys/class/power_supply/usb/online` | live Linux kernel (WSL2) |
| `linux/ac_online_0.txt` | `/sys/class/power_supply/ac/online` | live Linux kernel (WSL2) |
| `linux/battery_status_full.txt` | `/sys/class/power_supply/battery/status` | live Linux kernel (WSL2) |
| `linux/power_supply_type_mains.txt` | `/sys/class/power_supply/ac/type` | live Linux kernel (WSL2) |
| `linux/xset_q_on.txt` | `xset q` | `ubuntu-latest` runner under Xvfb |
| `darwin/pmset_ac.txt` | `pmset -g batt` | `macos-latest` runner |
| `darwin/ioreg_display.txt` | `ioreg -r -d 1 -c IODisplayWrangler` | `macos-latest` runner |
| `darwin/ioreg_hid_idle.txt` | `ioreg -c IOHIDSystem -d 4 -S` | `macos-latest` runner |
| `darwin/ioreg_clamshell_absent.txt` | `ioreg -r -k AppleClamshellState -d 1` | `macos-latest` runner |
| `darwin/system_profiler_usb_empty.txt` | `system_profiler SPUSBDataType -detailLevel mini` | `macos-latest` runner |
| `windows/battery_count_none.txt` | `(Get-WmiObject Win32_Battery).Count` | `windows-latest` runner |
| `windows/lid_status_unavailable.txt` | `MSAcpi_LidStatus` query, which threw | `windows-latest` runner |
| `windows/logonui_absent.txt` | `(Get-Process LogonUI) -ne $null` | `windows-latest` runner |

Two of these are empty files, and that is the real result rather than a failed
capture: the macOS runner has no clamshell sensor and no attached USB device.
The PowerShell captures were written by `Out-File -Encoding utf8`, which emits a
UTF-8 BOM and CRLF; both were stripped, because `exec.Command(...).Output()`
never sees either in production.

## Written by hand from the documented format

No hosted runner or VM can produce these states, so they were written from the
documented output format rather than captured. They are the reason
`docs/manual-verification.md` exists.

| File | Format source | Why it could not be captured |
| --- | --- | --- |
| `linux/battery_status_discharging.txt` | `Documentation/ABI/testing/sysfs-class-power` | the runner exposes no power supply at all |
| `linux/lid_state_open.txt` | `Documentation/acpi/button.txt` | no ACPI lid button on any runner or on QEMU x86 |
| `linux/lid_state_closed.txt` | `Documentation/acpi/button.txt` | as above |
| `linux/xset_q_dpms_off.txt` | `xset(1)` | Xvfb on the runner reports "Server does not have the DPMS Extension" |
| `darwin/pmset_battery.txt` | `pmset(1)` | the macOS runner has no battery |
| `darwin/ioreg_clamshell_open.txt` | IOKit `AppleClamshellState` | the macOS runner has no clamshell |
| `darwin/ioreg_clamshell_closed.txt` | IOKit `AppleClamshellState` | as above |
| `darwin/ioreg_display_asleep.txt` | IOKit `IOPowerManagement` | a headless runner never sleeps its display |
| `windows/lid_status_open.txt` | `MSAcpi_LidStatus` | the runner has no lid, so the WMI class is absent |
| `windows/lid_status_closed.txt` | `MSAcpi_LidStatus` | as above |
| `windows/battery_count_one.txt` | `Win32_Battery` | the runner has no battery |
| `windows/logonui_present.txt` | `Get-Process LogonUI` | locking the runner session would break the runner |
