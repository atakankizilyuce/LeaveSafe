// Package harness starts the real leavesafe binary in an isolated environment
// and drives it the way a phone would. It is shared by the e2e, sandbox and
// realtrigger test layers, so it carries no build tag and must compile on
// every supported platform.
package harness

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
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
	// RemoteAccess seeds remote_access, the mode that exposes the port beyond
	// the local network.
	RemoteAccess bool
	// MaxAuthAttempts overrides max_auth_attempts. Zero keeps the default of 5.
	MaxAuthAttempts int
	// LocationEnabled turns on location tracking. Only the phone anchor is
	// seeded as a source, so the suite stays hermetic: no scan, no lookup, no
	// dependency on a third-party service being up.
	LocationEnabled bool
	// LocationGeolocateKey seeds the geolocation API key, so a test can prove
	// it is never handed back to a paired client.
	LocationGeolocateKey string
	// BreakTLSSetup plants an ordinary file where the app must create its TLS
	// directory, so certificate setup fails the way a permission problem or a
	// stray file would. Used to prove that remote access refuses to fall back
	// to cleartext.
	BreakTLSSetup bool
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

// syncBuffer collects process output from the reader goroutines the exec
// package spawns, while tests read it from their own goroutine.
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

// binary returns the leavesafe executable to run, compiling it once per test
// binary. LEAVESAFE_TEST_BINARY overrides the build, so environments without a
// Go toolchain — the sandbox VM — can run the same tests against a binary that
// was cross-compiled on the host.
func binary(t *testing.T) string {
	t.Helper()

	if path := os.Getenv("LEAVESAFE_TEST_BINARY"); path != "" {
		// #nosec G703 -- the path is set by whoever runs the suite, in the same
		// shell that could run the binary directly; it is not attacker input.
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("LEAVESAFE_TEST_BINARY=%s is not usable: %v", path, err)
		}
		return path
	}

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

// Start launches the binary with a fresh isolated home directory and waits
// until it is serving. The process is stopped when the test ends.
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

	if opts.BreakTLSSetup {
		if err := os.WriteFile(filepath.Join(configDir, "tls"), []byte("not a directory"), 0o600); err != nil {
			t.Fatalf("plant blocking tls file: %v", err)
		}
	}

	app := &App{
		t:         t,
		port:      opts.Port,
		homeDir:   home,
		configDir: configDir,
		out:       &syncBuffer{},
	}

	// #nosec G204 -- the path is either our own go build output or an operator-supplied test binary
	cmd := exec.Command(binary(t))
	if os.Getenv("LEAVESAFE_TEST_BINARY") == "" {
		cmd.Dir = repoRoot(t)
	} else {
		cmd.Dir = home
	}
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
	remote := opts.RemoteAccess
	maxAuthAttempts := opts.MaxAuthAttempts
	if maxAuthAttempts == 0 {
		maxAuthAttempts = 5
	}
	cfg := map[string]any{
		"port":                     0,
		"max_sessions":             3,
		"max_auth_attempts":        maxAuthAttempts,
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
		"location": map[string]any{
			"enabled":       opts.LocationEnabled,
			"poll_seconds":  60,
			"phone_anchor":  true,
			"ip_fallback":   false,
			"wifi_enabled":  false,
			"geolocate_key": opts.LocationGeolocateKey,
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

// waitUntilServing blocks until the app answers a request, or the deadline
// passes. A dead process fails fast with its output attached.
//
// Answering is the thing worth waiting for, and it is not the same as the port
// being open. main binds the listener early and only begins serving at the very
// end of startup — after it has drawn the dashboard, rendered a QR code for
// every address and registered the sensors. The kernel completes the TCP
// handshake into the listen backlog throughout that window, so a plain dial
// succeeded while the app was still starting and the harness handed the test a
// server that could not yet reply.
//
// What that cost was a flake with no fingerprints on it: the test would pair,
// spend its whole ten-second auth deadline waiting for a greeting, and fail
// with "timed out waiting for an auth reply" — on a loaded runner, in the first
// test of the suite, while every later test passed in under a second because
// the app was warm by then.
func (a *App) waitUntilServing() {
	a.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		if a.cmd.ProcessState != nil && a.cmd.ProcessState.Exited() {
			a.t.Fatalf("leavesafe exited before serving\n--- output ---\n%s", a.out.String())
		}
		if err := a.probeOnce(); err == nil {
			return
		} else {
			lastErr = err
		}
		time.Sleep(200 * time.Millisecond)
	}
	a.t.Fatalf("leavesafe did not answer on port %d within 30s (last attempt: %v)\n--- output ---\n%s",
		a.port, lastErr, a.out.String())
}

// probeClient is used only for the liveness probe above.
//
// The certificate LeaveSafe serves is self-signed by design, and the harness is
// asking "are you answering yet", not "do I trust you" — the tests that care
// about the certificate check it themselves.
var probeClient = &http.Client{
	Timeout: 2 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 -- liveness probe, not a trust decision
	},
}

// probeOnce asks for the dashboard page and reports whether anything answered.
//
// Both schemes are tried because the harness does not get to assume which one
// is in force: remote access serves HTTPS, but it also falls back to plain HTTP
// when certificate setup fails, and the suite deliberately exercises that
// fallback. Discovering the scheme keeps this from encoding a copy of the app's
// own decision, which could then drift out of step with it.
func (a *App) probeOnce() error {
	var firstErr error
	for _, scheme := range []string{"http", "https"} {
		resp, err := probeClient.Get(fmt.Sprintf("%s://127.0.0.1:%d/", scheme, a.port))
		if err == nil {
			_ = resp.Body.Close()
			return nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
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
