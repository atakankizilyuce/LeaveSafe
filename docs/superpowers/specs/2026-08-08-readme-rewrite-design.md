# README rewrite — design

**Date:** 2026-08-08
**Scope:** Root `README.md` restructure, `docs/assets/hero.svg` touch-up.

## Problem

The root README is 341 lines and reads as an essay rather than a project page.
It opens with a scene ("You are in a café, a library, a co-working space"),
argues design decisions in prose ("Armed is not red", the four-line rationale
for macOS-only Bluetooth), and puts installation behind a heading called "Set it
up". A reader arriving from GitHub search wants to know what it is, whether it
runs on their machine, and how to install it — in that order, under the headings
they expect.

## Non-problem

`docs/` has no orphan files. All seven markdown files are linked:
`configuration`, `development`, `location`, `remote-access`, `service`,
`releasing` from the README; `manual-verification` from `development.md` and
`releasing.md`. `docs/superpowers/` is gitignored and never was in the repo.

## Decisions

- **Structure:** conventional open-source README. Header → intro → demo →
  Features → Installation → Quick start → Usage → How it works → Security →
  Documentation → Contributing → License.
- **Language:** English (the repo is English throughout).
- **docs/:** no file is moved, renamed or deleted. `manual-verification.md`
  gains a row in the README's documentation table.
- **Assets:** all fourteen images keep a home, so nothing in `docs/assets/`
  becomes dead weight.

## Cut from the README

Prose that argues a design decision rather than describing the product:

| Cut | Replaced by |
|---|---|
| The café/library opening scene | One sentence in the intro |
| "Armed is not red" rationale (5 sentences) | One bullet in Features |
| The macOS-only Bluetooth rationale (5 sentences) | One sentence + platform table |
| Luhn / lockout rationale paragraph | Already stated in the security table |
| Duplicate first-run remote-access note | The docs table row |

## hero.svg

Read in full before deciding. Three of the four planned touch-ups turned out to
be unnecessary:

- **Tagline sync** — the banner already reads `LEAVE YOUR LAPTOP. STAY SAFE.`
  and `NO CLOUD · NO ACCOUNTS · ONE QR SCAN`, which is what the new README says.
- **width/height removal** — the banner carries `width="1200" height="340"`,
  which is what gives it an intrinsic size. The README already constrains it
  with `width="100%"`. Removing the attributes would trade a real guarantee for
  a hypothetical renderer.
- **Grid contrast** — the banner paints its own near-black background, so it is
  independent of the viewer's GitHub theme. `stroke-opacity=".05"` is doing what
  it was set to do.

What it genuinely lacks:

- **`prefers-reduced-motion`.** A twelve-second SMIL loop runs forever with no
  way to stop it. A `<style>` block setting `display: none` on `animate`,
  `animateTransform` and `animateMotion` under the reduce query stops SMIL in
  every engine that implements it. Every element then holds its authored
  attribute values, which is the standby frame: wordmark, tagline, the readout
  reading `STANDBY · NOTHING IS WATCHED`, the laptop, and the six sensors at
  rest in their ring. A complete, honest frame — the state before you arm.
