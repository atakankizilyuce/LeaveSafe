# Carrying Three Platforms' Coverage Profiles to Sonar — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Get the coverage profiles that the macOS and Windows test legs produce, and currently throw away, through to Sonar, so that the tests already written for platform-specific files are counted.

**Architecture:** All three legs of the test matrix upload their profile as an artifact named after the platform. The sonar job downloads all three and merges them into a single `coverage.out` by summing the per-block counters. The merge is not left to Sonar; `sonar-project.properties` does not change at all.

**Tech Stack:** GitHub Actions, `go test -coverprofile`, `go tool cover`, awk.

## Global Constraints

- The repository's commit style is **not conventional commits**: sentence case, imperative, a single descriptive line. For example: `Keep the hook's count safe from a goroutine still logging`. Do not use a `feat:`/`fix:` prefix.
- Do not add AI attribution to commit messages (no `Co-Authored-By`, no `Generated with...`).
- Do not put a Claude Code session link in a commit message, a branch name, or a PR title or body.
- Every action in `ci.yml` is pinned to a full SHA; in new steps **copy the existing SHAs verbatim** rather than writing a version tag.
- Workflow comments are in English and in the detailed, explanatory register of the existing ci.yml.
- `sonar-project.properties` **does not change** in this plan.
- `docs/superpowers/` is in gitignore, so this plan and its spec are not committed.

---

### Task 1: Prove the merge logic locally

This task writes nothing into the repository. Its purpose is to see that the awk we are about to put into CI really does produce a union, and that `go tool cover` can read its output. If the merge is wrong, the coverage of portable files drops, and noticing that in CI would be far too late.

**Files:**
- Create (scratch, outside the repo): `$SCRATCH/merge-coverage.sh`, `$SCRATCH/verify-merge.py`

**Interfaces:**
- Produces: the awk program that Task 2 will embed in `ci.yml`, character for character the same text.

- [ ] **Step 1: Produce two overlapping profiles**

Run this at the repository root.

```bash
SCRATCH="C:/Users/ataka/AppData/Local/Temp/claude/C--workSpace-LeaveSafe/4b0ce090-c164-4e16-9743-e701a9c77311/scratchpad"
go test ./internal/ws/... ./internal/auth/... \
  -count=1 -coverpkg=./... -covermode=atomic -coverprofile="$SCRATCH/part-a.out"
go test ./internal/config/... ./internal/state/... \
  -count=1 -coverpkg=./... -covermode=atomic -coverprofile="$SCRATCH/part-b.out"
```

Both profiles contain the same portable files with different counters, which is exactly what the three platform profiles in CI do for portable files.

- [ ] **Step 2: Write the merge script**

`$SCRATCH/merge-coverage.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail
out=$1; shift
{
  echo "mode: atomic"
  awk 'FNR > 1 {
         key = $1 " " $2
         count[key] += $3
         if (!(key in seen)) { seen[key]; order[++n] = key }
       }
       END { for (i = 1; i <= n; i++) print order[i], count[order[i]] }' "$@"
} > "$out"
```

A profile line has the form `<import-path>/file.go:122.24,163.2 1 29`: `$1` is the block position, `$2` the statement count, `$3` the execution counter. The key is `$1 $2`, the counters are summed, and first-seen order is preserved.

- [ ] **Step 3: Write the verification script**

`$SCRATCH/verify-merge.py`:

```python
import sys

def load(path):
    blocks = {}
    for line in open(path):
        if line.startswith('mode:'):
            continue
        loc, stmts, count = line.split()
        blocks[(loc, stmts)] = blocks.get((loc, stmts), 0) + int(count)
    return blocks

a, b, m = (load(p) for p in sys.argv[1:4])
covered = lambda d: {k for k, v in d.items() if v > 0}

assert set(m) == set(a) | set(b), 'BLOCKS LOST OR GAINED'
assert not [k for k in m if m[k] != a.get(k, 0) + b.get(k, 0)], 'COUNTER SUM IS WRONG'
assert covered(m) == covered(a) | covered(b), 'COVERED SET IS NOT THE UNION'

print('blocks: a=%d b=%d union=%d merged=%d' % (len(a), len(b), len(set(a) | set(b)), len(m)))
print('covered: a=%d b=%d union=%d merged=%d'
      % (len(covered(a)), len(covered(b)), len(covered(a) | covered(b)), len(covered(m))))
print('OK: block set and covered set are exactly the union')
```

- [ ] **Step 4: Merge and verify**

```bash
bash "$SCRATCH/merge-coverage.sh" "$SCRATCH/merged.out" "$SCRATCH/part-a.out" "$SCRATCH/part-b.out"
python "$SCRATCH/verify-merge.py" "$SCRATCH/part-a.out" "$SCRATCH/part-b.out" "$SCRATCH/merged.out"
```

Expected: all three asserts pass, and the last line is `OK: block set and covered set are exactly the union`.

If an assert fails, do not move on to Task 2. It means the awk is wrong and coverage will drop in CI.

Note: `part-a.out` will have more lines than it has unique blocks. That is normal; `go test` emits repeated blocks for each test binary and `go tool cover` reads them by summing. The script reproduces the same behaviour.

- [ ] **Step 5: Can `go tool cover` read the output**

From the repository root, not from the scratch directory, because `go tool cover` has to find the source tree and `go.mod`:

```bash
go tool cover -func="$SCRATCH/merged.out" | tail -1
```

Expected: a single line of the form `total: (statements) NN.N%`, with no error.

- [ ] **Step 6: No commit**

This task stayed in scratch and changed nothing in the repository. `git status` should be clean.

---

### Task 2: Collect and merge the three profiles in `ci.yml`

**Files:**
- Modify: `.github/workflows/ci.yml:120-128` (the test matrix), `:141-163` (the coverage steps), `:503-506` (the sonar job's download)

**Interfaces:**
- Consumes: the awk program verified in Task 1.
- Produces: a `coverage.out` at the root of the sonar job's working directory. The `sonar.go.coverage.reportPaths=coverage.out` in `sonar-project.properties` reads it, and does not change.

The test job and the sonar job have to change in **a single commit**. If the artifact name changes while the sonar job goes on looking for the old one, CI stays broken on the branch.

- [ ] **Step 1: Add a `label` field to the matrix**

`.github/workflows/ci.yml:120-128`, as it stands:

```yaml
      matrix:
        include:
          - os: ubuntu-latest
            # -race needs cgo, which is only guaranteed present on the Unix runners.
            race: "-race"
          - os: macos-latest
            race: "-race"
          - os: windows-latest
            race: ""
```

The new version:

```yaml
      matrix:
        include:
          - os: ubuntu-latest
            # -race needs cgo, which is only guaranteed present on the Unix runners.
            race: "-race"
            label: linux
          - os: macos-latest
            race: "-race"
            label: macos
          - os: windows-latest
            race: ""
            label: windows
```

There is a separate `label` rather than reusing `matrix.os` because we want the file called `coverage-linux.out` instead of `coverage-ubuntu-latest.out`; the `lint` job already follows the same pattern with `goos`.

- [ ] **Step 2: Say which platform the Linux summary describes**

`.github/workflows/ci.yml:146`, the current line:

```yaml
          echo "### Coverage: $total" >> "$GITHUB_STEP_SUMMARY"
```

The new version:

```yaml
          echo "### Coverage (linux leg only): $total" >> "$GITHUB_STEP_SUMMARY"
```

This step is Linux-specific and stays that way. The real combined figure for all three platforms is printed in the sonar job in Step 5; the two should not be confused for one another.

- [ ] **Step 3: Rename the profile after the platform and upload it on every leg**

`.github/workflows/ci.yml:155-163`, as it stands:

```yaml
      - name: Upload coverage report
        if: matrix.os == 'ubuntu-latest'
        uses: actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a # v7
        with:
          name: coverage-report
          path: |
            coverage.out
            coverage.html
          retention-days: 14
```

The new version. The rename has to come **after** the `Coverage HTML report` step, because that step still reads `coverage.out`:

```yaml
      # Every leg produces a profile, but only for the files its GOOS compiles.
      # Naming them apart is what lets the sonar job download all three side by
      # side; left as coverage.out they would overwrite one another.
      - name: Name the profile after the platform that produced it
        shell: bash
        run: mv coverage.out coverage-${{ matrix.label }}.out

      - name: Upload the coverage profile
        uses: actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a # v7
        with:
          name: coverage-report-${{ matrix.label }}
          path: coverage-${{ matrix.label }}.out
          retention-days: 14

      # The HTML report is for whoever is reading the run, and it is kept in its
      # own artifact so that the sonar job never downloads it. Analysing it fails
      # the quality gate on its own — see the coverage.html note in
      # sonar-project.properties.
      - name: Upload the coverage HTML report
        if: matrix.os == 'ubuntu-latest'
        uses: actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a # v7
        with:
          name: coverage-html
          path: coverage.html
          retention-days: 14
```

`shell: bash` is required: the default shell on the Windows runner is PowerShell, where `mv` is not the same thing.

- [ ] **Step 4: Turn the sonar job's download step into three profiles**

`.github/workflows/ci.yml:503-506`, as it stands:

```yaml
      - name: Fetch the coverage profile from the Linux test run
        uses: actions/download-artifact@3e5f45b2cfb9172054b4087a40e8e0b5a5461e7c # v8.0.1
        with:
          name: coverage-report
```

The new version:

```yaml
      - name: Set up Go
        uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7
        with:
          go-version-file: go.mod

      - name: Fetch every platform's coverage profile
        uses: actions/download-artifact@3e5f45b2cfb9172054b4087a40e8e0b5a5461e7c # v8.0.1
        with:
          pattern: coverage-report-*
          merge-multiple: true
```

`merge-multiple: true` unpacks the contents of all three artifacts into the same directory, which is the repository root. The paths in the profile lines are import paths, so where the file sits does not matter; but keeping it at root level also leaves the root-matching patterns in `sonar.exclusions` intact.

`Set up Go` is new: Step 5 runs `go tool cover`, and there is no Go in the sonar job today.

- [ ] **Step 5: Add the merge step**

Immediately after the download step from Step 4, and before the `Fetch the phone interface's coverage` step:

```yaml
      - name: Merge the three profiles into the one Sonar reads
        shell: bash
        run: |
          # Each leg only compiled its own GOOS, so a platform file appears in
          # exactly one profile while portable files appear in all three with
          # identical block boundaries. Summing the counters per block is
          # therefore a plain union, and it is what `go tool cover` already does
          # with the duplicate blocks `go test` emits per test binary.
          #
          # Sonar accepts several report paths, but whether it unions or
          # overwrites a file present in more than one of them is undocumented,
          # and guessing wrong would silently lower every portable file. Merging
          # here keeps that decision in the repository.
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

          total=$(go tool cover -func=coverage.out | tail -1 | awk '{print $3}')
          echo "### Coverage across all three platforms: $total" >> "$GITHUB_STEP_SUMMARY"
```

If a leg has produced no profile, awk stops on the missing file and the job goes red. That is preferable to silently reporting low coverage.

- [ ] **Step 6: Validate the YAML**

```bash
python -c "import yaml,sys; yaml.safe_load(open('.github/workflows/ci.yml')); print('yaml ok')"
```

Expected: `yaml ok`.

- [ ] **Step 7: Check that no reference to the old artifact name is left**

```bash
grep -n "coverage-report\|coverage\.out\|coverage\.html" .github/workflows/ci.yml
```

Expected: `name: coverage-report` (hyphenated, no suffix) must **not appear at all**; only `coverage-report-${{ matrix.label }}` and `pattern: coverage-report-*` should show up.

- [ ] **Step 8: Commit**

```bash
git checkout -b ci/merge-platform-coverage
git add .github/workflows/ci.yml
git commit -m "Let the macOS and Windows coverage reach Sonar instead of being thrown away"
```

---

### Task 3: Open the PR and verify the real Sonar result

The output of this task is not code but evidence. Whether the change worked is something you establish by looking at Sonar, not by guessing.

**Files:** none.

**Interfaces:**
- Consumes: the commit from Task 2.

- [ ] **Step 1: Push and open the PR**

```bash
git push -u origin ci/merge-platform-coverage
gh pr create --base main \
  --title "Let the macOS and Windows coverage reach Sonar instead of being thrown away" \
  --body "$(cat <<'EOF'
The test matrix runs on all three platforms and every leg writes a coverage
profile, but only the Linux leg uploaded one. macOS and Windows profiles were
discarded with the runner, so Sonar counted every `_darwin.go` and
`_windows.go` file as 0% covered — including files that already have passing
tests.

Measured on Windows locally: `internal/location/parse_windows.go` is 24/24,
`internal/monitor/parse_windows.go` is 9/9, `internal/monitor/powershell_windows.go`
is 2/2. All three read as 0% in Sonar today.

Each leg now uploads its own profile and the sonar job merges the three by
summing the per-block counters. Sonar can take several report paths, but its
merge semantics for a file present in more than one report are undocumented, so
the merge stays here rather than being guessed at.

`sonar-project.properties` is unchanged.
EOF
)"
```

- [ ] **Step 2: Wait for CI to go green**

```bash
gh pr checks --watch
```

Expected: `test` passes on all three legs, and `sonar` passes.

If the `sonar` job fails with "Artifact not found", then Step 3 and Step 4 of Task 2 disagree: the artifact name and the `pattern` do not match each other.

- [ ] **Step 3: See the combined total in the step summary**

The sonar job's summary should carry the line `### Coverage across all three platforms: NN.N%`, and it should be **higher** than the Linux leg's `### Coverage (linux leg only): ...` line.

- [ ] **Step 4: Check the five criteria in Sonar**

```bash
curl -s "https://sonarcloud.io/api/measures/component?component=atakankizilyuce_LeaveSafe&metricKeys=coverage,line_coverage,uncovered_lines"
```

```bash
curl -s "https://sonarcloud.io/api/measures/component_tree?component=atakankizilyuce_LeaveSafe&metricKeys=coverage&strategy=leaves&ps=500" \
  | python -c "
import sys, json
targets = ('parse_windows.go', 'parse_darwin.go', 'powershell_windows.go', 'service_darwin.go')
for c in json.load(sys.stdin)['components']:
    if c['path'].endswith(targets):
        m = {x['metric']: x.get('value') for x in c['measures']}
        print(m.get('coverage'), c['path'])
"
```

The criteria:

1. Overall `coverage` must have **risen** — 73.8% at the start, ~74.8-75.1% expected.
2. `internal/location/parse_windows.go` from 0% to ~100%.
3. `internal/monitor/parse_windows.go` from 0% to ~100%.
4. `internal/monitor/parse_darwin.go` must have come off 0%.
5. No portable file's coverage may have fallen.

- [ ] **Step 5: What to do if coverage falls**

If overall coverage fell, or a portable file went backwards, the merge is not producing a union. Do not merge; look at these in order:

1. In the sonar job log, is the `go tool cover -func` total higher than the Linux one? If it is not, the fault is in the awk rather than in Sonar — run Task 1 again against those three real profiles (download the artifacts with `gh run download`).
2. If the total is higher but Sonar reports lower, the problem is on the `sonar.exclusions` side; verify that `coverage.html` has not leaked into the sonar job's working directory (`ls coverage.html` should come back empty).

- [ ] **Step 6: Merge once the criteria are met**

When all five criteria hold, the PR can be merged. Write the resulting figure into the PR as a comment — that will be the starting point for phase 1.

---

## Out of scope

Phases 1-3 are separate PRs. The measured gap and the ceiling estimate are in the spec:
`docs/superpowers/specs/2026-08-07-sonar-coverage-merge-design.md`
