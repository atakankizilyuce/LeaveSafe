# A landing page on GitHub Pages

**Date:** 2026-08-10
**Status:** Approved, ready for implementation

## The problem

LeaveSafe is explained in exactly one place: the README. A README speaks to a
developer — badges, `go test ./... -race`, a contributing section. It does not
answer the question a stranger arrives with, which is *what is this, and how do
I get it*, in the ten seconds before they leave.

There is also no link worth sharing. `github.com/atakankizilyuce/LeaveSafe` is a
repository address; it invites a judgement about the code, not about the
product.

## What we are building

**A demo page. Nothing on it has to actually work.**

One page at `https://atakankizilyuce.github.io/LeaveSafe/`, built from a new
`site/` directory. Three jobs, in this order: catch the visitor, explain the
product in a few seconds, show them how to download it.

It is not documentation — the README and `docs/` keep that. It is not a
showcase of the engineering. And it does not talk to anything.

Language: English, matching the product interface and the README.

## No build step

`site/index.html`, `site/style.css`, `site/demo.js`. No framework, no
`package.json`, no `npm install`, no bundler.

This was not the first plan. An earlier version of this design ran the *real*
panel — importing `web/src/components/*` and feeding them a simulated transport
— so the demo could never drift from the product. That is a good idea for a page
that has to prove something. This page does not have to prove anything, and the
cost of the idea was a second node project, a CI step, a browser test, and a
constraint against `localStorage` that the product's own `appendLog` violated.
All of it bought fidelity that a visitor deciding whether to download cannot
perceive.

Every path is relative, so there is no base-path configuration to get wrong.

## The hero: a panel that is pretending

The visitor sees something that looks like the phone interface and can be
played with:

| Action | What happens |
|---|---|
| **Arm** | A three-second countdown. The sensors travel into the shield. The page climbs from standby near-black to armed blue. |
| **Trigger** | A red alert covers the panel and the station that fired lights red. |
| **Dismiss / disarm** | Back to standby, the fired station still lit. |

It is CSS transitions and a couple of hundred lines of vanilla JavaScript.
Nothing is connected to anything.

**It has to say it can be touched.** The first build's only cue sat under the
panel — "a demonstration, nothing here is connected to anything" — which reads
as *do not bother*, and nobody bothered. So the invitation goes above the panel
instead ("Try it — this panel is live"), the arm button breathes until it is
tapped once, and the note underneath tells the visitor what to tap rather than
what the page is not.

**The shield contains what it claims to.** It is drawn large enough to hold the
eye, the word and all six sensor icons with room to spare, and the content is
pushed up out of the pointed lower third, which holds nothing. An earlier
version was sized to the words and left the sensor icons riding its lower edge
— a shield that does not contain the things inside it argues against the one
idea the picture exists to carry. While armed, a sensor that fires lights in
that row rather than out in the ring, which by then is empty.

**Colour is the whole point of the trick.** The product's own rule is that state
is the colour of the entire page rather than a badge, driven by
`:root[data-state]` in `web/src/styles/tokens.css`. The mock sets the same
attribute on the same element, so arming repaints the nav, the headline, the
install commands and the footer along with the panel. That is a rule worth
copying — six CSS declarations — not a system worth importing.

Accuracy note: the product does **not** paint the whole page red on an alarm.
`data-state` takes standby, arming and armed; the alarm is a full-screen overlay
and a red station. The mock does the same. Red appears in one place, which is
what makes it mean something.

Under `prefers-reduced-motion: reduce` the transitions collapse to 1ms, as
`tokens.css` already does.

## Page structure

| # | Section | Content |
|---|---|---|
| 1 | Hero | Headline, the one-sentence promise, the mock panel, `↓ Download`, the platform line |
| 2 | What it does | Three steps — run it, scan the QR code, arm it — then the six sensors, one line each |
| 3 | Get it | brew / winget / scoop / direct binary, each command copyable. A link to `gh attestation verify` in the README rather than an explanation here |
| 4 | Footer | GitHub, the `docs/` pages, Apache-2.0. No version number — a static page cannot keep one true, and the download link points at `/releases/latest` |

Kept from the longer design, in one line rather than a section: *the phone is
the convenience, not the alarm — the laptop sounds regardless*. The README's
most trustworthy habit is stating its own limits, and it costs a sentence.

## Look

The palette, type scale and easing curves are lifted from
`web/src/styles/tokens.css` — the standby/armed rooms, `--brand #3d8fc4`,
`--trip #f04438`, the `cubic-bezier(0.32, 0.72, 0, 1)` curve. Copied as a small
subset into `site/style.css`, not imported: the page needs perhaps twenty of
those values and none of `panel.css`'s 1,433 lines.

No web fonts, matching the product's own decision.

21st.dev is used for the structure and motion of individual pieces — the install
tabs, the copy control, section reveals — translated into this page's CSS.

## Deployment

`.github/workflows/pages.yml`, on push to `main` when `site/**` or
`docs/assets/**` changed, plus `workflow_dispatch`. It builds nothing: it copies
the handful of images it needs out of `docs/assets/` next to `site/`, uploads
the result, and deploys. Copying at deploy time rather than committing a second
copy keeps one source of truth for those files.

It follows `ci.yml`'s standing rule: every `uses:` pinned to a commit SHA with
the version in a trailing comment. Permissions are `pages: write` and
`id-token: write` and nothing else.

**One manual step, which must be done by the repository owner:** Settings →
Pages → Source = *GitHub Actions*.

## Verification

1. The page is served and the deployed URL returns 200.
2. Arm, trigger, dismiss all behave — checked by hand in a browser, and with a
   Playwright pass over the same three actions.
3. No broken image or link, including the `docs/` links pointing at GitHub.
4. Nothing overflows at a 360px viewport.
5. Under `prefers-reduced-motion: reduce` the page settles rather than animating.

## Out of scope

- Any real behaviour. Nothing connects to anything.
- Publishing `docs/*.md` as web pages. The page links to them on GitHub.
- A custom domain. One DNS record whenever it is wanted.
- Analytics of any kind. The product ships no telemetry; its page will not either.
- A Turkish translation. English only, revisitable later.
