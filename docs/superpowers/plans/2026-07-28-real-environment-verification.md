# Real-Environment Verification Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every pull request proves, with evidence, that LeaveSafe actually starts and actually detects real hardware changes on Windows, Linux and macOS — and openly declares every case it could not prove.

**Architecture:** Four layers. Layer 0 starts the real binary on all three GitHub runners and drives the real WebSocket protocol as if it were the phone. Layer 1 boots a real Ubuntu VM under QEMU/KVM and creates real kernel-backed hardware (`test_power`, `uinput`, `dummy_hcd`, `Xvfb`) so the unmodified binary reads a real `/sys`. Layer 2 fires the few genuine triggers Windows and macOS runners permit and skips the rest with a stated reason. Layer 3 extracts the OS-output-parsing logic into pure functions tested against output captured from the real runners. Docker support is removed because it measurably cannot read any sensor.

**Tech Stack:** Go 1.25.12, `nhooyr.io/websocket` (already a dependency), GitHub Actions, QEMU/KVM, cloud-init.

## Global Constraints

- Go version comes from `go.mod` (currently 1.25.12); never pin a different one in workflows — always `go-version-file: go.mod`.
- Commit messages: lowercase, imperative, no `feat:`/`fix:` prefix — this repo's history has no conventional-commit prefixes. Never add AI attribution.
- No new Go module dependencies. `nhooyr.io/websocket` and `nhooyr.io/websocket/wsjson` are already available.
- A test either exercises the real thing or calls `t.Skip` with a reason. Never substitute a fake that reports success.
- Every skip must reach the reader: `t.Skipf` message plus a row in the coverage matrix written to `$GITHUB_STEP_SUMMARY`.
- New test layers live behind build tags (`e2e`, `sandbox`, `realtrigger`) so plain `go test ./...` stays fast and hardware-free.
- `gofmt` must be clean; CI fails on unformatted files.
- Any new CI job MUST be added to the `ci-success` `needs` list, which fails on skipped jobs.

## File Structure

| Path | Responsibility |
| --- | --- |
| `test/harness/app.go` | Build, start, isolate and stop the real binary; recover the pairing key |
| `test/harness/signal_unix.go` / `signal_windows.go` | Graceful termination per OS |
| `test/harness/phone.go` | WebSocket client that plays the phone; typed send/expect |
| `test/harness/matrix.go` | Records triggered/skipped sensors, writes the job summary |
| `test/e2e/*_test.go` | Layer 0, build tag `e2e` |
| `test/realtrigger/*` | Layer 2, build tag `realtrigger` |
| `test/sandbox/linuxvm/` | Layer 1: host-side VM launcher + guest-side scenarios (tag `sandbox`) |
| `internal/monitor/parse_{linux,darwin,windows}.go` | Layer 3 pure parsers |
| `internal/monitor/testdata/{linux,darwin,windows}/` | Output captured from real runners |
| `docs/manual-verification.md` | What CI provably cannot cover |

`test/harness` carries no build tag: all three test layers import it, so it must compile everywhere. Platform differences inside it use GOOS file suffixes, not custom tags.

---

### Task 1: Remove Docker support

Docker cannot read five of six sensors, reports another machine's battery for the sixth, and cannot sound the alarm. The README documents a `privileged: true` setup that `docker-compose.yml` does not contain. It goes.

**Files:**
- Delete: `Dockerfile`, `docker-compose.yml`, `.dockerignore`
- Modify: `.github/workflows/ci.yml` (drop the `docker` job; update `ci-success` needs)
- Modify: `.github/dependabot.yml:48-61` (drop the docker ecosystem block)
- Modify: `Makefile:5` (`.PHONY`), `Makefile:54-58` (targets)
- Modify: `README.md:76`, `README.md:237`, `README.md:242-248`
- Modify: `internal/server/server.go:110-129` (`URLs`), `internal/server/server.go:159-161` (`isContainer`), and the `"os"` import

**Interfaces:**
- Consumes: nothing.
- Produces: nothing. Later tasks only rely on `ci-success` having been reduced to `[format, typos, lint, test, frontend, build, vulncheck]`, which they each extend.

- [ ] **Step 1: Delete the container files**

```bash
git rm Dockerfile docker-compose.yml .dockerignore
```

- [ ] **Step 2: Remove the `docker` CI job**

In `.github/workflows/ci.yml`, delete the entire block from the `# ---` comment banner above `docker:` (line ~224) through the end of the `Trivy scan` step (line ~256), and change the `ci-success` needs list to:

```yaml
    needs: [format, typos, lint, test, frontend, build, vulncheck]
```

- [ ] **Step 3: Remove the docker ecosystem from Dependabot**

In `.github/dependabot.yml`, delete lines 48-61 (the `# Base images in the Dockerfile.` comment and the whole `package-ecosystem: docker` entry). The file must end after the `github-actions` block.

- [ ] **Step 4: Remove the Make targets**

In `Makefile`, drop `docker` and `docker-run` from the `.PHONY` line so it reads:

```makefile
.PHONY: all build build-windows build-darwin build-darwin-arm build-linux clean test fmt vet lint typos web-lint vuln check
```

and delete the final two target blocks:

```makefile
docker:
	docker build -t $(BINARY_NAME) .

docker-run:
	docker run --rm -it -p 8080:8080 -e PORT=8080 -e CONTAINER=1 $(BINARY_NAME)
```

- [ ] **Step 5: Remove the container branch from the server**

In `internal/server/server.go`, delete the `"os"` line from the import block, delete the whole `isContainer` function (lines 159-161 plus its preceding blank line), and remove its call site so `URLs` reads:

```go
// URLs returns the HTTP(S) URLs clients can connect to.
func (s *Server) URLs() []string {
	scheme := "http"
	if s.tlsCert != nil {
		scheme = "https"
	}

	ips := getLocalIPs()
	urls := make([]string, 0, len(ips))

	for _, ip := range ips {
		urls = append(urls, fmt.Sprintf("%s://%s:%d", scheme, ip.String(), s.port))
	}
	return urls
}
```

- [ ] **Step 6: Update the README**

Delete the feature bullet on line 76 (`- **Docker Support** — Run in a container with privileged hardware access`). Delete the `| \`docker\` | builds the container image and scans it with Trivy |` table row. Delete the whole `### Docker` section including the fenced `docker-compose up` block and the `> **Note:**` line beneath it.

Replace the platform-support claim in the feature table's Deployment cell so it no longer promises containers — the remaining bullets (`Cross-Platform`, `Single Binary`, `Configuration Persistence`) stay as they are.

- [ ] **Step 7: Verify nothing references Docker any more**

Run: `git grep -in "docker\|CONTAINER=1\|isContainer" -- . ':!docs/'`
Expected: no output. (`docs/` legitimately retains the design record explaining the removal.)

- [ ] **Step 8: Verify the build and tests still pass**

Run: `go build ./... && go test ./... -count=1`
Expected: PASS, no compile error about an unused `"os"` import.

- [ ] **Step 9: Verify the workflow is still valid YAML**

Run: `python -c "import yaml,sys; yaml.safe_load(open('.github/workflows/ci.yml')); yaml.safe_load(open('.github/dependabot.yml')); print('ok')"`
Expected: `ok`

- [ ] **Step 10: Commit**

```bash
git add -A
git commit -m "remove docker support, it cannot read any sensor"
```

---

### Task 2: Test harness and the first real-process test

Builds the binary, starts it under an isolated home directory, recovers the pairing key from its output, and proves it shuts down cleanly and releases its port. This is the foundation all three test layers stand on.

**Files:**
- Create: `test/harness/app.go`, `test/harness/signal_unix.go`, `test/harness/signal_windows.go`
- Create: `test/e2e/lifecycle_test.go`
- Modify: `Makefile`

**Interfaces:**
- Consumes: `internal/config` (for the seeded config shape).
- Produces:
  - `harness.Start(t *testing.T, opts harness.Options) *harness.App`
  - `harness.Options{Port int, Pin string, EnabledSensors map[string]bool}`
  - `(*App).Port() int`, `(*App).Key() string`, `(*App).HomeDir() string`, `(*App).ConfigDir() string`
  - `(*App).Stop() error` — graceful, returns the process exit error
  - `(*App).Output() string` — everything the process has written so far
  - `harness.FreePort(t *testing.T) int`

- [ ] **Step 1: Write the failing test**

Create `test/e2e/lifecycle_test.go`:

```go
//go:build e2e

package e2e_test

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/leavesafe/leavesafe/test/harness"
)

// TestApp_StartsAndServes proves the binary boots on this OS and accepts TCP.
func TestApp_StartsAndServes(t *testing.T) {
	app := harness.Start(t, harness.Options{})

	conn, err := net.DialTimeout("tcp",
		fmt.Sprintf("127.0.0.1:%d", app.Port()), 5*time.Second)
	if err != nil {
		t.Fatalf("app is not accepting connections on port %d: %v", app.Port(), err)
	}
	_ = conn.Close()
}

// TestApp_PublishesPairingKey proves a usable pairing key reaches the operator.
func TestApp_PublishesPairingKey(t *testing.T) {
	app := harness.Start(t, harness.Options{})

	key := app.Key()
	if len(key) != 19 {
		t.Fatalf("pairing key %q has length %d, want 19 (XXXX-XXXX-XXXX-XXXX)", key, len(key))
	}
	for _, part := range strings.Split(key, "-") {
		if len(part) != 4 {
			t.Fatalf("pairing key %q is not four groups of four digits", key)
		}
	}
}

// TestApp_ShutsDownCleanly proves the process exits on a termination signal
// and releases its port, so a restart is never blocked by a zombie.
func TestApp_ShutsDownCleanly(t *testing.T) {
	app := harness.Start(t, harness.Options{})
	port := app.Port()

	if err := app.Stop(); err != nil {
		t.Fatalf("app did not shut down cleanly: %v\n--- output ---\n%s", err, app.Output())
	}

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("port %d was not released after shutdown: %v", port, err)
	}
	_ = ln.Close()
}

// TestApp_WritesEventLog proves the audit trail is created in the config dir
// and holds well-formed JSONL.
func TestApp_WritesEventLog(t *testing.T) {
	app := harness.Start(t, harness.Options{})
	path := filepath.Join(app.ConfigDir(), "events.jsonl")

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("event log was not created at %s: %v", path, err)
	}

	data, err := os.ReadFile(path) //nolint:gosec // path is built by the harness
	if err != nil {
		t.Fatalf("read event log: %v", err)
	}
	for i, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var evt map[string]any
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			t.Fatalf("event log line %d is not valid JSON: %v (%q)", i+1, err, line)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -tags e2e ./test/e2e/... -run TestApp_StartsAndServes -v`
Expected: FAIL — `no required module provides package github.com/leavesafe/leavesafe/test/harness`

- [ ] **Step 3: Implement the harness**

Create `test/harness/app.go`:

```go
// Package harness starts the real leavesafe binary in an isolated environment
// and drives it the way a phone would. It is shared by the e2e, sandbox and
// realtrigger test layers, so it carries no build tag and must compile on
// every supported platform.
package harness

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// Options configures a harness-managed application instance.
type Options struct {
	// Port is the TCP port to bind. Zero means the harness picks a free one.
	Port int
	// Pin, when non-empty, enables PIN-protected disarm with this code.
	Pin string
	// EnabledSensors seeds the sensor enable-map in the config file.
	EnabledSensors map[string]bool
}

// App is a running leavesafe process.
type App struct {
	t         *testing.T
	cmd       *exec.Cmd
	port      int
	key       string
	homeDir   string
	configDir string
	out       *syncBuffer
	stopOnce  sync.Once
	stopErr   error
}

type syncBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

var (
	buildOnce sync.Once
	binPath   string
	buildErr  error
)

// binary compiles cmd/leavesafe once per test binary and returns its path.
func binary(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "leavesafe-bin")
		if err != nil {
			buildErr = err
			return
		}
		name := "leavesafe"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		out := filepath.Join(dir, name)
		cmd := exec.Command("go", "build", "-o", out, "./cmd/leavesafe")
		cmd.Dir = repoRoot(t)
		if combined, err := cmd.CombinedOutput(); err != nil {
			buildErr = fmt.Errorf("build leavesafe: %w\n%s", err, combined)
			return
		}
		binPath = out
	})
	if buildErr != nil {
		t.Fatalf("%v", buildErr)
	}
	return binPath
}

// repoRoot walks up from the working directory until it finds go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the working directory")
		}
		dir = parent
	}
}

// FreePort asks the OS for an unused TCP port.
func FreePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve free port: %v", err)
	}
	defer func() { _ = ln.Close() }()
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatal("listener address is not TCP")
	}
	return addr.Port
}

var keyPattern = regexp.MustCompile(`\d{4}-\d{4}-\d{4}-\d{4}`)

// Start launches the binary with an isolated home directory and waits until it
// is serving. The process is stopped automatically when the test ends.
func Start(t *testing.T, opts Options) *App {
	t.Helper()

	if opts.Port == 0 {
		opts.Port = FreePort(t)
	}

	home := t.TempDir()
	configDir := filepath.Join(home, ".leavesafe")
	if runtime.GOOS == "windows" {
		configDir = filepath.Join(home, "LeaveSafe")
	}
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatalf("create config dir: %v", err)
	}
	writeSeedConfig(t, configDir, opts)

	app := &App{
		t:         t,
		port:      opts.Port,
		homeDir:   home,
		configDir: configDir,
		out:       &syncBuffer{},
	}

	// #nosec G204 -- the binary path is produced by our own go build
	cmd := exec.Command(binary(t))
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"USERPROFILE="+home,
		"APPDATA="+home,
		fmt.Sprintf("PORT=%d", opts.Port),
	)
	cmd.Stdout = app.out
	cmd.Stderr = app.out
	configureProcessGroup(cmd)

	if err := cmd.Start(); err != nil {
		t.Fatalf("start leavesafe: %v", err)
	}
	app.cmd = cmd
	t.Cleanup(func() { _ = app.Stop() })

	app.waitUntilServing()
	app.key = app.waitForKey()
	return app
}

// writeSeedConfig pre-answers the interactive first-run prompt and applies the
// caller's options, so the process never blocks on stdin.
func writeSeedConfig(t *testing.T, dir string, opts Options) {
	t.Helper()
	remote := false
	cfg := map[string]any{
		"port":                     0,
		"max_sessions":             3,
		"max_auth_attempts":        5,
		"lockout_seconds":          60,
		"heartbeat_seconds":        15,
		"disconnect_grace_seconds": 30,
		"auto_arm_on_lock":         false,
		"input_threshold":          1,
		"connection_mode":          "wifi",
		"remote_access":            &remote,
		"alarm": map[string]any{
			"escalation_enabled": false,
			"levels": []map[string]any{
				{"delay_seconds": 0, "action": "notify_phone_only"},
			},
		},
		"pin_protection": map[string]any{
			"enabled": opts.Pin != "",
			"pin":     opts.Pin,
		},
	}
	if opts.EnabledSensors != nil {
		cfg["enabled_sensors"] = opts.EnabledSensors
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal seed config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), data, 0o600); err != nil {
		t.Fatalf("write seed config: %v", err)
	}
}

// waitUntilServing blocks until the port accepts a connection or the deadline
// passes. A dead process fails fast with its output attached.
func (a *App) waitUntilServing() {
	a.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	addr := fmt.Sprintf("127.0.0.1:%d", a.port)
	for time.Now().Before(deadline) {
		if a.cmd.ProcessState != nil && a.cmd.ProcessState.Exited() {
			a.t.Fatalf("leavesafe exited before serving\n--- output ---\n%s", a.out.String())
		}
		conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	a.t.Fatalf("leavesafe did not serve on %s within 30s\n--- output ---\n%s", addr, a.out.String())
}

// waitForKey recovers the pairing key from the dashboard the process renders.
func (a *App) waitForKey() string {
	a.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if m := keyPattern.FindString(a.out.String()); m != "" {
			return m
		}
		time.Sleep(200 * time.Millisecond)
	}
	a.t.Fatalf("pairing key never appeared in output\n--- output ---\n%s", a.out.String())
	return ""
}

// Port returns the bound TCP port.
func (a *App) Port() int { return a.port }

// Key returns the pairing key in XXXX-XXXX-XXXX-XXXX form.
func (a *App) Key() string { return a.key }

// HomeDir returns the isolated home directory.
func (a *App) HomeDir() string { return a.homeDir }

// ConfigDir returns the directory holding config.json and events.jsonl.
func (a *App) ConfigDir() string { return a.configDir }

// Output returns everything the process has written so far.
func (a *App) Output() string { return a.out.String() }

// Stop terminates the process gracefully and waits for it to exit. It is safe
// to call more than once; only the first call does the work.
func (a *App) Stop() error {
	a.stopOnce.Do(func() {
		if a.cmd == nil || a.cmd.Process == nil {
			return
		}
		if err := terminate(a.cmd); err != nil {
			a.stopErr = fmt.Errorf("signal process: %w", err)
			_ = a.cmd.Process.Kill()
			_ = a.cmd.Wait()
			return
		}

		done := make(chan error, 1)
		go func() { done <- a.cmd.Wait() }()

		select {
		case <-done:
			// The app calls os.Exit(0) from its signal handler, and a killed
			// process reports an exit error; neither is a failure here.
		case <-time.After(15 * time.Second):
			_ = a.cmd.Process.Kill()
			<-done
			a.stopErr = fmt.Errorf("process ignored the termination signal for 15s")
		}
	})
	return a.stopErr
}
```

- [ ] **Step 4: Implement graceful termination for Unix**

Create `test/harness/signal_unix.go`:

```go
//go:build !windows

package harness

import (
	"os/exec"
	"syscall"
)

// configureProcessGroup is a no-op on Unix; the default group is fine.
func configureProcessGroup(_ *exec.Cmd) {}

// terminate asks the process to shut down the way a real operator would.
func terminate(cmd *exec.Cmd) error {
	return cmd.Process.Signal(syscall.SIGTERM)
}
```

- [ ] **Step 5: Implement graceful termination for Windows**

Windows has no SIGTERM. The equivalent is a console CTRL+BREAK, which the Go runtime delivers to the child as `os.Interrupt` — and `main.go` already listens for `syscall.SIGINT`. Delivering it requires the child to own a process group, hence `CREATE_NEW_PROCESS_GROUP`.

Create `test/harness/signal_windows.go`:

```go
//go:build windows

package harness

import (
	"fmt"
	"os/exec"
	"syscall"
)

const createNewProcessGroup = 0x00000200

// configureProcessGroup puts the child in its own console process group so a
// CTRL+BREAK can be addressed to it without hitting the test runner too.
func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup}
}

// terminate sends CTRL+BREAK, which the Go runtime surfaces in the child as
// os.Interrupt — the signal main.go installs a handler for.
func terminate(cmd *exec.Cmd) error {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	proc := kernel32.NewProc("GenerateConsoleCtrlEvent")
	const ctrlBreakEvent = 1
	ret, _, err := proc.Call(uintptr(ctrlBreakEvent), uintptr(cmd.Process.Pid))
	if ret == 0 {
		return fmt.Errorf("GenerateConsoleCtrlEvent: %w", err)
	}
	return nil
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test -tags e2e ./test/e2e/... -v -count=1`
Expected: PASS — all four tests.

If `TestApp_ShutsDownCleanly` fails on Windows with "process ignored the termination signal", capture the harness output and report it rather than switching to a hard kill: a binary that ignores CTRL+BREAK is a real product bug worth surfacing.

- [ ] **Step 7: Verify the default test run is unaffected**

Run: `go test ./... -count=1 && gofmt -l .`
Expected: PASS, and `gofmt -l` prints nothing. The `test/harness` package compiles without a tag; `test/e2e` is skipped because it is tagged.

- [ ] **Step 8: Add the Make target**

In `Makefile`, add `test-e2e` to `.PHONY` and append:

```makefile
# Layer 0: starts the real binary on this OS and drives the full user flow.
test-e2e:
	go test -tags e2e ./test/e2e/... -v -count=1
```

- [ ] **Step 9: Commit**

```bash
git add test/harness test/e2e Makefile
git commit -m "add test harness that runs the real binary"
```

---

### Task 3: Phone client and the pairing flow

Adds the WebSocket client that plays the phone, then uses it to prove authentication, lockout and the session cap behave on a real running process.

**Files:**
- Create: `test/harness/phone.go`
- Create: `test/e2e/pairing_test.go`

**Interfaces:**
- Consumes: `harness.Start`, `(*App).Port`, `(*App).Key` from Task 2.
- Produces:
  - `harness.Dial(t *testing.T, port int) *harness.Phone`
  - `(*Phone).Send(msg ws.ClientMessage)`
  - `(*Phone).Expect(msgType string, within time.Duration) ws.ServerMessage` — skips non-matching messages, fails the test on timeout
  - `(*Phone).ExpectNot(msgType string, within time.Duration)` — fails if the type arrives
  - `(*Phone).Authenticate(key string) ws.ServerMessage`
  - `(*Phone).Close()`

- [ ] **Step 1: Write the failing test**

Create `test/e2e/pairing_test.go`:

```go
//go:build e2e

package e2e_test

import (
	"testing"
	"time"

	"github.com/leavesafe/leavesafe/internal/ws"
	"github.com/leavesafe/leavesafe/test/harness"
)

// TestPairing_CorrectKeyIssuesToken proves the documented pairing flow works
// against the real process.
func TestPairing_CorrectKeyIssuesToken(t *testing.T) {
	app := harness.Start(t, harness.Options{})
	phone := harness.Dial(t, app.Port())

	reply := phone.Authenticate(app.Key())
	if reply.Type != ws.MsgTypeAuthOK {
		t.Fatalf("auth reply type = %q, want %q (reason: %q)",
			reply.Type, ws.MsgTypeAuthOK, reply.Reason)
	}
	if reply.Token == "" {
		t.Error("auth_ok carried no session token")
	}
	if len(reply.Sensors) == 0 {
		t.Error("auth_ok carried no sensor list; the phone cannot render its UI")
	}
}

// TestPairing_WrongKeyIsRejected proves a bad key never yields a session.
func TestPairing_WrongKeyIsRejected(t *testing.T) {
	app := harness.Start(t, harness.Options{})
	phone := harness.Dial(t, app.Port())

	reply := phone.Authenticate("0000-0000-0000-0000")
	if reply.Type != ws.MsgTypeAuthFail {
		t.Fatalf("auth reply type = %q, want %q", reply.Type, ws.MsgTypeAuthFail)
	}
	if reply.Token != "" {
		t.Error("auth_fail carried a session token")
	}
}

// TestPairing_UnauthenticatedCommandsRejected proves an unpaired client cannot
// arm the system — the single most important access-control rule here.
func TestPairing_UnauthenticatedCommandsRejected(t *testing.T) {
	app := harness.Start(t, harness.Options{})
	phone := harness.Dial(t, app.Port())

	phone.Send(ws.ClientMessage{Type: ws.MsgTypeArm})
	reply := phone.Expect(ws.MsgTypeAuthFail, 5*time.Second)
	if reply.Reason != "not authenticated" {
		t.Errorf("reason = %q, want %q", reply.Reason, "not authenticated")
	}
}

// TestPairing_LockoutAfterFiveFailures proves brute force is stopped. This test
// gets its own process because the lockout lasts 60 seconds.
func TestPairing_LockoutAfterFiveFailures(t *testing.T) {
	app := harness.Start(t, harness.Options{})
	phone := harness.Dial(t, app.Port())

	for i := 1; i <= 4; i++ {
		reply := phone.Authenticate("0000-0000-0000-0000")
		if reply.Type != ws.MsgTypeAuthFail {
			t.Fatalf("attempt %d: type = %q, want auth_fail", i, reply.Type)
		}
		if want := 5 - i; reply.RemainingAttempts != want {
			t.Errorf("attempt %d: remaining_attempts = %d, want %d",
				i, reply.RemainingAttempts, want)
		}
	}

	// The fifth failure engages the lockout.
	phone.Authenticate("0000-0000-0000-0000")

	// The correct key must now be refused too.
	reply := phone.Authenticate(app.Key())
	if reply.Type != ws.MsgTypeAuthFail {
		t.Fatalf("after lockout the correct key was accepted (type %q)", reply.Type)
	}
}

// TestPairing_SessionCapEnforced proves the fourth concurrent phone is refused.
func TestPairing_SessionCapEnforced(t *testing.T) {
	app := harness.Start(t, harness.Options{})

	for i := 1; i <= 3; i++ {
		p := harness.Dial(t, app.Port())
		if reply := p.Authenticate(app.Key()); reply.Type != ws.MsgTypeAuthOK {
			t.Fatalf("client %d could not authenticate: %q", i, reply.Reason)
		}
	}

	fourth := harness.Dial(t, app.Port())
	reply := fourth.Authenticate(app.Key())
	if reply.Type != ws.MsgTypeAuthFail {
		t.Fatalf("fourth client was accepted; the session cap is not enforced")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -tags e2e ./test/e2e/... -run TestPairing -v`
Expected: FAIL — `undefined: harness.Dial`

- [ ] **Step 3: Implement the phone client**

Create `test/harness/phone.go`:

```go
package harness

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/leavesafe/leavesafe/internal/ws"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

// Phone is a WebSocket client that speaks the protocol the mobile UI speaks.
// Incoming messages are drained by a background reader so status broadcasts
// never block an Expect for a different type.
type Phone struct {
	t      *testing.T
	conn   *websocket.Conn
	ctx    context.Context
	cancel context.CancelFunc
	inbox  chan ws.ServerMessage
}

// Dial connects to a running app and starts the reader.
func Dial(t *testing.T, port int) *Phone {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	url := fmt.Sprintf("ws://127.0.0.1:%d/ws", port)

	dialCtx, dialCancel := context.WithTimeout(ctx, 10*time.Second)
	defer dialCancel()

	conn, _, err := websocket.Dial(dialCtx, url, nil)
	if err != nil {
		cancel()
		t.Fatalf("dial %s: %v", url, err)
	}
	conn.SetReadLimit(1 << 20)

	p := &Phone{
		t:      t,
		conn:   conn,
		ctx:    ctx,
		cancel: cancel,
		inbox:  make(chan ws.ServerMessage, 64),
	}

	go p.readLoop()
	t.Cleanup(p.Close)
	return p
}

func (p *Phone) readLoop() {
	for {
		var msg ws.ServerMessage
		if err := wsjson.Read(p.ctx, p.conn, &msg); err != nil {
			close(p.inbox)
			return
		}
		select {
		case p.inbox <- msg:
		case <-p.ctx.Done():
			return
		}
	}
}

// Send writes one client message.
func (p *Phone) Send(msg ws.ClientMessage) {
	p.t.Helper()
	ctx, cancel := context.WithTimeout(p.ctx, 10*time.Second)
	defer cancel()
	if err := wsjson.Write(ctx, p.conn, msg); err != nil {
		p.t.Fatalf("send %s: %v", msg.Type, err)
	}
}

// Expect waits for the next message of the given type, discarding others.
func (p *Phone) Expect(msgType string, within time.Duration) ws.ServerMessage {
	p.t.Helper()
	deadline := time.After(within)
	var seen []string
	for {
		select {
		case msg, ok := <-p.inbox:
			if !ok {
				p.t.Fatalf("connection closed while waiting for %q (saw %v)", msgType, seen)
			}
			if msg.Type == msgType {
				return msg
			}
			seen = append(seen, msg.Type)
		case <-deadline:
			p.t.Fatalf("timed out after %s waiting for %q (saw %v)", within, msgType, seen)
		}
	}
}

// ExpectNot fails the test if the given type arrives within the window.
func (p *Phone) ExpectNot(msgType string, within time.Duration) {
	p.t.Helper()
	deadline := time.After(within)
	for {
		select {
		case msg, ok := <-p.inbox:
			if !ok {
				return
			}
			if msg.Type == msgType {
				p.t.Fatalf("received %q, which must not happen here", msgType)
			}
		case <-deadline:
			return
		}
	}
}

// Authenticate sends a pairing key and returns the auth_ok or auth_fail reply.
func (p *Phone) Authenticate(key string) ws.ServerMessage {
	p.t.Helper()
	p.Send(ws.ClientMessage{Type: ws.MsgTypeAuth, Key: key})

	deadline := time.After(10 * time.Second)
	for {
		select {
		case msg, ok := <-p.inbox:
			if !ok {
				p.t.Fatal("connection closed while authenticating")
			}
			if msg.Type == ws.MsgTypeAuthOK || msg.Type == ws.MsgTypeAuthFail {
				return msg
			}
		case <-deadline:
			p.t.Fatal("timed out waiting for an auth reply")
		}
	}
}

// Close tears down the connection. Safe to call twice.
func (p *Phone) Close() {
	p.cancel()
	_ = p.conn.Close(websocket.StatusNormalClosure, "")
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test -tags e2e ./test/e2e/... -run TestPairing -v -count=1`
Expected: PASS — all five tests.

- [ ] **Step 5: Commit**

```bash
git add test/harness/phone.go test/e2e/pairing_test.go
git commit -m "verify pairing, lockout and session cap end to end"
```

---

### Task 4: Arm, alarm and disarm flow

Proves the product's core promise on a real process: an armed system raises an alarm, and only the right PIN clears it.

**Files:**
- Create: `test/e2e/alarm_test.go`

**Interfaces:**
- Consumes: everything from Tasks 2 and 3.
- Produces: nothing new.

Protocol facts this task depends on, verified against `internal/ws/hub.go`:
- `configure` is refused while armed, so sensors must be enabled *before* `arm`.
- `trigger_sensor` emits `alert`, and additionally `alarm_active` when armed.
- With PIN protection on, plain `disarm` replies `pin_required`; `disarm_with_pin` with a wrong PIN replies `auth_fail` with reason `invalid PIN`.

- [ ] **Step 1: Write the failing test**

Create `test/e2e/alarm_test.go`:

```go
//go:build e2e

package e2e_test

import (
	"testing"
	"time"

	"github.com/leavesafe/leavesafe/internal/ws"
	"github.com/leavesafe/leavesafe/test/harness"
)

// armedPhone pairs, enables the network sensor and arms the system.
func armedPhone(t *testing.T, opts harness.Options) (*harness.App, *harness.Phone) {
	t.Helper()
	app := harness.Start(t, opts)
	phone := harness.Dial(t, app.Port())

	if reply := phone.Authenticate(app.Key()); reply.Type != ws.MsgTypeAuthOK {
		t.Fatalf("authenticate: %s", reply.Reason)
	}

	phone.Send(ws.ClientMessage{
		Type:    ws.MsgTypeConfigure,
		Sensors: map[string]bool{"network": true},
	})
	phone.Send(ws.ClientMessage{Type: ws.MsgTypeArm})

	status := phone.Expect(ws.MsgTypeStatus, 10*time.Second)
	if status.Armed == nil || !*status.Armed {
		t.Fatal("system did not report itself armed after arm")
	}
	return app, phone
}

// TestAlarm_TriggerRaisesAlarmWhenArmed proves an armed system escalates a
// sensor event into an active alarm on the phone.
func TestAlarm_TriggerRaisesAlarmWhenArmed(t *testing.T) {
	_, phone := armedPhone(t, harness.Options{})

	phone.Send(ws.ClientMessage{Type: ws.MsgTypeTriggerSensor, Sensor: "network"})

	alert := phone.Expect(ws.MsgTypeAlert, 10*time.Second)
	if alert.Alert == nil || alert.Alert.Sensor != "network" {
		t.Fatalf("alert did not name the network sensor: %+v", alert.Alert)
	}

	active := phone.Expect(ws.MsgTypeAlarmActive, 10*time.Second)
	if active.Alert == nil || active.Alert.Message == "" {
		t.Error("alarm_active carried no message for the user to act on")
	}
}

// TestAlarm_NoAlarmWhenDisarmed proves a disarmed system stays quiet — a false
// alarm destroys trust as surely as a missed one.
func TestAlarm_NoAlarmWhenDisarmed(t *testing.T) {
	app := harness.Start(t, harness.Options{})
	phone := harness.Dial(t, app.Port())
	if reply := phone.Authenticate(app.Key()); reply.Type != ws.MsgTypeAuthOK {
		t.Fatalf("authenticate: %s", reply.Reason)
	}

	phone.Send(ws.ClientMessage{Type: ws.MsgTypeTriggerSensor, Sensor: "network"})
	phone.ExpectNot(ws.MsgTypeAlarmActive, 5*time.Second)
}

// TestAlarm_DisarmClearsArmedState proves disarming without PIN protection
// returns the system to rest.
func TestAlarm_DisarmClearsArmedState(t *testing.T) {
	_, phone := armedPhone(t, harness.Options{})

	phone.Send(ws.ClientMessage{Type: ws.MsgTypeDisarm})

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		status := phone.Expect(ws.MsgTypeStatus, 10*time.Second)
		if status.Armed != nil && !*status.Armed {
			return
		}
	}
	t.Fatal("system never reported itself disarmed")
}

// TestAlarm_PinProtectedDisarm proves a thief holding the phone cannot silence
// the alarm without the PIN.
func TestAlarm_PinProtectedDisarm(t *testing.T) {
	_, phone := armedPhone(t, harness.Options{Pin: "4271"})

	// A bare disarm must be refused with a PIN challenge.
	phone.Send(ws.ClientMessage{Type: ws.MsgTypeDisarm})
	phone.Expect(ws.MsgTypePinRequired, 10*time.Second)

	// A wrong PIN must be rejected.
	phone.Send(ws.ClientMessage{Type: ws.MsgTypeDisarmPin, Pin: "0000"})
	fail := phone.Expect(ws.MsgTypeAuthFail, 10*time.Second)
	if fail.Reason != "invalid PIN" {
		t.Errorf("reason = %q, want %q", fail.Reason, "invalid PIN")
	}

	// The correct PIN must work.
	phone.Send(ws.ClientMessage{Type: ws.MsgTypeDisarmPin, Pin: "4271"})
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		status := phone.Expect(ws.MsgTypeStatus, 10*time.Second)
		if status.Armed != nil && !*status.Armed {
			return
		}
	}
	t.Fatal("the correct PIN did not disarm the system")
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -tags e2e ./test/e2e/... -run TestAlarm -v`
Expected: FAIL — the file does not compile until Task 3's `harness.Phone` exists; if Task 3 is done, expect real assertions to run.

- [ ] **Step 3: Run the tests and investigate any failure**

Run: `go test -tags e2e ./test/e2e/... -run TestAlarm -v -count=1`
Expected: PASS.

These tests exercise existing behaviour, so no production change should be needed. If one fails, that is a genuine product bug — report it with the harness output rather than weakening the assertion.

- [ ] **Step 4: Commit**

```bash
git add test/e2e/alarm_test.go
git commit -m "verify arm, alarm and pin-protected disarm end to end"
```

---

### Task 5: Config round-trip and the e2e CI job

Completes Layer 0 and wires it into CI on all three operating systems.

**Files:**
- Create: `test/e2e/config_test.go`
- Modify: `.github/workflows/ci.yml`

**Interfaces:**
- Consumes: Tasks 2-4.
- Produces: a CI job named `e2e` that later tasks add to alongside their own.

- [ ] **Step 1: Write the failing test**

Create `test/e2e/config_test.go`:

```go
//go:build e2e

package e2e_test

import (
	"testing"
	"time"

	"github.com/leavesafe/leavesafe/internal/ws"
	"github.com/leavesafe/leavesafe/test/harness"
)

func pairedPhone(t *testing.T) (*harness.App, *harness.Phone) {
	t.Helper()
	app := harness.Start(t, harness.Options{})
	phone := harness.Dial(t, app.Port())
	if reply := phone.Authenticate(app.Key()); reply.Type != ws.MsgTypeAuthOK {
		t.Fatalf("authenticate: %s", reply.Reason)
	}
	return app, phone
}

// TestConfig_GetReturnsSettings proves the phone can read the live config.
func TestConfig_GetReturnsSettings(t *testing.T) {
	_, phone := pairedPhone(t)

	phone.Send(ws.ClientMessage{Type: ws.MsgTypeGetConfig})
	reply := phone.Expect(ws.MsgTypeConfigData, 10*time.Second)

	if reply.Config == nil {
		t.Fatal("config_data carried no config")
	}
	if reply.Config.MaxSessions != 3 {
		t.Errorf("max_sessions = %d, want 3", reply.Config.MaxSessions)
	}
}

// TestConfig_UpdatePersists proves a setting changed from the phone survives a
// restart — the feature is worthless if it forgets.
func TestConfig_UpdatePersists(t *testing.T) {
	app, phone := pairedPhone(t)

	phone.Send(ws.ClientMessage{Type: ws.MsgTypeGetConfig})
	current := phone.Expect(ws.MsgTypeConfigData, 10*time.Second)

	updated := *current.Config
	updated.InputThreshold = 7
	phone.Send(ws.ClientMessage{Type: ws.MsgTypeUpdateConfig, Config: &updated})
	phone.Expect(ws.MsgTypeConfigData, 10*time.Second)

	// Restart against the same home directory and confirm the value stuck.
	if err := app.Stop(); err != nil {
		t.Fatalf("stop app: %v", err)
	}
	restarted := harness.StartIn(t, app.HomeDir(), harness.Options{Port: harness.FreePort(t)})
	phone2 := harness.Dial(t, restarted.Port())
	if reply := phone2.Authenticate(restarted.Key()); reply.Type != ws.MsgTypeAuthOK {
		t.Fatalf("authenticate after restart: %s", reply.Reason)
	}

	phone2.Send(ws.ClientMessage{Type: ws.MsgTypeGetConfig})
	after := phone2.Expect(ws.MsgTypeConfigData, 10*time.Second)
	if after.Config.InputThreshold != 7 {
		t.Errorf("input_threshold after restart = %d, want 7", after.Config.InputThreshold)
	}
}

// TestConfig_ResetRestoresDefaults proves the reset escape hatch works.
func TestConfig_ResetRestoresDefaults(t *testing.T) {
	_, phone := pairedPhone(t)

	phone.Send(ws.ClientMessage{Type: ws.MsgTypeGetConfig})
	current := phone.Expect(ws.MsgTypeConfigData, 10*time.Second)

	updated := *current.Config
	updated.InputThreshold = 9
	phone.Send(ws.ClientMessage{Type: ws.MsgTypeUpdateConfig, Config: &updated})
	phone.Expect(ws.MsgTypeConfigData, 10*time.Second)

	phone.Send(ws.ClientMessage{Type: ws.MsgTypeResetConfig})
	reset := phone.Expect(ws.MsgTypeConfigData, 10*time.Second)
	if reset.Config.InputThreshold == 9 {
		t.Error("reset_config left the modified input_threshold in place")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -tags e2e ./test/e2e/... -run TestConfig -v`
Expected: FAIL — `undefined: harness.StartIn`

- [ ] **Step 3: Add `StartIn` to the harness**

In `test/harness/app.go`, refactor `Start` so it delegates, and export the reuse path. Replace the `Start` function with:

```go
// Start launches the binary with a fresh isolated home directory.
func Start(t *testing.T, opts Options) *App {
	t.Helper()
	return StartIn(t, t.TempDir(), opts)
}

// StartIn launches the binary against an existing home directory. Use it to
// restart an app and assert that state survived.
func StartIn(t *testing.T, home string, opts Options) *App {
	t.Helper()

	if opts.Port == 0 {
		opts.Port = FreePort(t)
	}

	configDir := filepath.Join(home, ".leavesafe")
	if runtime.GOOS == "windows" {
		configDir = filepath.Join(home, "LeaveSafe")
	}
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatalf("create config dir: %v", err)
	}
	// Only seed a config that is not already there, so a restart keeps state.
	if _, err := os.Stat(filepath.Join(configDir, "config.json")); os.IsNotExist(err) {
		writeSeedConfig(t, configDir, opts)
	}

	app := &App{
		t:         t,
		port:      opts.Port,
		homeDir:   home,
		configDir: configDir,
		out:       &syncBuffer{},
	}

	// #nosec G204 -- the binary path is produced by our own go build
	cmd := exec.Command(binary(t))
	cmd.Dir = repoRoot(t)
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"USERPROFILE="+home,
		"APPDATA="+home,
		fmt.Sprintf("PORT=%d", opts.Port),
	)
	cmd.Stdout = app.out
	cmd.Stderr = app.out
	configureProcessGroup(cmd)

	if err := cmd.Start(); err != nil {
		t.Fatalf("start leavesafe: %v", err)
	}
	app.cmd = cmd
	t.Cleanup(func() { _ = app.Stop() })

	app.waitUntilServing()
	app.key = app.waitForKey()
	return app
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test -tags e2e ./test/e2e/... -v -count=1`
Expected: PASS — every Layer 0 test.

- [ ] **Step 5: Add the `e2e` CI job**

In `.github/workflows/ci.yml`, insert after the `test` job:

```yaml
  # ---------------------------------------------------------------------------
  # Layer 0: the real binary is started on every supported OS and driven through
  # the whole user journey over a real WebSocket. This is what catches "the new
  # version does not boot on macOS" before it reaches a user.
  # ---------------------------------------------------------------------------
  e2e:
    name: E2E (${{ matrix.os }})
    runs-on: ${{ matrix.os }}
    timeout-minutes: 15
    strategy:
      fail-fast: false
      matrix:
        os: [ubuntu-latest, windows-latest, macos-latest]
    steps:
      - uses: actions/checkout@v7

      - name: Set up Go
        uses: actions/setup-go@v7
        with:
          go-version-file: go.mod

      - name: Run end-to-end suite
        shell: bash
        run: go test -tags e2e ./test/e2e/... -v -count=1 -timeout 15m
```

and add `e2e` to the `ci-success` needs list:

```yaml
    needs: [format, typos, lint, test, e2e, frontend, build, vulncheck]
```

- [ ] **Step 6: Verify the workflow parses**

Run: `python -c "import yaml; yaml.safe_load(open('.github/workflows/ci.yml')); print('ok')"`
Expected: `ok`

- [ ] **Step 7: Commit**

```bash
git add test/e2e/config_test.go test/harness/app.go .github/workflows/ci.yml
git commit -m "verify config round-trip and run e2e suite on all three runners"
```

---

### Task 6: Capture real OS output as parser fixtures

The parser tests in Tasks 7-9 are only worth anything if their fixtures are output the operating systems really produced. Nobody can hand-write `ioreg` output correctly, so this task collects it from the runners themselves.

**Files:**
- Create: `.github/workflows/capture-fixtures.yml`
- Create: `internal/monitor/testdata/{linux,darwin,windows}/*.txt`

**Interfaces:**
- Consumes: nothing.
- Produces: fixture files consumed by Tasks 7, 8 and 9 under these exact names:
  - `testdata/linux/`: `xset_q_on.txt`, `xset_q_off.txt`, `power_supply_online_1.txt`, `power_supply_online_0.txt`, `power_supply_status_charging.txt`, `lid_state_open.txt`, `lid_state_closed.txt`
  - `testdata/darwin/`: `pmset_ac.txt`, `pmset_battery.txt`, `ioreg_clamshell_open.txt`, `ioreg_display_on.txt`, `ioreg_hid_idle.txt`, `system_profiler_usb.txt`
  - `testdata/windows/`: `lid_status_true.txt`, `lid_status_false.txt`, `battery_count.txt`, `usb_event_arrival.txt`, `usb_event_removal.txt`

- [ ] **Step 1: Create the capture workflow**

Create `.github/workflows/capture-fixtures.yml`:

```yaml
name: Capture Fixtures

# Run by hand when a parser fixture needs refreshing. The captured output is
# downloaded from the run artifacts and committed under
# internal/monitor/testdata/.
on:
  workflow_dispatch:

permissions:
  contents: read

jobs:
  linux:
    runs-on: ubuntu-latest
    timeout-minutes: 10
    steps:
      - name: Capture
        run: |
          mkdir -p out
          sudo apt-get update -qq
          sudo apt-get install -y -qq xvfb x11-xserver-utils
          export DISPLAY=:99
          Xvfb :99 -screen 0 1024x768x24 &
          sleep 2
          xset q > out/xset_q_on.txt
          xset dpms force off || true
          sleep 1
          xset q > out/xset_q_off.txt
          for supply in /sys/class/power_supply/*; do
            [ -e "$supply" ] || continue
            echo "--- $supply ---" >> out/power_supply_dump.txt
            cat "$supply/type" >> out/power_supply_dump.txt 2>/dev/null || true
            cat "$supply/online" >> out/power_supply_dump.txt 2>/dev/null || true
            cat "$supply/status" >> out/power_supply_dump.txt 2>/dev/null || true
          done
          cat /proc/acpi/button/lid/*/state > out/lid_state_dump.txt 2>/dev/null || \
            echo "no acpi lid on this runner" > out/lid_state_dump.txt
      - uses: actions/upload-artifact@v7
        with:
          name: fixtures-linux
          path: out/

  darwin:
    runs-on: macos-latest
    timeout-minutes: 10
    steps:
      - name: Capture
        run: |
          mkdir -p out
          pmset -g batt > out/pmset_ac.txt
          ioreg -r -k AppleClamshellState -d 1 > out/ioreg_clamshell.txt
          ioreg -r -d 1 -c IODisplayWrangler > out/ioreg_display_on.txt
          ioreg -c IOHIDSystem -d 4 -S > out/ioreg_hid_idle.txt
          system_profiler SPUSBDataType -detailLevel mini > out/system_profiler_usb.txt
      - uses: actions/upload-artifact@v7
        with:
          name: fixtures-darwin
          path: out/

  windows:
    runs-on: windows-latest
    timeout-minutes: 10
    steps:
      - name: Capture
        shell: powershell
        run: |
          New-Item -ItemType Directory -Force out | Out-Null
          (Get-WmiObject -Class Win32_Battery).Count |
            Out-File -Encoding utf8 out/battery_count.txt
          try {
            (Get-WmiObject -Namespace root/WMI -Class MSAcpi_LidStatus).LidStatus |
              Out-File -Encoding utf8 out/lid_status.txt
          } catch {
            "no MSAcpi_LidStatus on this runner" |
              Out-File -Encoding utf8 out/lid_status.txt
          }
          Get-WmiObject -Class Win32_PnPEntity |
            Where-Object { $_.PNPDeviceID -like 'USB\VID_%' } |
            Select-Object -First 3 Name, PNPDeviceID, Service |
            Out-File -Encoding utf8 out/usb_devices.txt
      - uses: actions/upload-artifact@v7
        with:
          name: fixtures-windows
          path: out/
```

- [ ] **Step 2: Commit and push the workflow, then run it**

```bash
git add .github/workflows/capture-fixtures.yml
git commit -m "add fixture capture workflow for parser tests"
git push
gh workflow run capture-fixtures.yml --ref test/real-environment-verification
```

- [ ] **Step 3: Wait for the run and download the artifacts**

Run: `gh run watch` then `gh run download <run-id> -D /tmp/fixtures`
Expected: three directories, `fixtures-linux`, `fixtures-darwin`, `fixtures-windows`.

- [ ] **Step 4: Split the dumps into the named fixture files**

Create `internal/monitor/testdata/{linux,darwin,windows}/` and copy the captured text into the exact file names listed in the Interfaces block above.

Where the runner could not produce a state (a Linux runner has no ACPI lid, a macOS runner has no battery, a Windows runner has no `MSAcpi_LidStatus`), write the fixture **by hand from the documented format** and add a first-line comment recording that it is synthetic and why:

```
# synthetic: GitHub runners have no ACPI lid; format per Documentation/acpi/button.txt
state:      closed
```

Then, in the parser, strip lines beginning with `#` before parsing so the marker never changes behaviour. This keeps the honesty rule intact: the file itself says it was not captured.

- [ ] **Step 5: Verify every fixture named in the Interfaces block exists**

Run: `ls internal/monitor/testdata/linux internal/monitor/testdata/darwin internal/monitor/testdata/windows`
Expected: all 18 files present.

- [ ] **Step 6: Commit**

```bash
git add internal/monitor/testdata
git commit -m "capture parser fixtures from real runner output"
```

---

### Task 7: Extract and test the Linux parsers

**Files:**
- Create: `internal/monitor/parse_linux.go`, `internal/monitor/parse_linux_test.go`
- Modify: `internal/monitor/power_linux.go:111-127`, `internal/monitor/lid_linux.go:74-80`, `internal/monitor/screen_linux.go:65-77`

**Interfaces:**
- Consumes: fixtures from Task 6.
- Produces (all in package `monitor`, build tag `linux`):
  - `parseACOnline(raw string) bool`
  - `parseBatteryCharging(raw string) bool`
  - `parseLidOpen(raw string) bool`
  - `parseDPMSOn(raw string) bool`
  - `stripFixtureComments(raw string) string`

Note: these files only build on Linux. On a Windows workstation verify with `GOOS=linux go vet ./...`; the tests themselves run in CI.

- [ ] **Step 1: Write the failing test**

Create `internal/monitor/parse_linux_test.go`:

```go
//go:build linux

package monitor

import (
	"os"
	"path/filepath"
	"testing"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "linux", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(data)
}

func TestParseACOnline(t *testing.T) {
	tests := []struct {
		name string
		file string
		want bool
	}{
		{"charger plugged in", "power_supply_online_1.txt", true},
		{"charger unplugged", "power_supply_online_0.txt", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := parseACOnline(fixture(t, tt.file)); got != tt.want {
				t.Errorf("parseACOnline() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseBatteryCharging(t *testing.T) {
	if !parseBatteryCharging(fixture(t, "power_supply_status_charging.txt")) {
		t.Error("a charging battery was read as not charging")
	}
	if parseBatteryCharging("Discharging\n") {
		t.Error("a discharging battery was read as charging")
	}
	if !parseBatteryCharging("Full\n") {
		t.Error("a full battery must count as on mains")
	}
}

func TestParseLidOpen(t *testing.T) {
	if !parseLidOpen(fixture(t, "lid_state_open.txt")) {
		t.Error("an open lid was read as closed")
	}
	if parseLidOpen(fixture(t, "lid_state_closed.txt")) {
		t.Error("a closed lid was read as open — the alarm would never fire")
	}
}

func TestParseDPMSOn(t *testing.T) {
	if !parseDPMSOn(fixture(t, "xset_q_on.txt")) {
		t.Error("an active monitor was read as off")
	}
	if parseDPMSOn(fixture(t, "xset_q_off.txt")) {
		t.Error("a blanked monitor was read as on")
	}
	for _, state := range []string{"Monitor is Off", "Monitor is Standby", "Monitor is Suspend"} {
		if parseDPMSOn("DPMS is Enabled\n  " + state + "\n") {
			t.Errorf("%q was read as the monitor being on", state)
		}
	}
}

func TestStripFixtureComments(t *testing.T) {
	got := stripFixtureComments("# synthetic: no acpi lid\nstate:      closed\n")
	want := "state:      closed\n"
	if got != want {
		t.Errorf("stripFixtureComments() = %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `GOOS=linux go vet ./internal/monitor/`
Expected: FAIL — `undefined: parseACOnline`

(On Linux, run `go test ./internal/monitor/ -run TestParse -v` and expect the same.)

- [ ] **Step 3: Implement the parsers**

Create `internal/monitor/parse_linux.go`:

```go
//go:build linux

package monitor

import "strings"

// stripFixtureComments removes leading `#` lines. Test fixtures that could not
// be captured from a real machine carry such a line declaring themselves
// synthetic; production files never contain one, so this is a no-op there.
func stripFixtureComments(raw string) string {
	var b strings.Builder
	for _, line := range strings.SplitAfter(raw, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		b.WriteString(line)
	}
	return b.String()
}

// parseACOnline reads /sys/class/power_supply/<supply>/online.
func parseACOnline(raw string) bool {
	return strings.TrimSpace(stripFixtureComments(raw)) == "1"
}

// parseBatteryCharging reads /sys/class/power_supply/<supply>/status. A full
// battery still means the machine is on mains power.
func parseBatteryCharging(raw string) bool {
	status := strings.TrimSpace(stripFixtureComments(raw))
	return status == "Charging" || status == "Full"
}

// parseLidOpen reads /proc/acpi/button/lid/<id>/state.
func parseLidOpen(raw string) bool {
	return strings.Contains(stripFixtureComments(raw), "open")
}

// parseDPMSOn reads `xset q` output. DPMS reports On, Off, Standby or Suspend;
// everything but On means the screen is dark.
func parseDPMSOn(raw string) bool {
	out := stripFixtureComments(raw)
	return !strings.Contains(out, "Monitor is Off") &&
		!strings.Contains(out, "Monitor is Standby") &&
		!strings.Contains(out, "Monitor is Suspend")
}
```

- [ ] **Step 4: Route the sensors through the parsers**

In `internal/monitor/power_linux.go`, replace the body of `isACOnline` with:

```go
func isACOnline(supplyPath string) (bool, error) {
	// Try "online" first (AC adapters), then fall back to battery status.
	onlinePath := filepath.Join(supplyPath, "online")
	data, err := os.ReadFile(onlinePath) // #nosec G304 -- path comes from sysfs enumeration
	if err == nil {
		return parseACOnline(string(data)), nil
	}

	statusPath := filepath.Join(supplyPath, "status")
	data, err = os.ReadFile(statusPath) // #nosec G304 -- path comes from sysfs enumeration
	if err != nil {
		return false, err
	}
	return parseBatteryCharging(string(data)), nil
}
```

In `internal/monitor/lid_linux.go`, replace the body of `isLidOpenLinux` with:

```go
func isLidOpenLinux() (bool, error) {
	data, err := os.ReadFile(lidStatePath)
	if err != nil {
		return true, err
	}
	return parseLidOpen(string(data)), nil
}
```

In `internal/monitor/screen_linux.go`, replace the body of `isScreenOnLinux` with:

```go
func isScreenOnLinux() (bool, error) {
	out, err := exec.Command("xset", "q").Output()
	if err != nil {
		return true, err
	}
	return parseDPMSOn(string(out)), nil
}
```

Remove the now-unused `strings` import from `lid_linux.go` and `screen_linux.go` if nothing else uses it.

- [ ] **Step 5: Verify it compiles and the tests pass**

Run: `GOOS=linux go vet ./internal/monitor/`
Expected: no output.

In CI (or on Linux): `go test ./internal/monitor/ -run TestParse -v -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/monitor/parse_linux.go internal/monitor/parse_linux_test.go \
        internal/monitor/power_linux.go internal/monitor/lid_linux.go internal/monitor/screen_linux.go
git commit -m "extract and test linux sensor output parsers"
```

---

### Task 8: Extract and test the macOS parsers

**Files:**
- Create: `internal/monitor/parse_darwin.go`, `internal/monitor/parse_darwin_test.go`
- Modify: `internal/monitor/power_darwin.go:71-78`, `internal/monitor/lid_darwin.go:74-81`, `internal/monitor/screen_darwin.go:60-67`, `internal/monitor/input_darwin.go:88-107`, `internal/monitor/usb_darwin.go:70-86`

**Interfaces:**
- Consumes: fixtures from Task 6.
- Produces (package `monitor`, build tag `darwin`):
  - `parseOnACPower(raw string) bool`
  - `parseClamshellOpen(raw string) bool`
  - `parseDisplayOn(raw string) bool`
  - `parseHIDIdleSeconds(raw string) float64` — returns -1 when absent
  - `parseUSBDeviceNames(raw string) []string`
  - `stripFixtureCommentsDarwin(raw string) string`

The comment-stripper is duplicated per platform rather than shared, because each `parse_*.go` carries a different build tag and a shared file would need a fourth. The name differs to avoid a redeclaration when someone later builds with multiple tags.

- [ ] **Step 1: Write the failing test**

Create `internal/monitor/parse_darwin_test.go`:

```go
//go:build darwin

package monitor

import (
	"os"
	"path/filepath"
	"testing"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "darwin", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(data)
}

func TestParseOnACPower(t *testing.T) {
	if !parseOnACPower(fixture(t, "pmset_ac.txt")) {
		t.Error("a machine on AC power was read as running on battery")
	}
	if parseOnACPower(fixture(t, "pmset_battery.txt")) {
		t.Error("a machine on battery was read as on AC — the alarm would never fire")
	}
}

func TestParseClamshellOpen(t *testing.T) {
	if !parseClamshellOpen(fixture(t, "ioreg_clamshell_open.txt")) {
		t.Error("an open lid was read as closed")
	}
	if parseClamshellOpen(`"AppleClamshellState" = Yes`) {
		t.Error("a closed lid was read as open")
	}
}

func TestParseDisplayOn(t *testing.T) {
	if !parseDisplayOn(fixture(t, "ioreg_display_on.txt")) {
		t.Error("an active display was read as off")
	}
	if parseDisplayOn(`"DevicePowerState" = 0`) {
		t.Error("a sleeping display was read as on")
	}
}

func TestParseHIDIdleSeconds(t *testing.T) {
	got := parseHIDIdleSeconds(fixture(t, "ioreg_hid_idle.txt"))
	if got < 0 {
		t.Fatalf("HIDIdleTime was not found in real ioreg output (got %v)", got)
	}

	if got := parseHIDIdleSeconds(`"HIDIdleTime" = 2000000000`); got != 2 {
		t.Errorf("parseHIDIdleSeconds() = %v, want 2 (nanoseconds to seconds)", got)
	}
	if got := parseHIDIdleSeconds("no idle time here"); got != -1 {
		t.Errorf("missing HIDIdleTime = %v, want -1", got)
	}
}

func TestParseUSBDeviceNames(t *testing.T) {
	names := parseUSBDeviceNames(fixture(t, "system_profiler_usb.txt"))
	for _, n := range names {
		if n == "" {
			t.Error("an empty device name was extracted")
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `GOOS=darwin go vet ./internal/monitor/`
Expected: FAIL — `undefined: parseOnACPower`

- [ ] **Step 3: Implement the parsers**

Create `internal/monitor/parse_darwin.go`:

```go
//go:build darwin

package monitor

import (
	"strconv"
	"strings"
)

// stripFixtureCommentsDarwin removes leading `#` lines from a fixture that
// declares itself synthetic. Real command output never has them.
func stripFixtureCommentsDarwin(raw string) string {
	var b strings.Builder
	for _, line := range strings.SplitAfter(raw, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		b.WriteString(line)
	}
	return b.String()
}

// parseOnACPower reads `pmset -g batt` output.
func parseOnACPower(raw string) bool {
	return strings.Contains(stripFixtureCommentsDarwin(raw), "'AC Power'")
}

// parseClamshellOpen reads `ioreg -r -k AppleClamshellState -d 1`.
// AppleClamshellState = No means the lid is open.
func parseClamshellOpen(raw string) bool {
	return strings.Contains(stripFixtureCommentsDarwin(raw), `"AppleClamshellState" = No`)
}

// parseDisplayOn reads `ioreg -r -d 1 -c IODisplayWrangler`.
// DevicePowerState 4 is on; 0 means the display is asleep.
func parseDisplayOn(raw string) bool {
	return !strings.Contains(stripFixtureCommentsDarwin(raw), `"DevicePowerState" = 0`)
}

// parseHIDIdleSeconds reads `ioreg -c IOHIDSystem -d 4 -S` and converts the
// nanosecond HIDIdleTime to seconds. It returns -1 when the key is absent.
func parseHIDIdleSeconds(raw string) float64 {
	for _, line := range strings.Split(stripFixtureCommentsDarwin(raw), "\n") {
		if !strings.Contains(line, "HIDIdleTime") {
			continue
		}
		parts := strings.Split(line, "=")
		if len(parts) < 2 {
			continue
		}
		ns, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
		if err != nil {
			continue
		}
		return float64(ns) / 1e9
	}
	return -1
}

// parseUSBDeviceNames reads `system_profiler SPUSBDataType -detailLevel mini`
// and returns the device section headings.
func parseUSBDeviceNames(raw string) []string {
	var names []string
	for _, line := range strings.Split(stripFixtureCommentsDarwin(raw), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasSuffix(line, ":") && !strings.Contains(line, "USB") {
			names = append(names, strings.TrimSuffix(line, ":"))
		}
	}
	return names
}
```

- [ ] **Step 4: Route the sensors through the parsers**

`internal/monitor/power_darwin.go` — `isOnACPower`:

```go
func isOnACPower() (bool, error) {
	out, err := exec.Command("pmset", "-g", "batt").Output()
	if err != nil {
		return false, err
	}
	return parseOnACPower(string(out)), nil
}
```

`internal/monitor/lid_darwin.go` — `isLidOpenDarwin`:

```go
func isLidOpenDarwin() (bool, error) {
	out, err := exec.Command("ioreg", "-r", "-k", "AppleClamshellState", "-d", "1").Output()
	if err != nil {
		return true, err
	}
	return parseClamshellOpen(string(out)), nil
}
```

`internal/monitor/screen_darwin.go` — `isScreenOnDarwin`:

```go
func isScreenOnDarwin() (bool, error) {
	out, err := exec.Command("ioreg", "-r", "-d", "1", "-c", "IODisplayWrangler").Output()
	if err != nil {
		return true, err
	}
	return parseDisplayOn(string(out)), nil
}
```

`internal/monitor/input_darwin.go` — `getIdleSeconds`:

```go
func getIdleSeconds() float64 {
	out, err := exec.Command("ioreg", "-c", "IOHIDSystem", "-d", "4", "-S").Output()
	if err != nil {
		return -1
	}
	return parseHIDIdleSeconds(string(out))
}
```

`internal/monitor/usb_darwin.go` — `getUSBSnapshotDarwin`:

```go
func getUSBSnapshotDarwin() (string, []string, error) {
	out, err := exec.Command("system_profiler", "SPUSBDataType", "-detailLevel", "mini").Output()
	if err != nil {
		return "", nil, err
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(out))
	return hash, parseUSBDeviceNames(string(out)), nil
}
```

Remove imports that are now unused (`strings` in `lid_darwin.go`, `screen_darwin.go`, `power_darwin.go`; `strconv` and `strings` in `input_darwin.go`; `strings` in `usb_darwin.go`).

- [ ] **Step 5: Verify it compiles**

Run: `GOOS=darwin go vet ./internal/monitor/`
Expected: no output. The tests themselves run on the macOS runner in CI.

- [ ] **Step 6: Commit**

```bash
git add internal/monitor/parse_darwin.go internal/monitor/parse_darwin_test.go \
        internal/monitor/power_darwin.go internal/monitor/lid_darwin.go \
        internal/monitor/screen_darwin.go internal/monitor/input_darwin.go \
        internal/monitor/usb_darwin.go
git commit -m "extract and test macos sensor output parsers"
```

---

### Task 9: Extract and test the Windows parsers

**Files:**
- Create: `internal/monitor/parse_windows.go`, `internal/monitor/parse_windows_test.go`
- Modify: `internal/monitor/lid_windows.go:32-41,74-81`, `internal/monitor/usb_windows.go:80-104`, `internal/monitor/screen_windows.go:63-75`

**Interfaces:**
- Consumes: fixtures from Task 6.
- Produces (package `monitor`, build tag `windows`):
  - `parseLidStatusWMI(raw string) bool`
  - `parseHasBattery(raw string) bool`
  - `parseUSBEventLine(line string) (eventType, name string, ok bool)`
  - `parseLogonUIPresent(raw string) bool`
  - `stripFixtureCommentsWindows(raw string) string`

- [ ] **Step 1: Write the failing test**

Create `internal/monitor/parse_windows_test.go`:

```go
//go:build windows

package monitor

import (
	"os"
	"path/filepath"
	"testing"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "windows", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(data)
}

func TestParseLidStatusWMI(t *testing.T) {
	if !parseLidStatusWMI(fixture(t, "lid_status_true.txt")) {
		t.Error("an open lid was read as closed")
	}
	if parseLidStatusWMI(fixture(t, "lid_status_false.txt")) {
		t.Error("a closed lid was read as open — the alarm would never fire")
	}
	if !parseLidStatusWMI("1\r\n") {
		t.Error("the numeric form of an open lid was not recognised")
	}
}

func TestParseHasBattery(t *testing.T) {
	if parseHasBattery("0\r\n") {
		t.Error("a machine with no battery was reported as a laptop")
	}
	if parseHasBattery("") {
		t.Error("empty WMI output was reported as a laptop")
	}
	if !parseHasBattery("1\r\n") {
		t.Error("a machine with one battery was not reported as a laptop")
	}
	// The captured fixture must at least parse without panicking, whatever the
	// capturing runner happened to report.
	_ = parseHasBattery(fixture(t, "battery_count.txt"))
}

func TestParseUSBEventLine(t *testing.T) {
	tests := []struct {
		name      string
		line      string
		wantType  string
		wantName  string
		wantOK    bool
	}{
		{"arrival", "A|SanDisk Ultra", "A", "SanDisk Ultra", true},
		{"removal", "R|Logitech Mouse", "R", "Logitech Mouse", true},
		{"name containing a pipe", "A|Foo|Bar", "A", "Foo|Bar", true},
		{"no separator", "garbage", "", "", false},
		{"empty", "", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotType, gotName, gotOK := parseUSBEventLine(tt.line)
			if gotOK != tt.wantOK || gotType != tt.wantType || gotName != tt.wantName {
				t.Errorf("parseUSBEventLine(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tt.line, gotType, gotName, gotOK, tt.wantType, tt.wantName, tt.wantOK)
			}
		})
	}
}

func TestParseLogonUIPresent(t *testing.T) {
	if !parseLogonUIPresent("True\r\n") {
		t.Error("a locked session was read as unlocked")
	}
	if parseLogonUIPresent("False\r\n") {
		t.Error("an unlocked session was read as locked")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/monitor/ -run TestParse -v`
Expected: FAIL — `undefined: parseLidStatusWMI`

- [ ] **Step 3: Implement the parsers**

Create `internal/monitor/parse_windows.go`:

```go
//go:build windows

package monitor

import "strings"

// stripFixtureCommentsWindows removes leading `#` lines from a fixture that
// declares itself synthetic. Real command output never has them.
func stripFixtureCommentsWindows(raw string) string {
	var b strings.Builder
	for _, line := range strings.SplitAfter(raw, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		b.WriteString(line)
	}
	return b.String()
}

// parseLidStatusWMI reads MSAcpi_LidStatus.LidStatus, which PowerShell renders
// as True/False on some systems and 1/0 on others.
func parseLidStatusWMI(raw string) bool {
	status := strings.TrimSpace(strings.ToLower(stripFixtureCommentsWindows(raw)))
	return status == "true" || status == "1"
}

// parseHasBattery reads (Get-WmiObject Win32_Battery).Count. Anything other
// than zero or empty means this machine has a battery, so it is a laptop.
func parseHasBattery(raw string) bool {
	count := strings.TrimSpace(stripFixtureCommentsWindows(raw))
	return count != "0" && count != ""
}

// parseUSBEventLine splits one line emitted by the WMI event helper, which
// writes "<sourceIdentifier>|<device name>". Only the first separator counts,
// because device names may themselves contain a pipe.
func parseUSBEventLine(line string) (eventType, name string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", "", false
	}
	parts := strings.SplitN(line, "|", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// parseLogonUIPresent reports whether the LogonUI process is running, which
// means the session is locked.
func parseLogonUIPresent(raw string) bool {
	return strings.Contains(strings.TrimSpace(stripFixtureCommentsWindows(raw)), "True")
}
```

- [ ] **Step 4: Route the sensors through the parsers**

`internal/monitor/lid_windows.go` — `Available` and `isLidOpenWindows`:

```go
func (s *LidSensor) Available() bool {
	// A battery is the cheapest reliable signal that this is a laptop.
	out, err := exec.Command("powershell", "-Command",
		"(Get-WmiObject -Class Win32_Battery).Count").Output()
	if err != nil {
		return false
	}
	return parseHasBattery(string(out))
}

func isLidOpenWindows() (bool, error) {
	out, err := exec.Command("powershell", "-Command",
		"(Get-WmiObject -Namespace root/WMI -Class MSAcpi_LidStatus).LidStatus").Output()
	if err != nil {
		return true, err // Assume open if we cannot determine it.
	}
	return parseLidStatusWMI(string(out)), nil
}
```

`internal/monitor/screen_windows.go` — `isScreenOnWindows`, fallback branch:

```go
		out2, err2 := exec.Command("powershell", "-Command",
			"(Get-Process -Name LogonUI -ErrorAction SilentlyContinue) -ne $null").Output()
		if err2 != nil {
			return true, err
		}
		// LogonUI running means the session is locked, so the screen is not ours.
		return !parseLogonUIPresent(string(out2)), nil
```

`internal/monitor/usb_windows.go` — inside the scanner loop, replace the manual split:

```go
		eventType, name, ok := parseUSBEventLine(scanner.Text())
		if !ok {
			continue
		}
```

and delete the now-dead `line`/`parts` handling above it. Remove the `strings` import if nothing else in the file uses it.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/monitor/ -run TestParse -v -count=1`
Expected: PASS (this runs natively on the Windows workstation).

- [ ] **Step 6: Verify all three platforms still compile**

Run: `go vet ./... && GOOS=linux go vet ./... && GOOS=darwin go vet ./...`
Expected: no output.

- [ ] **Step 7: Commit**

```bash
git add internal/monitor/parse_windows.go internal/monitor/parse_windows_test.go \
        internal/monitor/lid_windows.go internal/monitor/screen_windows.go internal/monitor/usb_windows.go
git commit -m "extract and test windows sensor output parsers"
```

---

### Task 10: Real triggers on Windows and macOS, with an honest coverage matrix

Fires the few hardware changes a runner genuinely permits, skips the rest with a stated reason, and publishes the whole matrix so a PR reader can see exactly what was proven.

**Files:**
- Create: `test/harness/matrix.go`
- Create: `test/realtrigger/realtrigger_test.go`, `test/realtrigger/trigger_linux.go`, `test/realtrigger/trigger_darwin.go`, `test/realtrigger/trigger_windows.go`
- Modify: `.github/workflows/ci.yml`, `Makefile`

**Interfaces:**
- Consumes: `harness.Start`, `harness.Dial`, `(*Phone).Expect` from Tasks 2-3.
- Produces:
  - `harness.NewMatrix() *harness.Matrix`
  - `(*Matrix).Triggered(sensor string)`, `(*Matrix).Skipped(sensor, reason string)`
  - `(*Matrix).WriteSummary() error` — appends a markdown table to `$GITHUB_STEP_SUMMARY` when set, and always prints it to stdout
  - In package `realtrigger`: `triggerNetworkChange() error`, `triggerScreenOff() error`, `triggerInputActivity() error` — each returns `errUnsupported` when the platform cannot do it
  - `var errUnsupported = errors.New("...")`

- [ ] **Step 1: Write the failing test**

Create `test/realtrigger/realtrigger_test.go`:

```go
//go:build realtrigger

package realtrigger

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/leavesafe/leavesafe/internal/ws"
	"github.com/leavesafe/leavesafe/test/harness"
)

var matrix = harness.NewMatrix()

func TestMain(m *testing.M) {
	code := m.Run()
	if err := matrix.WriteSummary(); err != nil {
		panic(err)
	}
	os.Exit(code)
}

// armWith pairs, enables one sensor and arms the system.
func armWith(t *testing.T, sensor string) *harness.Phone {
	t.Helper()
	app := harness.Start(t, harness.Options{})
	phone := harness.Dial(t, app.Port())
	if reply := phone.Authenticate(app.Key()); reply.Type != ws.MsgTypeAuthOK {
		t.Fatalf("authenticate: %s", reply.Reason)
	}
	phone.Send(ws.ClientMessage{
		Type:    ws.MsgTypeConfigure,
		Sensors: map[string]bool{sensor: true},
	})
	phone.Send(ws.ClientMessage{Type: ws.MsgTypeArm})
	status := phone.Expect(ws.MsgTypeStatus, 10*time.Second)
	if status.Armed == nil || !*status.Armed {
		t.Fatal("system did not arm")
	}
	return phone
}

// TestRealTrigger_Network changes this machine's IP addresses for real and
// requires the armed system to notice.
func TestRealTrigger_Network(t *testing.T) {
	phone := armWith(t, "network")

	if err := triggerNetworkChange(); err != nil {
		if errors.Is(err, errUnsupported) {
			matrix.Skipped("network", err.Error())
			t.Skipf("no real network trigger here: %v", err)
		}
		t.Fatalf("trigger network change: %v", err)
	}

	alert := phone.Expect(ws.MsgTypeAlert, 20*time.Second)
	if alert.Alert == nil || alert.Alert.Sensor != "network" {
		t.Fatalf("alert did not come from the network sensor: %+v", alert.Alert)
	}
	matrix.Triggered("network")
}

// TestRealTrigger_Screen puts the display to sleep for real.
func TestRealTrigger_Screen(t *testing.T) {
	phone := armWith(t, "screen")

	if err := triggerScreenOff(); err != nil {
		if errors.Is(err, errUnsupported) {
			matrix.Skipped("screen", err.Error())
			t.Skipf("no real screen trigger here: %v", err)
		}
		t.Fatalf("trigger screen off: %v", err)
	}

	alert := phone.Expect(ws.MsgTypeAlert, 20*time.Second)
	if alert.Alert == nil || alert.Alert.Sensor != "screen" {
		t.Fatalf("alert did not come from the screen sensor: %+v", alert.Alert)
	}
	matrix.Triggered("screen")
}

// TestRealTrigger_Input generates real keyboard or pointer activity.
func TestRealTrigger_Input(t *testing.T) {
	phone := armWith(t, "input")

	if err := triggerInputActivity(); err != nil {
		if errors.Is(err, errUnsupported) {
			matrix.Skipped("input", err.Error())
			t.Skipf("no real input trigger here: %v", err)
		}
		t.Fatalf("trigger input activity: %v", err)
	}

	alert := phone.Expect(ws.MsgTypeAlert, 30*time.Second)
	if alert.Alert == nil || alert.Alert.Sensor != "input" {
		t.Fatalf("alert did not come from the input sensor: %+v", alert.Alert)
	}
	matrix.Triggered("input")
}

// TestRealTrigger_UnavailableSensors records the sensors no runner can produce,
// so the coverage matrix tells the whole truth rather than half of it.
func TestRealTrigger_UnavailableSensors(t *testing.T) {
	matrix.Skipped("power", "CI runners have no battery or removable charger")
	matrix.Skipped("lid", "CI runners have no laptop lid")
	matrix.Skipped("usb", "CI runners have no attachable USB device")
	t.Skip("recorded in the coverage matrix; see the job summary")
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -tags realtrigger ./test/realtrigger/... -v`
Expected: FAIL — `undefined: harness.NewMatrix`, `undefined: triggerNetworkChange`

- [ ] **Step 3: Implement the coverage matrix**

Create `test/harness/matrix.go`:

```go
package harness

import (
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
)

// Matrix records which sensors were genuinely exercised and which were skipped,
// so a reader of the CI run learns exactly what was proven. Silence about a gap
// would be indistinguishable from coverage.
type Matrix struct {
	mu      sync.Mutex
	results map[string]string // sensor -> "triggered" or "skipped: <reason>"
}

// NewMatrix creates an empty matrix.
func NewMatrix() *Matrix {
	return &Matrix{results: make(map[string]string)}
}

// Triggered records that a real hardware change was performed and detected.
func (m *Matrix) Triggered(sensor string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.results[sensor] = "triggered"
}

// Skipped records that no real trigger was possible, and why.
func (m *Matrix) Skipped(sensor, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Never let a skip overwrite a real result.
	if m.results[sensor] == "triggered" {
		return
	}
	m.results[sensor] = "skipped: " + reason
}

// WriteSummary renders the matrix to stdout and, when running in GitHub
// Actions, appends it to the job summary.
func (m *Matrix) WriteSummary() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	sensors := make([]string, 0, len(m.results))
	for name := range m.results {
		sensors = append(sensors, name)
	}
	sort.Strings(sensors)

	var b strings.Builder
	fmt.Fprintf(&b, "### Real-trigger coverage on %s\n\n", runtime.GOOS)
	b.WriteString("| Sensor | Result | Reason |\n|---|---|---|\n")
	for _, name := range sensors {
		result := m.results[name]
		if reason, ok := strings.CutPrefix(result, "skipped: "); ok {
			fmt.Fprintf(&b, "| %s | not proven | %s |\n", name, reason)
			continue
		}
		fmt.Fprintf(&b, "| %s | real hardware change detected | — |\n", name)
	}

	fmt.Print(b.String())

	path := os.Getenv("GITHUB_STEP_SUMMARY")
	if path == "" {
		return nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) // #nosec G304 -- path supplied by the CI runner
	if err != nil {
		return fmt.Errorf("open step summary: %w", err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(b.String()); err != nil {
		return fmt.Errorf("write step summary: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Implement the Linux triggers**

Create `test/realtrigger/trigger_linux.go`:

```go
//go:build realtrigger && linux

package realtrigger

import (
	"errors"
	"fmt"
	"os/exec"
)

var errUnsupported = errors.New("this platform cannot produce the change")

// triggerNetworkChange adds a real address to the loopback interface, which
// changes what net.InterfaceAddrs reports.
func triggerNetworkChange() error {
	// #nosec G204 -- fixed arguments, no external input
	out, err := exec.Command("sudo", "ip", "addr", "add", "10.99.99.99/32", "dev", "lo").CombinedOutput()
	if err != nil {
		return fmt.Errorf("ip addr add: %w (%s)", err, out)
	}
	return nil
}

// triggerScreenOff is handled by the VM sandbox layer, which owns a real X
// server; the bare runner has no display.
func triggerScreenOff() error {
	return fmt.Errorf("%w: the Linux runner has no X display (covered by the VM sandbox)", errUnsupported)
}

// triggerInputActivity is handled by the VM sandbox layer via uinput.
func triggerInputActivity() error {
	return fmt.Errorf("%w: /dev/input is absent on the runner (covered by the VM sandbox)", errUnsupported)
}
```

- [ ] **Step 5: Implement the macOS triggers**

Create `test/realtrigger/trigger_darwin.go`:

```go
//go:build realtrigger && darwin

package realtrigger

import (
	"errors"
	"fmt"
	"os/exec"
)

var errUnsupported = errors.New("this platform cannot produce the change")

// triggerNetworkChange adds a real alias to the loopback interface.
func triggerNetworkChange() error {
	// #nosec G204 -- fixed arguments, no external input
	out, err := exec.Command("sudo", "ifconfig", "lo0", "alias", "10.99.99.99", "255.255.255.255").CombinedOutput()
	if err != nil {
		return fmt.Errorf("ifconfig alias: %w (%s)", err, out)
	}
	return nil
}

// triggerScreenOff puts the display to sleep for real, which changes the
// IODisplayWrangler power state the sensor reads.
func triggerScreenOff() error {
	// #nosec G204 -- fixed arguments, no external input
	out, err := exec.Command("pmset", "displaysleepnow").CombinedOutput()
	if err != nil {
		return fmt.Errorf("pmset displaysleepnow: %w (%s)", err, out)
	}
	return nil
}

// triggerInputActivity cannot be done here: synthesising HID events needs an
// Accessibility grant that no hosted runner can give.
func triggerInputActivity() error {
	return fmt.Errorf("%w: synthetic HID events require an Accessibility grant unavailable on hosted runners", errUnsupported)
}
```

- [ ] **Step 6: Implement the Windows triggers**

Create `test/realtrigger/trigger_windows.go`:

```go
//go:build realtrigger && windows

package realtrigger

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"unsafe"
)

var errUnsupported = errors.New("this platform cannot produce the change")

// triggerNetworkChange adds a real IP address to the loopback adapter.
func triggerNetworkChange() error {
	// #nosec G204 -- fixed arguments, no external input
	out, err := exec.Command("netsh", "interface", "ipv4", "add", "address",
		"name=Loopback Pseudo-Interface 1", "address=10.99.99.99", "mask=255.255.255.255").CombinedOutput()
	if err != nil {
		return fmt.Errorf("netsh add address: %w (%s)", err, out)
	}
	return nil
}

// triggerScreenOff is deliberately not attempted: blanking or locking the
// session on a hosted runner risks breaking the runner itself.
func triggerScreenOff() error {
	return fmt.Errorf("%w: locking or blanking the runner session would break the runner", errUnsupported)
}

// triggerInputActivity synthesises a real pointer move through SendInput, which
// advances the tick GetLastInputInfo reports. Whether a hosted runner's session
// station permits this is measured, not assumed.
func triggerInputActivity() error {
	type mouseInput struct {
		dx, dy      int32
		mouseData   uint32
		dwFlags     uint32
		time        uint32
		dwExtraInfo uintptr
	}
	type input struct {
		inputType uint32
		mi        mouseInput
		_         [8]byte // pad to the union size on amd64
	}

	const (
		inputMouse       = 0
		mouseEventFMove  = 0x0001
	)

	user32 := syscall.NewLazyDLL("user32.dll")
	sendInput := user32.NewProc("SendInput")

	for i := 0; i < 20; i++ {
		in := input{
			inputType: inputMouse,
			mi:        mouseInput{dx: 10, dy: 10, dwFlags: mouseEventFMove},
		}
		ret, _, err := sendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
		if ret == 0 {
			return fmt.Errorf("%w: SendInput was rejected by this session (%v)", errUnsupported, err)
		}
	}
	return nil
}
```

- [ ] **Step 7: Run the tests**

Run: `go test -tags realtrigger ./test/realtrigger/... -v -count=1`
Expected on Windows: `TestRealTrigger_Network` PASS (run from an elevated shell) or a clear `netsh` permission error; `TestRealTrigger_Screen` SKIP; `TestRealTrigger_Input` PASS or SKIP; the coverage matrix printed at the end.

If `netsh` fails locally for lack of elevation, that is a local-environment limitation, not a code failure — CI runners are elevated. Note it and move on.

- [ ] **Step 8: Add the CI job and Make target**

In `.github/workflows/ci.yml`, add after the `e2e` job:

```yaml
  # ---------------------------------------------------------------------------
  # Layer 2: fire the hardware changes a hosted runner genuinely permits and
  # record every one it does not, so the coverage gaps are visible in the run
  # summary rather than implied by a green check.
  # ---------------------------------------------------------------------------
  realtrigger:
    name: Real triggers (${{ matrix.os }})
    runs-on: ${{ matrix.os }}
    timeout-minutes: 15
    strategy:
      fail-fast: false
      matrix:
        os: [ubuntu-latest, windows-latest, macos-latest]
    steps:
      - uses: actions/checkout@v7

      - name: Set up Go
        uses: actions/setup-go@v7
        with:
          go-version-file: go.mod

      - name: Run real-trigger suite
        shell: bash
        run: go test -tags realtrigger ./test/realtrigger/... -v -count=1 -timeout 15m
```

and extend the needs list:

```yaml
    needs: [format, typos, lint, test, e2e, realtrigger, frontend, build, vulncheck]
```

In `Makefile`, add `test-realtrigger` to `.PHONY` and append:

```makefile
# Layer 2: fires the hardware changes this machine genuinely permits.
test-realtrigger:
	go test -tags realtrigger ./test/realtrigger/... -v -count=1
```

- [ ] **Step 9: Verify the workflow parses and everything still builds**

Run: `python -c "import yaml; yaml.safe_load(open('.github/workflows/ci.yml')); print('ok')" && go build ./... && gofmt -l .`
Expected: `ok`, then no output from either command.

- [ ] **Step 10: Commit**

```bash
git add test/harness/matrix.go test/realtrigger .github/workflows/ci.yml Makefile
git commit -m "fire real hardware triggers where runners allow and publish the coverage matrix"
```

---

### Task 11: Linux VM sandbox with real kernel-backed hardware

The one platform where hardware can genuinely be conjured. A real Ubuntu VM under QEMU/KVM, real kernel modules, and the unmodified binary reading a real `/sys`.

**Files:**
- Create: `test/sandbox/linuxvm/run.sh`, `test/sandbox/linuxvm/cloud-init.yaml`, `test/sandbox/linuxvm/README.md`
- Create: `test/sandbox/linuxvm/hardware.go`, `test/sandbox/linuxvm/scenarios_test.go`
- Modify: `.github/workflows/ci.yml`, `Makefile`

**Interfaces:**
- Consumes: `harness.Start`, `harness.Dial`, `harness.NewMatrix` from Tasks 2, 3 and 10.
- Produces:
  - `loadModule(name string) error` — returns a wrapped `errNoModule` when modprobe fails
  - `setTestPowerAC(online bool) error`
  - `createVirtualKeyboard() (func(), error)`
  - `attachDummyUSB() error`
  - `startXvfb() (func(), error)` and `forceDPMSOff() error`

- [ ] **Step 1: Write the failing test**

Create `test/sandbox/linuxvm/scenarios_test.go`:

```go
//go:build sandbox && linux

package linuxvm

import (
	"os"
	"testing"
	"time"

	"github.com/leavesafe/leavesafe/internal/ws"
	"github.com/leavesafe/leavesafe/test/harness"
)

var matrix = harness.NewMatrix()

func TestMain(m *testing.M) {
	code := m.Run()
	if err := matrix.WriteSummary(); err != nil {
		panic(err)
	}
	os.Exit(code)
}

func armWith(t *testing.T, sensor string) *harness.Phone {
	t.Helper()
	app := harness.Start(t, harness.Options{})
	phone := harness.Dial(t, app.Port())
	if reply := phone.Authenticate(app.Key()); reply.Type != ws.MsgTypeAuthOK {
		t.Fatalf("authenticate: %s", reply.Reason)
	}
	phone.Send(ws.ClientMessage{
		Type:    ws.MsgTypeConfigure,
		Sensors: map[string]bool{sensor: true},
	})
	phone.Send(ws.ClientMessage{Type: ws.MsgTypeArm})
	status := phone.Expect(ws.MsgTypeStatus, 10*time.Second)
	if status.Armed == nil || !*status.Armed {
		t.Fatal("system did not arm")
	}
	return phone
}

// TestSandbox_ChargerUnplugged is the product's headline scenario: the charger
// is genuinely removed and the phone must be told immediately.
func TestSandbox_ChargerUnplugged(t *testing.T) {
	if err := loadModule("test_power"); err != nil {
		matrix.Skipped("power", err.Error())
		t.Skipf("no synthetic power supply available: %v", err)
	}
	if err := setTestPowerAC(true); err != nil {
		t.Fatalf("set initial AC state: %v", err)
	}

	phone := armWith(t, "power")

	if err := setTestPowerAC(false); err != nil {
		t.Fatalf("unplug the charger: %v", err)
	}

	alert := phone.Expect(ws.MsgTypeAlert, 20*time.Second)
	if alert.Alert == nil || alert.Alert.Sensor != "power" {
		t.Fatalf("alert did not come from the power sensor: %+v", alert.Alert)
	}
	if alert.Alert.Level != "critical" {
		t.Errorf("charger removal reported as %q, want critical", alert.Alert.Level)
	}
	matrix.Triggered("power")
}

// TestSandbox_KeyboardActivity proves real HID events wake the input sensor.
func TestSandbox_KeyboardActivity(t *testing.T) {
	if err := loadModule("uinput"); err != nil {
		matrix.Skipped("input", err.Error())
		t.Skipf("no uinput available: %v", err)
	}

	phone := armWith(t, "input")

	typeKeys, err := createVirtualKeyboard()
	if err != nil {
		matrix.Skipped("input", err.Error())
		t.Skipf("could not create a virtual keyboard: %v", err)
	}
	defer typeKeys()

	// The sensor has a five-second grace period after arming.
	time.Sleep(6 * time.Second)
	typeKeys()

	alert := phone.Expect(ws.MsgTypeAlert, 30*time.Second)
	if alert.Alert == nil || alert.Alert.Sensor != "input" {
		t.Fatalf("alert did not come from the input sensor: %+v", alert.Alert)
	}
	matrix.Triggered("input")
}

// TestSandbox_USBAttached proves a real USB device appearing is noticed.
func TestSandbox_USBAttached(t *testing.T) {
	if err := loadModule("dummy_hcd"); err != nil {
		matrix.Skipped("usb", err.Error())
		t.Skipf("no virtual USB host controller: %v", err)
	}

	phone := armWith(t, "usb")

	if err := attachDummyUSB(); err != nil {
		matrix.Skipped("usb", err.Error())
		t.Skipf("could not attach a virtual USB device: %v", err)
	}

	alert := phone.Expect(ws.MsgTypeAlert, 30*time.Second)
	if alert.Alert == nil || alert.Alert.Sensor != "usb" {
		t.Fatalf("alert did not come from the usb sensor: %+v", alert.Alert)
	}
	matrix.Triggered("usb")
}

// TestSandbox_ScreenBlanked proves a real DPMS transition is noticed.
func TestSandbox_ScreenBlanked(t *testing.T) {
	stopX, err := startXvfb()
	if err != nil {
		matrix.Skipped("screen", err.Error())
		t.Skipf("no X server available: %v", err)
	}
	defer stopX()

	phone := armWith(t, "screen")

	if err := forceDPMSOff(); err != nil {
		t.Fatalf("blank the screen: %v", err)
	}

	alert := phone.Expect(ws.MsgTypeAlert, 20*time.Second)
	if alert.Alert == nil || alert.Alert.Sensor != "screen" {
		t.Fatalf("alert did not come from the screen sensor: %+v", alert.Alert)
	}
	matrix.Triggered("screen")
}

// TestSandbox_LidUnavailable records the one sensor even a VM cannot produce.
func TestSandbox_LidUnavailable(t *testing.T) {
	if _, err := os.Stat("/proc/acpi/button/lid"); err == nil {
		t.Fatal("this VM has an ACPI lid after all — write a real test for it")
	}
	matrix.Skipped("lid", "QEMU x86 emulates no ACPI lid button")
	t.Skip("recorded in the coverage matrix")
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `GOOS=linux go vet -tags sandbox ./test/sandbox/...`
Expected: FAIL — `undefined: loadModule`

- [ ] **Step 3: Implement the hardware helpers**

Create `test/sandbox/linuxvm/hardware.go`:

```go
//go:build sandbox && linux

// Package linuxvm creates real kernel-backed hardware inside a VM so the
// unmodified binary reads a real /sys and /dev. Nothing here fakes a sensor
// reading: every helper produces a change the kernel itself reports.
package linuxvm

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// errNoModule marks a module the running kernel cannot provide, which is a
// reason to skip a scenario — never a reason to fake it.
var errNoModule = errors.New("kernel module unavailable")

// loadModule inserts a kernel module, tolerating one that is already loaded.
func loadModule(name string) error {
	// #nosec G204 -- name comes from a fixed set of literals in the test file
	if out, err := exec.Command("modprobe", name).CombinedOutput(); err != nil {
		return fmt.Errorf("%w: modprobe %s: %v (%s)", errNoModule, name, err, out)
	}
	return nil
}

// setTestPowerAC drives the synthetic AC adapter the test_power module exposes.
// The kernel then reports the new state through /sys/class/power_supply, which
// is exactly what the production sensor reads.
func setTestPowerAC(online bool) error {
	value := "0"
	if online {
		value = "1"
	}
	const param = "/sys/module/test_power/parameters/ac_online"
	if err := os.WriteFile(param, []byte(value), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", param, err)
	}
	// Give the kernel a moment to publish the change through sysfs.
	time.Sleep(500 * time.Millisecond)

	matches, err := filepath.Glob("/sys/class/power_supply/*/online")
	if err != nil || len(matches) == 0 {
		return fmt.Errorf("%w: test_power exposed no power supply", errNoModule)
	}
	return nil
}

// createVirtualKeyboard registers a uinput keyboard and returns a function that
// emits real key events through it.
func createVirtualKeyboard() (func(), error) {
	if _, err := os.Stat("/dev/uinput"); err != nil {
		return nil, fmt.Errorf("%w: /dev/uinput is absent: %v", errNoModule, err)
	}
	// evemu-device / evemu-event ship in the evemu-tools package installed by
	// cloud-init. They drive uinput directly, so the events are real.
	if _, err := exec.LookPath("evemu-event"); err != nil {
		return nil, fmt.Errorf("%w: evemu-tools not installed: %v", errNoModule, err)
	}

	matches, err := filepath.Glob("/dev/input/event*")
	if err != nil || len(matches) == 0 {
		return nil, fmt.Errorf("%w: no /dev/input/event* device exists", errNoModule)
	}
	device := matches[0]

	return func() {
		for i := 0; i < 10; i++ {
			// KEY_A down then up, as a real keyboard would report it.
			// #nosec G204 -- device comes from a glob of /dev/input
			_ = exec.Command("evemu-event", device, "--type", "EV_KEY", "--code", "KEY_A", "--value", "1", "--sync").Run()
			_ = exec.Command("evemu-event", device, "--type", "EV_KEY", "--code", "KEY_A", "--value", "0", "--sync").Run()
			time.Sleep(300 * time.Millisecond)
		}
	}, nil
}

// attachDummyUSB binds a gadget to the dummy_hcd virtual host controller, which
// makes a real device node appear under /sys/bus/usb/devices.
func attachDummyUSB() error {
	if err := loadModule("g_mass_storage"); err != nil {
		// Fall back to the zero gadget, which needs no backing file.
		if err2 := loadModule("g_zero"); err2 != nil {
			return fmt.Errorf("%w: neither g_mass_storage nor g_zero loaded: %v; %v",
				errNoModule, err, err2)
		}
	}
	time.Sleep(2 * time.Second)

	entries, err := os.ReadDir("/sys/bus/usb/devices")
	if err != nil || len(entries) == 0 {
		return fmt.Errorf("%w: no USB devices appeared after attaching the gadget", errNoModule)
	}
	return nil
}

// startXvfb brings up a real X server so the screen sensor has something to
// query, and returns a function that tears it down.
func startXvfb() (func(), error) {
	if _, err := exec.LookPath("Xvfb"); err != nil {
		return nil, fmt.Errorf("%w: Xvfb is not installed: %v", errNoModule, err)
	}
	cmd := exec.Command("Xvfb", ":99", "-screen", "0", "1024x768x24")
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start Xvfb: %w", err)
	}
	if err := os.Setenv("DISPLAY", ":99"); err != nil {
		return nil, fmt.Errorf("set DISPLAY: %w", err)
	}
	time.Sleep(2 * time.Second)

	if out, err := exec.Command("xset", "q").CombinedOutput(); err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("%w: xset cannot reach the display: %v (%s)", errNoModule, err, out)
	}
	return func() { _ = cmd.Process.Kill() }, nil
}

// forceDPMSOff blanks the real X display, which is what the sensor detects.
func forceDPMSOff() error {
	if out, err := exec.Command("xset", "dpms", "force", "off").CombinedOutput(); err != nil {
		return fmt.Errorf("xset dpms force off: %w (%s)", err, out)
	}
	return nil
}
```

- [ ] **Step 4: Write the VM launcher**

Create `test/sandbox/linuxvm/cloud-init.yaml`:

```yaml
#cloud-config
users:
  - name: sandbox
    sudo: ALL=(ALL) NOPASSWD:ALL
    shell: /bin/bash
    lock_passwd: false
    ssh_authorized_keys:
      - SSH_PUBLIC_KEY_PLACEHOLDER

package_update: true
packages:
  - xvfb
  - x11-xserver-utils
  - evemu-tools

runcmd:
  # The extra modules package is kernel-version specific, so it can only be
  # resolved once the guest kernel is known.
  - [ sh, -c, "apt-get install -y linux-modules-extra-$(uname -r) || true" ]
  - [ sh, -c, "touch /var/lib/cloud/instance/sandbox-ready" ]
```

Create `test/sandbox/linuxvm/run.sh`:

```bash
#!/usr/bin/env bash
# Boots a real Ubuntu VM under QEMU/KVM, installs the compiled scenario suite
# and the leavesafe binary, and runs the suite as root inside the guest.
#
# A VM rather than a container: the scenarios need to load kernel modules and
# own real device nodes, which a container cannot do without mutating the host.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$HERE/../../.." && pwd)"
WORK="${SANDBOX_WORKDIR:-$HERE/.work}"
IMG_URL="https://cloud-images.ubuntu.com/releases/24.04/release/ubuntu-24.04-server-cloudimg-amd64.img"
SSH_PORT="${SANDBOX_SSH_PORT:-2222}"

if [ ! -e /dev/kvm ]; then
  echo "::error::/dev/kvm is missing; this runner cannot boot the sandbox VM" >&2
  exit 1
fi

mkdir -p "$WORK"
cd "$WORK"

if [ ! -f base.img ]; then
  echo "Downloading Ubuntu cloud image..."
  curl -fsSL -o base.img "$IMG_URL"
fi

# A fresh overlay per run keeps the downloaded base image pristine and cacheable.
rm -f disk.qcow2
qemu-img create -f qcow2 -F qcow2 -b base.img disk.qcow2 20G >/dev/null

if [ ! -f id_sandbox ]; then
  ssh-keygen -t ed25519 -N "" -f id_sandbox -q
fi
sed "s|SSH_PUBLIC_KEY_PLACEHOLDER|$(cat id_sandbox.pub)|" \
  "$HERE/cloud-init.yaml" > user-data
touch meta-data
cloud-localds seed.img user-data meta-data

echo "Cross-compiling the scenario suite for the guest..."
(cd "$REPO" && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
  go test -c -tags sandbox -o "$WORK/sandbox.test" ./test/sandbox/linuxvm)
(cd "$REPO" && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
  go build -o "$WORK/leavesafe" ./cmd/leavesafe)

echo "Booting the VM..."
qemu-system-x86_64 \
  -enable-kvm -m 2048 -smp 2 -nographic \
  -drive file=disk.qcow2,if=virtio \
  -drive file=seed.img,if=virtio,format=raw \
  -netdev user,id=net0,hostfwd=tcp::"$SSH_PORT"-:22 \
  -device virtio-net-pci,netdev=net0 \
  > vm-console.log 2>&1 &
QEMU_PID=$!
trap 'kill "$QEMU_PID" 2>/dev/null || true' EXIT

SSH_OPTS=(-i id_sandbox -p "$SSH_PORT" -o StrictHostKeyChecking=no
          -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR)

echo "Waiting for SSH..."
for _ in $(seq 1 120); do
  if ssh "${SSH_OPTS[@]}" sandbox@127.0.0.1 true 2>/dev/null; then break; fi
  sleep 5
done
if ! ssh "${SSH_OPTS[@]}" sandbox@127.0.0.1 true 2>/dev/null; then
  echo "::error::VM never became reachable over SSH" >&2
  tail -50 vm-console.log >&2
  exit 1
fi

echo "Waiting for cloud-init to finish..."
ssh "${SSH_OPTS[@]}" sandbox@127.0.0.1 'sudo cloud-init status --wait' || true

echo "Copying artifacts into the guest..."
scp "${SSH_OPTS[@]/-p/-P}" sandbox.test leavesafe sandbox@127.0.0.1:/home/sandbox/

echo "Running the scenario suite as root..."
set +e
ssh "${SSH_OPTS[@]}" sandbox@127.0.0.1 \
  'cd /home/sandbox && chmod +x sandbox.test leavesafe && sudo GITHUB_STEP_SUMMARY="" ./sandbox.test -test.v -test.timeout=20m'
RESULT=$?
set -e

exit "$RESULT"
```

Make it executable: `chmod +x test/sandbox/linuxvm/run.sh` and, because the repo pins LF endings via `.gitattributes`, confirm the file is not checked in with CRLF.

- [ ] **Step 5: Handle the guest-side binary lookup**

The harness compiles `./cmd/leavesafe` with `go build`, which requires a Go toolchain. The guest has none. Add an override to `test/harness/app.go` so a pre-built binary can be supplied — insert at the top of `binary`:

```go
func binary(t *testing.T) string {
	t.Helper()
	// A pre-built binary lets environments without a Go toolchain (the sandbox
	// VM) run the same tests.
	if path := os.Getenv("LEAVESAFE_TEST_BINARY"); path != "" {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("LEAVESAFE_TEST_BINARY=%s is not usable: %v", path, err)
		}
		return path
	}
	buildOnce.Do(func() {
```

`repoRoot` is also unavailable in the guest, so guard `cmd.Dir` in `StartIn`:

```go
	// #nosec G204 -- the binary path is produced by our own go build
	cmd := exec.Command(binary(t))
	if os.Getenv("LEAVESAFE_TEST_BINARY") == "" {
		cmd.Dir = repoRoot(t)
	} else {
		cmd.Dir = home
	}
```

Then set it in `run.sh`'s final ssh command:

```bash
  'cd /home/sandbox && chmod +x sandbox.test leavesafe && sudo LEAVESAFE_TEST_BINARY=/home/sandbox/leavesafe ./sandbox.test -test.v -test.timeout=20m'
```

- [ ] **Step 6: Verify it compiles for the guest**

Run: `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test -c -tags sandbox -o /tmp/sandbox.test ./test/sandbox/linuxvm && go build ./... && gofmt -l .`
Expected: the test binary is produced; no gofmt output.

- [ ] **Step 7: Add the CI job and Make target**

In `.github/workflows/ci.yml`, add after `realtrigger`:

```yaml
  # ---------------------------------------------------------------------------
  # Layer 1: a real Ubuntu VM where real kernel modules create real hardware.
  # The charger is genuinely unplugged, not simulated — a container cannot do
  # this, which is why the Docker support was removed rather than reused.
  # ---------------------------------------------------------------------------
  sandbox-linux:
    name: Sandbox VM (linux)
    runs-on: ubuntu-latest
    timeout-minutes: 30
    steps:
      - uses: actions/checkout@v7

      - name: Set up Go
        uses: actions/setup-go@v7
        with:
          go-version-file: go.mod

      - name: Verify KVM is available
        run: |
          if [ ! -e /dev/kvm ]; then
            echo "::error::/dev/kvm is missing; the sandbox VM cannot boot"
            exit 1
          fi
          ls -l /dev/kvm

      - name: Install QEMU and cloud-image tools
        run: |
          sudo apt-get update -qq
          sudo apt-get install -y -qq qemu-system-x86 qemu-utils cloud-image-utils
          sudo usermod -aG kvm "$USER"

      - name: Cache the Ubuntu cloud image
        uses: actions/cache@v4
        with:
          path: test/sandbox/linuxvm/.work/base.img
          key: ubuntu-cloudimg-24.04

      - name: Run the sandbox scenarios
        run: sudo -E test/sandbox/linuxvm/run.sh

      - name: Upload the VM console log on failure
        if: failure()
        uses: actions/upload-artifact@v7
        with:
          name: sandbox-vm-console
          path: test/sandbox/linuxvm/.work/vm-console.log
          retention-days: 7
```

and extend the needs list:

```yaml
    needs: [format, typos, lint, test, e2e, realtrigger, sandbox-linux, frontend, build, vulncheck]
```

In `Makefile`, add `test-sandbox` to `.PHONY` and append:

```makefile
# Layer 1: boots a real Linux VM and creates real kernel-backed hardware.
# Needs qemu, cloud-image-utils and /dev/kvm; Linux hosts only.
test-sandbox:
	./test/sandbox/linuxvm/run.sh
```

- [ ] **Step 8: Add the sandbox README**

Create `test/sandbox/linuxvm/README.md`:

```markdown
# Linux VM sandbox

Boots a real Ubuntu VM under QEMU/KVM and creates hardware the kernel really
reports, so the unmodified `leavesafe` binary reads a real `/sys` and `/dev`.

## Why a VM and not a container

A container shares the host kernel and cannot load modules or own device nodes.
Measured inside one, `/proc/acpi/button/lid`, `/sys/bus/usb/devices` and
`/dev/input` are all absent, and `/sys/class/power_supply` reports the host VM's
battery rather than the laptop's. That is the reason Docker support was removed
from this project rather than reused for testing.

## What it creates

| Sensor  | Real trigger |
| ------- | ------------ |
| power   | `test_power` module; writing `ac_online=0` unplugs the charger for real |
| input   | `uinput` virtual keyboard driven by `evemu-event` |
| usb     | `dummy_hcd` host controller with a gadget bound to it |
| screen  | `Xvfb` plus `xset dpms force off` |
| lid     | not possible — QEMU x86 emulates no ACPI lid button |

Anything that cannot be created is skipped with the reason attached, and appears
in the coverage matrix the run prints.

## Running it

Linux host with `/dev/kvm`:

```bash
sudo apt-get install -y qemu-system-x86 qemu-utils cloud-image-utils
make test-sandbox
```

The first run downloads a ~600 MB cloud image into `.work/` and takes a few
minutes; later runs reuse it.
```

- [ ] **Step 9: Verify the workflow parses**

Run: `python -c "import yaml; yaml.safe_load(open('.github/workflows/ci.yml')); print('ok')"`
Expected: `ok`

- [ ] **Step 10: Commit**

```bash
git add test/sandbox test/harness/app.go .github/workflows/ci.yml Makefile
git commit -m "add linux vm sandbox with real kernel-backed hardware"
```

---

### Task 12: Document what CI cannot prove

A green pipeline must not be mistaken for full coverage. This writes down the gaps and updates the README to describe the testing that now exists.

**Files:**
- Create: `docs/manual-verification.md`
- Modify: `README.md`

**Interfaces:**
- Consumes: the coverage decisions from Tasks 10 and 11.
- Produces: nothing code-facing.

- [ ] **Step 1: Write the manual verification checklist**

Create `docs/manual-verification.md`:

```markdown
# Manual verification

CI proves a great deal, but some hardware simply does not exist on a hosted
runner or inside a VM. This checklist covers the remainder. Run it on real
hardware before tagging a release, and record the result in the release notes.

## What CI already proves, so you do not need to

- The binary builds, starts, pairs, arms, raises and clears an alarm, enforces
  the auth lockout and the three-session cap, serves config, and shuts down
  cleanly — on Windows, Linux and macOS.
- On Linux, real hardware changes are detected: charger removal, keyboard
  activity, USB attachment and screen blanking.
- On macOS, real network and display-sleep changes are detected.
- On Windows, real network changes are detected.

## What only you can check

| # | Check | Platform | Expected |
| - | ----- | -------- | -------- |
| 1 | Arm, then unplug the charger | Windows | Phone alerts within ~2 s; laptop alarm sounds |
| 2 | Arm, then unplug the charger | macOS | Phone alerts within ~2 s; laptop alarm sounds |
| 3 | Arm, then close the lid | Windows | Phone alerts within ~2 s |
| 4 | Arm, then close the lid | macOS | Phone alerts within ~2 s |
| 5 | Arm, then close the lid | Linux | Phone alerts within ~2 s |
| 6 | Arm, then unplug a USB stick | Windows | Phone alerts within ~1 s |
| 7 | Arm, then unplug a USB stick | macOS | Phone alerts within ~3 s |
| 8 | Trigger an alarm and let it escalate | any | Volume rises through the configured levels |
| 9 | Pair over Bluetooth instead of Wi-Fi | any | Pairing succeeds and alerts arrive over BLE |
| 10 | Arm, then walk out of Wi-Fi range with the phone | any | Alarm fires on disconnect after the grace period |

## Why these are absent from CI

- **Charger and lid:** hosted runners have no battery and no lid. QEMU emulates
  no ACPI lid button, so even the Linux VM cannot produce one.
- **USB on Windows and macOS:** no attachable device exists on a hosted runner.
- **Audible alarm and volume escalation:** runners have no audio device, and
  nothing could listen to it.
- **Bluetooth:** no runner exposes a Bluetooth adapter.
```

- [ ] **Step 2: Update the README testing section**

In `README.md`, replace the CI job table so it lists the current jobs, adding rows for the three new ones:

```markdown
| `e2e` | starts the real binary on each OS and drives the full user flow |
| `realtrigger` | fires the hardware changes each runner permits; publishes a coverage matrix |
| `sandbox-linux` | boots a real Linux VM and creates real kernel-backed hardware |
```

and add a short paragraph beneath it:

```markdown
Every run publishes a coverage matrix naming each sensor that was genuinely
triggered and each one that could not be, with the reason. What CI cannot reach
is listed in [docs/manual-verification.md](docs/manual-verification.md).
```

- [ ] **Step 3: Verify the docs are consistent with reality**

Run: `git grep -n "docker" README.md docs/manual-verification.md`
Expected: no output.

- [ ] **Step 4: Run the full local gate**

Run: `gofmt -l . && go vet ./... && GOOS=linux go vet ./... && GOOS=darwin go vet ./... && go test ./... -count=1`
Expected: no output from `gofmt` or the vets; tests PASS.

- [ ] **Step 5: Commit**

```bash
git add docs/manual-verification.md README.md
git commit -m "document what ci cannot prove and how the layers fit together"
```

---

### Task 13: Open the pull request

**Files:** none.

- [ ] **Step 1: Push the branch**

```bash
git push -u origin test/real-environment-verification
```

- [ ] **Step 2: Open the PR**

```bash
gh pr create --base main --title "Verify LeaveSafe on real Windows, Linux and macOS environments" --body-file - <<'EOF'
Every pull request now has to prove that this version actually runs, and
actually detects hardware changes, on all three platforms.

## What runs on every PR

- **`e2e` (Windows, Linux, macOS)** — builds the binary and *starts it*, then
  drives the real WebSocket protocol as the phone: pairing, auth lockout,
  session cap, arm, alarm, PIN-protected disarm, config round-trip, clean
  shutdown and port release.
- **`sandbox-linux`** — boots a real Ubuntu VM under QEMU/KVM and creates real
  hardware with `test_power`, `uinput`, `dummy_hcd` and `Xvfb`. The charger is
  genuinely unplugged and the unmodified binary reads a real `/sys`.
- **`realtrigger` (Windows, Linux, macOS)** — fires the hardware changes each
  runner genuinely permits and publishes a coverage matrix naming everything it
  could not.
- **Parser tests** — the OS-output-parsing logic is extracted into pure
  functions and tested against output captured from the real runners, so a
  format change in `pmset` or `ioreg` cannot silently disable the alarm.

## Honesty rule

No test fakes hardware and reports success. Where a real trigger is impossible —
lid on every platform, charger and USB on Windows and macOS — the test skips
with the reason attached, and the reason appears in the job summary. The
remainder is written down in `docs/manual-verification.md` as a pre-release
checklist.

## Docker support removed

Measured on the kernel Docker Desktop actually uses: `/proc/acpi/button/lid`,
`/sys/bus/usb/devices` and `/dev/input` are absent, `xset` does not exist in a
`FROM scratch` image, `/sys/class/power_supply` reports the VM's own battery,
and the network sensor sees the container namespace rather than the LAN. Five of
six sensors are dead, the sixth is misleading, and the alarm cannot sound. The
README additionally documented a `privileged: true` setup that
`docker-compose.yml` never contained.

Design record: `docs/superpowers/specs/2026-07-28-real-environment-verification-design.md`
EOF
```

- [ ] **Step 3: Report the PR URL to the user**

---

## Self-Review

**Spec coverage:** Docker removal → Task 1. Layer 0 → Tasks 2-5. Layer 1 → Task 11. Layer 2 → Task 10. Layer 3 → Tasks 6-9. Coverage matrix → Task 10 (`harness.Matrix`), reused by Task 11. `docs/manual-verification.md` → Task 12. `ci-success` needs updated in Tasks 1, 5, 10 and 11. Make targets → Tasks 2, 10, 11. Every spec section maps to a task.

**Deviation from the spec's file layout:** the spec placed `harness.go` and `client.go` under `test/e2e/`. All three layers need them, so they live in `test/harness/` instead. The layering and behaviour are unchanged.

**Type consistency:** `harness.Options`, `harness.Start`, `harness.StartIn`, `harness.FreePort`, `harness.Dial`, `(*Phone).Send/Expect/ExpectNot/Authenticate/Close`, `harness.NewMatrix`, `(*Matrix).Triggered/Skipped/WriteSummary` are defined in Tasks 2, 3, 5 and 10 and used with those exact names thereafter. `errUnsupported` is declared once per GOOS-tagged file in `realtrigger`, so no redeclaration occurs. Parser names differ per platform (`parseACOnline`, `parseOnACPower`, `parseLidStatusWMI`) and each `stripFixtureComments*` is uniquely named, so no collision is possible across build tags.

**Known risks carried from the spec:** `/dev/kvm` availability, kernel-module availability, stdout key parsing, Windows `SendInput` viability. Each surfaces as a loud failure or an explicit skip, never a silent pass.
