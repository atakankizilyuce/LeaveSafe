# README assets

Everything in here is used by the top-level [README](../../README.md).

| File | What it is |
|------|------------|
| `hero.svg`, `flow.svg` | Hand-written animated SVGs (SMIL, no scripts, no external fetches) |
| `tui-*.png` | The real terminal dashboard, captured from a running binary |
| `phone-*.png` | The real phone interface, captured in a mobile viewport |
| `demo.gif` | The arm → alert → dismiss sequence, recorded frame by frame from the same session |

## How the screenshots were captured

The screenshots are of the real program, not mock-ups. The binary was started,
a headless mobile browser paired with it over the actual WebSocket protocol,
and every state was reached by driving the interface the way a person would —
tapping *Arm*, waiting out the countdown, dismissing the alert.

The alert visible in `phone-alarm.png`, `tui-armed.png` and `demo.gif` is a
genuine `power` sensor event: the charger's `online` value changed, the sensor
read it, and the alert travelled to the phone over the wire.

One thing was staged. A container has no battery, no lid and no `/dev/input`,
so six sensors would have reported themselves unavailable and the screenshots
would have shown an interface nobody will ever see. For the capture only, the
Linux sensors were pointed at a directory tree with the same layout as
`/sys/class/power_supply`, `/proc/acpi/button/lid`, `/sys/bus/usb/devices` and
`/dev/input`. The sensor code, the protocol, the server and the interface are
untouched and unmodified — only the paths they read from were redirected, and
that redirection exists nowhere in this repository.

The pairing keys and the `192.0.2.2` address visible in the terminal
screenshots are per-run values from a throwaway session. A new key is generated
every time LeaveSafe starts.
