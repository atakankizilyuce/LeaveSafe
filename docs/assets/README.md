# README assets

Everything in here is used by the top-level [README](../../README.md).

| File | What it is |
|------|------------|
| `hero.svg`, `flow.svg` | Hand-written animated SVGs (SMIL, no scripts, no external fetches) |
| `tui-*.png` | The real terminal dashboard, captured from a running binary |
| `phone-*.png` | The real phone interface, captured in a mobile viewport |
| `demo.gif` | One whole run — pair, arm, alert, dismiss, disarm — recorded frame by frame from a single session |

## How the screenshots were captured

They are of the real program, not mock-ups and not staged. The binary was
started with a throwaway config directory, a headless mobile browser paired with
it over the actual WebSocket protocol, and every state was reached by driving
the interface the way a person would: tapping *Arm*, waiting the countdown out,
dismissing the alert, holding to disarm.

**The alert is a real sensor event.** The input sensor reads
`GetLastInputInfo`, which only moves when the operating system's input queue
sees genuine keyboard or pointer activity — no event a browser can raise reaches
it. So the pointer really was moved, on a machine that really was armed, and the
alert travelled to the phone over the wire. Nothing was pushed at the phone to
make a picture.

**Nothing was faked to fill the panel.** The machine these were taken on has no
lid sensor, so LID reports itself unavailable — which is why the shots show five
sensors inside the shield and LID standing outside it with `no sensor on this
machine` written underneath. That is the panel doing its job, and it is worth
more in a README than a machine with a full set would have been.

The terminal shots are the binary's own output. It writes a cursor-addressed
ANSI screen; that stream was replayed through a terminal emulator to rebuild the
frame exactly as a terminal draws it, then drawn as a window. The colours are a
terminal theme rather than the product's palette — what the binary emits is the
sixteen colours every console has had since the 1980s, and which shade of cyan a
terminal draws for colour 36 is the terminal's business.

The pairing keys and the local address visible in the screenshots are per-run
values from a throwaway session. A new key is generated every time LeaveSafe
starts.
