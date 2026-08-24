# SonarCloud maintainability cleanup: design

Date: 2026-08-05
Project: atakankizilyuce_LeaveSafe
Source: SonarCloud, 32 open MAINTAINABILITY issues, ~7h48min of effort

## Summary of decisions

- The PRs are opened against `main`, and **merging belongs to the user**. I do not merge.
- The two issues that change behaviour get PRs of their own, with the risk stated plainly.
- For the untested dev functions of `cmd/leavesafe/main.go`, a **characterization test comes
  first**, then extract-function. The test lands in its own commit.
- The two TODO issues (`internal/bluetooth/ble.go:23`, `ble_unsupported.go:27`, INFO) are **out
  of scope** and stay open.

## Step 0: get PR #43 back to green (clear the block)

`gh pr checks 43`: only SonarCloud is failing. The cause is not the issue count but
`new_duplicated_lines_density = 5.5` (the threshold is 3).

Source: the `allSensors` and `onlySensor()` blocks copied verbatim into
`test/realtrigger/realtrigger_test.go` and `test/sandbox/linuxvm/scenarios_test.go`, at 21 + 21
lines out of 767 new ones.

Fix: both files already import `test/harness`. Move the shared code into
`test/harness/sensors.go` as `AllSensors` / `OnlySensor()`.

Also: `cmd/leavesafe/dashboard_race_test.go:19` has complexity 22. It does not block the gate, but
once merged it becomes the 33rd issue. Fix it in the same PR.

## Steps 1-9: 30 issues, 9 PRs

| # | Branch | Scope | Issues |
|---|--------|-------|--------|
| 1 | `refactor/frontend-nested-ternaries` | 8x S3358 (Annunciator 88/90/92, app.tsx 281, ArmControl 106, StateHeader 25/42, Trace 53) + S7721 app.tsx:371 + S6606 app.tsx:376 | 10 |
| 2 | `refactor/phone-interface-complexity` | app.tsx:195 complexity 50 to 15 | 1 |
| 3 | `a11y/scrim-native-dialog` | Scrim.tsx role="dialog" to `<dialog>` (BEHAVIOUR) | 1 |
| 4 | `refactor/hub-complexity` | hub.go 1229 (53), 848 (36), 655 (23) | 3 |
| 5 | `refactor/hub-context-parameter` | hub.go:77 context field to a parameter (BEHAVIOUR) | 1 |
| 6 | `refactor/internal-complexity` | config.go:258 (21), safe.go:134 (16), server.go:530 (16), monitor/input_darwin.go:35 (16), update/watcher.go:80 (25), location/parse_linux.go:81 (23), location/tracker_test.go:52 (17) | 7 |
| 7 | `refactor/publicip-and-console` | publicip.go:121 (S3776 19 + S8209 parameter grouping), console_other.go:5 (S1186) | 3 |
| 8 | `refactor/alarm-complexity` | alarm.go:104 (26), after #43 | 1 |
| 9 | `refactor/main-startup-complexity` | main.go 1252 (113), 424 (55), 1041 (24) + characterization tests | 3 |

30 of 30 in total.

## Constraints

- **`web/dist` is committed**, and CI fails if `make web-verify` goes stale. PRs 1, 2 and 3 all
  change `web/dist/app.js`, so they are certain to conflict with each other. They have to be
  merged in order; rebase the next one once the previous has landed.
- PRs 4 and 5 touch the same file (hub.go), so they are sequential.
- PRs 6, 7, 8 and 9 are independent of one another.
- Commit message style: the repository does not use conventional commits. Full sentences,
  imperative mood, descriptive English subjects ("Let the siren be tested without a machine that
  shrieks"). NO AI attribution.

## Verification

Locally on every PR: `make fmt vet lint test`; on the ones that touch the frontend, additionally
`make web-lint web-verify`. Then wait for CI to go green and tell the user.
