# Working on LeaveSafe

## Build from source

```bash
# Requires Go 1.25+. Node is NOT required — see below.
git clone https://github.com/atakankizilyuce/LeaveSafe.git
cd LeaveSafe

go build -o leavesafe ./cmd/leavesafe   # your platform
make all                                # all five targets
```

```bash
./leavesafe          # normal mode
./leavesafe -dev     # serves web assets from the filesystem
```

## The phone interface

It is a Vite + TypeScript + Preact app in `web/src`, built into `web/dist` and
embedded in the binary.

**`web/dist` is committed.** That is deliberate: `go build` and `go install`
have to work on a machine that has never installed Node, and a Go project that
silently produces a binary with no UI is a bad surprise. The cost of committing a
build artifact is that it can drift from its source, so CI rebuilds it and fails
if the result differs from what is checked in.

If you change anything under `web/src`, rebuild and commit the output — and
rebuild the binary too, since it embeds `web/dist` at compile time:

```bash
cd web
npm ci
npm run build      # writes web/dist — commit this
npm run typecheck
cd .. && go build -o leavesafe ./cmd/leavesafe
```

For live reload, run the binary and point Vite's dev server at it. The dev
server proxies `/ws` to port 9443:

```bash
go run ./cmd/leavesafe        # terminal one
cd web && npm run dev         # terminal two
```

`./leavesafe -dev` also exists and serves `web/dist` straight from disk, so a
rebuild shows up without restarting the binary.

## Checks

Every pull request has to pass the same gate, and all of it runs locally:

```bash
make fmt         # gofmt
make vet         # go vet
make lint        # golangci-lint (staticcheck, gosec, revive, errcheck, ...)
make web-lint    # biome plus tsc, for web/src
make web-verify  # rebuilds web/dist and fails if the committed output drifted
make vuln        # govulncheck
make test        # unit tests
make check       # all of the above
```

The CI workflow adds a few things a laptop cannot cover on its own:

| Job | What it does |
|-----|--------------|
| `format` | `gofmt` plus a check that `go.mod`/`go.sum` are tidy |
| `typos` | spell check across the repo ([typos](https://github.com/crate-ci/typos), configured in `_typos.toml`) |
| `lint` | `golangci-lint` once per target OS — half this codebase sits behind build tags, so a single platform never sees all of it |
| `test` | unit tests on Linux, Windows and macOS, with coverage reported in the run summary |
| `e2e` | starts the real binary on each OS and drives the whole user flow over a real WebSocket |
| `realtrigger` | fires the hardware changes each runner genuinely permits, and records every one it cannot |
| `sandbox-linux` | boots a real Linux VM under QEMU/KVM and creates real kernel-backed hardware |
| `frontend` | Biome, `tsc`, a production build, and a check that the committed `web/dist` still matches `web/src` |
| `build` | the full five-target release matrix |
| `vulncheck` | `govulncheck` against the Go toolchain and dependencies |

`ci-success` aggregates all of them, so branch protection only needs that one
required check. Dependency and action updates arrive weekly through Dependabot.

## How much this proves

Every run publishes a coverage matrix naming each sensor that was genuinely
triggered and each one that could not be, with the reason. No test fakes hardware
and reports success: where a real trigger is impossible, it is skipped and the
gap is stated.

In the Linux VM the charger is genuinely unplugged through the `test_power`
kernel module and the real binary reads the change from a real `/sys`. On Windows
real pointer activity is synthesised and the input sensor fires; real IP changes
are detected on all three. Everything else skips with a measured reason rather
than a fake pass — what no CI environment can reach is listed in
[manual-verification.md](manual-verification.md).

Run the layers locally with `make test-e2e`, `make test-realtrigger` and
`make test-sandbox`; plain `make test` stays fast and touches no hardware.

## Cutting a release

[`releasing.md`](releasing.md) is the order to do it in: what to rehearse before
tagging, what to watch during the run, and why merging a pull request in the tap
repository — rather than pushing the tag — is the moment a version reaches
Homebrew and Scoop. It also covers testing the update check against releases that
already exist, without publishing anything.
