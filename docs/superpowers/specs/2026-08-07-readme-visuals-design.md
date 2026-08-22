# README visuals: a new banner and an honest demo

Date: 2026-08-07
Branch: `fix/panel-jump-and-demo-quality`

## Why

Both pictures at the top of the README are wrong, and one of them is broken.

**`hero.svg`.** Four defects, all visible in a rendered frame:

1. The six sensor tiles drawn inside the shield are not centred in it. The top
   row spans `x -33..21` and the bottom row `x -43..27`, so the group sits about
   seven units left of the shield's axis. The bottom row (`y 43..59`) also
   reaches into the shield's taper and crowds its point.
2. There is a dead beat around `t=5.2s`. The six sensors reach the centre and
   fade to nothing at keyTime `0.42–0.48`; the shield does not appear until
   `0.50`. For most of a second the right half of the banner is empty while the
   readout claims `ARMED · WATCHING 6 SENSORS`.
3. On the alert beat the INPUT node returns to `(811.5, 106)`, which lands on
   the shield's left edge. The two shapes collide.
4. The text block ends near `x 640` and the orbit starts at `x 800`. Five
   hundred units of the banner carry nothing.

**`demo.gif`.** It is not merely choppy, it is mis-assembled. Frames were
captured full-page against a page whose height changes with state, then pasted
into a fixed 320x693 canvas, so the content drifts vertically between frames.
The GIF is then diff-encoded with disposal `1` (do not dispose), so nothing ever
clears: a single composited frame shows the "phone sleeps" banner twice at two
offsets, and `Cancel` and `Hold to disarm` sharing one frame. It runs 148 frames
over 18.4s — about 8 fps.

## What we are building

### 1. `docs/assets/hero.svg` — a new composition

Canvas grows to 1200x340. Still hand-written SMIL: no scripts, no external
fetches, because GitHub will not render an SVG that needs either.

Layout: the text block stays left (`x 64..600`); the right half becomes a scene
(`x 640..1140`) with a laptop at roughly `x 830` and a phone at roughly
`x 1060`. The dead space becomes the scene.

Twelve second loop:

| Beat | What happens |
|------|--------------|
| 0–2.5s standby | Laptop open, screen dark. Six dim sensor pips along its edges (POWER, LID, USB, SCREEN, NETWORK, INPUT). Phone dark. `STANDBY · NOTHING IS WATCHED` |
| 2.5–4.5s arming | A hairline link draws between laptop and phone. The pips turn blue one after another while the shield outline starts drawing *around* the laptop |
| 4.5–7.5s armed | Shield closed around the laptop, calm blue, faint breathing. Scan sweep. Small blue `ARMED` chip on the phone. `ARMED · WATCHING 6 SENSORS` |
| 7.5–9.8s alert | The INPUT pip flares red, a pulse travels the link to the phone, the phone floods red and shakes slightly, the shield turns red. `ALERT · SOMEBODY TOUCHED IT` |
| 9.8–12s | Back to blue, then to standby |

This removes all four defects by construction rather than by adjustment. The
sensors never pile onto one point, so there is no centring to get wrong; the
shield closes around them while they are still visible, so there is no dead
beat; the laptop and the phone have separate fixed footprints, so nothing
collides.

It also shows the half of the product the current banner omits — the phone is
what actually raises the alarm, and it never appears.

Colours stay the product's own, from `web/src/styles/tokens.css`: `#3d8fc4`
brand, `#f04438` trip, `#0a0c11`/`#050609` panel and void, and the armed page's
bluer `#0a1b2d`/`#050e19`. Red still appears on exactly one beat.

Easing stays `calcMode="spline"` with `keySplines="0.32 0.72 0 1"` — the same
curve `--state-shift` uses in the panel — and holds stay linear.

### 2. `docs/assets/demo.gif` — a clean recording

Same story, told properly: pair, arm, the sensors gathering into the shield, the
alert arriving, dismissing it, disarming.

Recording keeps the property `docs/assets/README.md` claims, because that claim
is worth more than the picture: a real `leavesafe.exe` on a throwaway config
directory, a headless mobile browser paired over the actual WebSocket protocol,
and an alert that is a genuine operating-system input event rather than
something pushed at the phone.

Three changes kill the drift:

1. **Fixed viewport, no full-page capture.** This is the drift's cause.
2. **Fixed-cadence capture at 20 fps** through CDP screencast, rather than a
   screenshot loop whose period wanders.
3. **Full frames with correct disposal** in the encoder. The ghosts come from
   diff frames that never clear. A single global palette, sampled across every
   frame, is enough for an interface this flat.

Output: 320x693, about 13s, 20 fps (~260 frames), target 2 MB or under. The idle
stretches shrink; the beats stay.

No new dependencies. Playwright and a Chromium build are already in the npx
cache; the GIF encoder is Go's `image/gif`, and this is a Go repository.

## Constraints

- The recording machine must be left alone while it runs. The input sensor reads
  `GetLastInputInfo`, so a stray mouse move fires the alarm early.
- `hero.svg` must stay script-free and self-contained.
- `docs/assets/README.md` must keep describing what was actually done.

## Out of scope

The `phone-*.png` and `tui-*.png` stills, and `flow.svg`. They are not what was
reported and they render correctly.
