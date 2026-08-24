# Carrying all three platforms' coverage profiles to Sonar (Phase 0)

Date: 2026-08-07
Scope: phase 0 only. Phases 1-3 are outside this PR, noted at the end.

## Problem

The test matrix in `ci.yml` produces coverage on all three operating systems:

```
run: go test ./... -v ${{ matrix.race }} -count=1 -coverprofile=coverage.out -covermode=atomic
```

But the upload step is conditional on `if: matrix.os == 'ubuntu-latest'` (`ci.yml:155-162`). The
macOS and Windows profiles are deleted along with the runner when the job ends. Sonar sees only
the Linux profile, so every `_darwin.go` and `_windows.go` file counts as 0% covered.

That means tests which have already been written do not show up. Measured, from a local run on
Windows:

| File | Actually, on Windows | As Sonar sees it |
|---|---|---|
| `internal/location/parse_windows.go` | 24/24 100% | 0% |
| `internal/monitor/parse_windows.go` | 9/9 100% | 0% |
| `internal/monitor/powershell_windows.go` | 2/2 100% | 0% |
| `internal/monitor/lid_windows.go` | 6/29 20.7% | 0% |

`parse_windows_test.go`, `parse_darwin_test.go`, `service_darwin_test.go` and
`volume_windows_test.go` all exist and all pass; their results are thrown away.

## Expected gain

The direct gain, as far as it was measured, is modest: around 38 genuinely covered lines on the
Windows side, an estimated 35-60 on darwin. Together **+75-100 lines = +1.0-1.3 points**
(73.8% to ~75%).

The size of the gain is not the real argument. Until this is fixed, writing a platform-specific
test earns nothing in Sonar at all. It is the precondition for phases 1-3.

## Design

### Decision: the merge is not left to Sonar

`sonar.go.coverage.reportPaths` accepts several comma-separated paths, but when the same file
appears in two reports it is undocumented whether the result is a union or an overwrite. The Sonar
team's own phrasing is *"we go 100% by what the coverage reports tell us"*. Since portable files
will appear in all three profiles, the wrong semantics could **lower** coverage.

So the three profiles are reduced to one file by us, in the sonar job.
`sonar-project.properties` does not change at all: it still reads a single `coverage.out` at the
root.

### The merge

A Go profile line reads `<import-path>/file.go:122.24,163.2 1 29`,
so `$1` is the block position, `$2` the statement count, `$3` the execution counter.

The key is `$1 $2`, the counters are summed, and first-seen order is preserved:

```bash
{
  echo "mode: atomic"
  awk 'FNR > 1 {
         key = $1 " " $2
         count[key] += $3
         if (!(key in seen)) { seen[key]; order[++n] = key }
       }
       END { for (i = 1; i <= n; i++) print order[i], count[order[i]] }' \
    coverage-linux.out coverage-macos.out coverage-windows.out
} > coverage.out
```

Platform-specific files appear in exactly one profile, so their blocks cannot collide. Portable
files appear in all three with identical block boundaries: the source file is the same, so the
`file:startLine.col,endLine.col` keys match exactly. Build-tagged code is separated at file level,
which makes a partial block collision impossible.

### Verification (done)

Two overlapping profiles were produced and merged:

```
unique blocks: a=1363 b=101  a|b=1363  merged=1363
count mismatch: 0
covered: a=711 b=78  union=755  merged=755
OK: block set and covered set are exactly the union
```

`go tool cover -func=merged.out` parsed the output without complaint. Also worth recording:
`part-a.out` had 1974 lines but contained 1363 unique blocks. Go itself emits repeated blocks for
each test binary, and `go tool cover` reads them by summing. The script reproduces that same
behaviour.

### Changes to `ci.yml`

**test job**

- A `label` field is added to the matrix: `linux` / `macos` / `windows`. Using `matrix.os` would
  have named the file `coverage-ubuntu-latest.out`; the `lint` job already follows the same
  pattern with `goos`.
- The `Coverage summary` and `Coverage HTML report` steps stay Linux-only and run **before the
  rename**, because both of them read `coverage.out`.
- New step: `mv coverage.out coverage-${{ matrix.label }}.out`.
- The `if:` condition comes off the `Upload coverage report` step, the artifact is named
  `coverage-report-${{ matrix.label }}`, and it carries the profile only.
- `coverage.html` is split into its own artifact (`coverage-html`, Linux-only). Otherwise the
  macOS and Windows legs would be listing a path that does not exist there.

**sonar job**

- A single `download-artifact` step with `pattern: coverage-report-*` and `merge-multiple: true`
  brings all three profiles down to the root (v8.0.1 supports this).
- The merge step above is added.
- `actions/setup-go` is added, and `go tool cover -func=coverage.out | tail -1` prints the real
  combined total into the step summary. Today the summary shows only Linux's number, which means
  the true cross-platform figure appears nowhere.

**The trap: the `coverage.html` exclusion**

A comment in `sonar-project.properties` records that `coverage.html`, if analysed, is enough on
its own to fail the quality gate (`go tool cover -html` embeds the entire source tree). The
`coverage.html` pattern in `sonar.exclusions` matches only at the root. Hence:

- the profiles are downloaded to the root, not into a subdirectory;
- `coverage.html` is never downloaded into the sonar job at all.

The exclusion line stays regardless, so that anyone who runs `make cover` and then tries the
scanner locally does not fall into the same trap.

**Failure behaviour**

If a leg produces no profile, awk fails on the missing file and the job goes red. That is
preferable to silently reporting low coverage. `fail-fast: false` is already in place and sonar
waits for all three legs through `needs: [test, frontend]`; those parts do not change.

The `needs` list of `ci-success` does not change; sonar is deliberately left out of it (forks have
no secrets).

## Success criteria

After the merge, in the PR's Sonar analysis:

1. Overall coverage must **rise**, to somewhere around 74.8-75.1%. If it falls, the merge
   semantics are wrong.
2. `internal/location/parse_windows.go` must go from 0% to ~100%.
3. `internal/monitor/parse_windows.go` must go from 0% to ~100%.
4. `internal/monitor/parse_darwin.go` must come off 0%.
5. No portable file's coverage may **fall**.

Because 2-4 are directly observable, whether this PR actually worked is something you can see
rather than guess.

## Out of scope

Phases 1-3 are separate PRs. The measured gap, recorded here for later reference:

| Group | Uncovered | Coverage |
|---|---|---|
| Go, portable `internal/` | 583 | 82.1% |
| Go, linux-only | 438 | 20.4% |
| Go, darwin-only | 298 | 0.0% |
| Web (vitest) | 281 | 87.4% |
| Go, windows-only | 249 | 0.0% |
| Go, portable `cmd/` | 246 | 77.4% |

The cheapest next targets (all at 0%): `cmd/leavesafe/cli.go` (56 lines, pure string formatting),
`internal/auth/keyfile.go` (21, file I/O), `internal/monitor/availability.go` (25),
`internal/network/publicip.go` (50), and the setters of `internal/ws/hub.go` plus `RunHeartbeat`
and `dropExpiredSessions`.

With the structure as it stands, a healthy ceiling is around 85-88%. Above that should not be
chased: `ble_darwin.go` + `clients_darwin.go` (100 lines, CoreBluetooth/CGO), the
install/uninstall paths of `service_*.go`, and the wiring in `main()`, some 250-350 lines.
Testing those would be testing the mock.
