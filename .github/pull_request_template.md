<!--
Thanks for working on LeaveSafe.

Keep this short. The diff says what changed; this says why, and what you did to
convince yourself it works.
-->

## What this changes

<!-- One or two sentences. What is different for someone running LeaveSafe? -->

## Why

<!--
The problem, not the patch. If it fixes an issue, "Fixes #123" is enough.

If it changes what LeaveSafe watches, what it reports, or what it trusts, say so
plainly here — those are the changes that need the most careful reading.
-->

## How it was verified

<!--
Which of these you actually ran, and on what. Half of this codebase sits behind
build tags, so "it compiles" says less here than it does elsewhere.
-->

- [ ] `go test ./...`
- [ ] `make check` (format, vet, lint, frontend, vulncheck, tests)
- [ ] `make test-e2e` — starts the real binary and drives the whole flow
- [ ] `make test-realtrigger` — fires the hardware changes this machine permits
- [ ] Tried by hand on: <!-- Windows 11 / macOS 15 / Ubuntu 24.04 -->

<!--
Touching a sensor, the alarm, or a platform backend? Say which platforms you ran
it on and which you did not. "Untested on macOS" is a useful thing to write, and
much better than leaving a reviewer to guess.
-->

## Notes for review

<!--
Anything that would take a reviewer a while to work out on their own: a
trade-off you made, a case you knowingly left unhandled, a piece you would like
a second opinion on.
-->

---

<!--
Two things CI will check that are easy to forget:

- Changed anything under web/src? Run `npm run build` in web/ and commit
  web/dist. The binary embeds that directory, and a stale build ships a UI that
  does not match its source.
- Added a config field, a message type, or a behaviour someone would notice?
  Add a line to CHANGELOG.md under Unreleased.
-->
