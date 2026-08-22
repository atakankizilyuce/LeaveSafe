# Connection Mode Live Toggle Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ask the connection mode on every interactive start, and let remote access be switched on or off at runtime without a restart and without disturbing the local-network connection.

**Architecture:** The single HTTP listener becomes two — a local plain-HTTP listener that opens once and never closes, and a remote TLS listener that starts and stops on demand. The certificate, UPnP mapping and public-IP discovery that currently sit inline in `main.go` move into a new `internal/remote` package whose `Controller` is driven by three callers: startup, the phone's `update_config`, and a new `mode` console command.

**Tech Stack:** Go 1.25.12, `nhooyr.io/websocket`, `gitlab.com/NebulousLabs/go-upnp`, Preact + TypeScript (web UI), standard `testing` (no assertion library — the repo uses plain `t.Errorf`/`t.Fatalf`).

## Global Constraints

- Branch is `feature/live-connection-mode`, already created from `origin/main` (b111358). Do not merge to main; this ships as its own PR.
- Commit messages: imperative mood, sentence case, no type prefix — match the existing log (`Answer pairing and status without waiting for a probe`, `Stop reporting a failed sensor as one that is watching`). **No AI attribution of any kind** — no `Co-Authored-By`, no "Generated with", no session links.
- No new third-party dependencies.
- `golangci-lint` must pass (`.golangci.yml` is strict — `gosec` is on; keep the existing `#nosec` comment style with a reason).
- Every user-facing string that already exists in Turkish + English (the startup prompt) keeps both languages. New log lines and phone-facing strings are English, matching the rest of the codebase.
- `docs/superpowers/` is gitignored. Never `git add` the spec or this plan.
- The stored `remote_access` value records what the user asked for and is never rewritten by a failure. Whether remote access is actually running is `remote.State.Enabled`.

---

### Task 1: CGNAT detection in `internal/network`

A public address inside `100.64.0.0/10` means the ISP is doing carrier-grade NAT: the address is not the subscriber's, so no port mapping can make the laptop reachable. `publicAddr` deliberately accepts these (there is a test asserting it), because refusing a syntactically valid public address is not its job. Detection is a separate question with a separate answer.

**Files:**
- Modify: `internal/network/publicip.go`
- Test: `internal/network/publicip_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `func IsCarrierGradeNAT(ip string) bool`

- [ ] **Step 1: Write the failing test**

Append to `internal/network/publicip_test.go`:

```go
// 100.64.0.0/10 is shared address space: the ISP hands it to the subscriber and
// keeps the routable address for itself. A port mapping on the home router
// cannot make this machine reachable, so telling the user their router is fine
// would be telling them to keep trying something that cannot work.
func TestCarrierGradeNATIsRecognised(t *testing.T) {
	cgnat := []string{
		"100.64.0.0",
		"100.64.0.1",
		"100.100.50.7",
		"100.127.255.255",
	}
	for _, ip := range cgnat {
		if !IsCarrierGradeNAT(ip) {
			t.Errorf("%s is inside 100.64.0.0/10 but was not recognised as CGNAT", ip)
		}
	}
}

// The boundaries matter: 100.63.x and 100.128.x are ordinary public addresses,
// and calling them CGNAT would tell a user with a working connection that their
// ISP has broken it.
func TestAddressesOutsideSharedSpaceAreNotCGNAT(t *testing.T) {
	notCGNAT := []string{
		"100.63.255.255",
		"100.128.0.0",
		"198.51.100.4",
		"203.0.113.9",
		"2001:db8::1",
		"192.168.1.5",
		"not-an-ip",
		"",
	}
	for _, ip := range notCGNAT {
		if IsCarrierGradeNAT(ip) {
			t.Errorf("%s was wrongly reported as CGNAT", ip)
		}
	}
}
```

- [ ] **Step 2: Run the test and watch it fail**

```
go test ./internal/network/ -run CGNAT -v
```

Expected: compile failure — `undefined: IsCarrierGradeNAT`.

- [ ] **Step 3: Implement it**

Append to `internal/network/publicip.go`:

```go
// cgnatBlock is RFC 6598 shared address space, the range an ISP assigns to a
// subscriber while keeping the routable address for itself.
var cgnatBlock = &net.IPNet{
	IP:   net.IPv4(100, 64, 0, 0),
	Mask: net.CIDRMask(10, 32),
}

// IsCarrierGradeNAT reports whether ip is inside 100.64.0.0/10.
//
// An address in this range is the answer to "what does the internet see" only
// in the sense that the ISP's NAT sees it. Nothing the user can do to their own
// router makes such a machine reachable from outside, so remote access is not
// merely misconfigured here — it cannot work, and saying so is more useful than
// leaving the user to forward ports at a problem that is not theirs.
//
// publicAddr deliberately accepts these addresses; refusing a syntactically
// valid public address is not its job. This is the separate question.
func IsCarrierGradeNAT(ip string) bool {
	parsed := net.ParseIP(strings.TrimSpace(ip))
	if parsed == nil {
		return false
	}
	v4 := parsed.To4()
	if v4 == nil {
		return false
	}
	return cgnatBlock.Contains(v4)
}
```

- [ ] **Step 4: Run the tests and watch them pass**

```
go test ./internal/network/ -v
```

Expected: PASS, including the pre-existing `TestARealPublicAddressIsAccepted` which asserts `publicAddr("100.64.0.1")` still succeeds. If that test broke, the change went into the wrong function.

- [ ] **Step 5: Fix the mislabelled test case next door**

`internal/network/upnp_test.go` labels `172.16.0.9` as `"carrier-grade NAT"`. It is RFC 1918 private space. Now that CGNAT has a precise meaning in this package, leaving the wrong label there will mislead the next reader. Change that map key to `"another private one, higher up the block"` — and since `"another private one"` already exists as a key, rename the `10.4.4.4` entry to `"a private ten-net address"` so both keys stay unique.

- [ ] **Step 6: Commit**

```bash
git add internal/network/publicip.go internal/network/publicip_test.go internal/network/upnp_test.go
git commit -m "Recognise carrier-grade NAT as its own answer"
```

---

### Task 2: A second listener in `internal/server`

**Files:**
- Modify: `internal/server/server.go`
- Test: `internal/server/remote_listener_test.go` (create)

**Interfaces:**
- Consumes: nothing from earlier tasks
- Produces:
  - `func (s *Server) StartRemote(cert tls.Certificate, certFP string, port int) (int, error)` — binds a TLS listener on `port`, serves the same mux, returns the bound port
  - `func (s *Server) StopRemote()` — closes it; safe to call when not running
  - `func (s *Server) RemotePort() int` — 0 when not running
  - `func (s *Server) RemoteCertFP() string` — "" when not running

The existing `Config.TLSCert`/`Config.CertFP` fields and `IsTLS()`/`CertFingerprint()` stay exactly as they are. They now describe the *local* listener only, which for every production path is plain HTTP. Tests and `test/harness` still use them.

- [ ] **Step 1: Write the failing test**

Create `internal/server/remote_listener_test.go`:

```go
package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/leavesafe/leavesafe/internal/auth"
	"github.com/leavesafe/leavesafe/internal/monitor"
	"github.com/leavesafe/leavesafe/internal/ws"
	"nhooyr.io/websocket"
)

// newTestServer returns a started local-only server and a func that stops it.
func newTestServer(t *testing.T) (*Server, func()) {
	t.Helper()
	authMgr, err := auth.NewManagerWithOptions(auth.Options{MaxSessions: 3, MaxAttempts: 5})
	if err != nil {
		t.Fatalf("auth manager: %v", err)
	}
	hub := ws.NewHub(authMgr, monitor.NewManager(), "test")
	srv := New(Config{Hub: hub, Port: 0})
	if err := srv.Listen(); err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = srv.Start() }()
	return srv, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}
}

// testCert generates a throwaway certificate through the same path production
// uses, so the test exercises the real loader rather than a hand-rolled one.
func testCert(t *testing.T) (tls.Certificate, string) {
	t.Helper()
	cert, fp, err := GenerateOrLoadCert(t.TempDir())
	if err != nil {
		t.Fatalf("generate cert: %v", err)
	}
	return cert, fp
}

// This is the central claim of the whole design: turning remote access on and
// off must not touch the local listener. A phone connected over Wi-Fi is what
// flips the switch, and if flipping it kills that phone's own socket then
// "no restart required" has bought the user nothing.
func TestLocalWebSocketSurvivesTheRemoteListenerComingAndGoing(t *testing.T) {
	srv, stop := newTestServer(t)
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, fmt.Sprintf("ws://127.0.0.1:%d/ws", srv.Port()), nil)
	if err != nil {
		t.Fatalf("dial local socket: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// The hub greets every socket. Reading it proves the connection is live
	// before anything else happens to the server.
	if _, _, err := conn.Read(ctx); err != nil {
		t.Fatalf("greeting before remote listener started: %v", err)
	}

	cert, fp := testCert(t)
	remotePort, err := srv.StartRemote(cert, fp, 0)
	if err != nil {
		t.Fatalf("start remote listener: %v", err)
	}
	if remotePort == 0 {
		t.Fatal("StartRemote returned port 0, want the bound port")
	}
	if srv.RemoteCertFP() != fp {
		t.Errorf("RemoteCertFP() = %q, want %q", srv.RemoteCertFP(), fp)
	}

	srv.StopRemote()

	if srv.RemotePort() != 0 {
		t.Errorf("RemotePort() = %d after StopRemote, want 0", srv.RemotePort())
	}

	// The socket opened before any of that must still carry traffic.
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"ping"}`)); err != nil {
		t.Fatalf("local socket died while the remote listener came and went: %v", err)
	}
	if _, _, err := conn.Read(ctx); err != nil {
		t.Fatalf("no answer on the local socket afterwards: %v", err)
	}
}

// The remote listener is the one exposed to the internet. A plain-HTTP request
// to it must not be served — that is the whole reason the design refuses to
// publish a port without a certificate.
func TestRemoteListenerRefusesPlainHTTP(t *testing.T) {
	srv, stop := newTestServer(t)
	defer stop()

	cert, fp := testCert(t)
	port, err := srv.StartRemote(cert, fp, 0)
	if err != nil {
		t.Fatalf("start remote listener: %v", err)
	}
	defer srv.StopRemote()

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/", port)) //nolint:noctx // short-lived test request
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("plain HTTP was served on the remote listener, want a TLS handshake failure")
	}
}

// Both listeners serve the same application. A phone that pairs over the public
// URL has to reach the same hub as one on the LAN, or remote access would be a
// second, empty copy of the app.
func TestBothListenersServeTheSameApplication(t *testing.T) {
	srv, stop := newTestServer(t)
	defer stop()

	cert, fp := testCert(t)
	port, err := srv.StartRemote(cert, fp, 0)
	if err != nil {
		t.Fatalf("start remote listener: %v", err)
	}
	defer srv.StopRemote()

	// The certificate is self-signed, but it is not unknown: this test generated
	// it. Trusting exactly that one and verifying against it is both closer to
	// what the phone is asked to do and a stronger assertion than skipping
	// verification — it proves the listener serves the certificate it was
	// handed, and that the SANs cover the address being dialled.
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("parse generated certificate: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)

	client := &http.Client{
		Timeout: 3 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
		},
	}
	resp, err := client.Get(fmt.Sprintf("https://127.0.0.1:%d/", port)) //nolint:noctx // short-lived test request
	if err != nil {
		t.Fatalf("remote listener did not serve the UI: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("remote listener returned %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Strict-Transport-Security"); got == "" {
		t.Error("remote listener served no HSTS header, but it is the TLS one")
	}
}
```

- [ ] **Step 2: Run the test and watch it fail**

```
go test ./internal/server/ -run Remote -v
```

Expected: compile failure — `srv.StartRemote undefined`.

- [ ] **Step 3: Implement the second listener**

In `internal/server/server.go`, add fields to `Server`:

```go
type Server struct {
	httpServer *http.Server
	listener   net.Listener
	port       int
	hub        *ws.Hub
	tlsCert    *tls.Certificate
	certFP     string
	devMode    bool
	mux        *http.ServeMux

	// The remote listener is a second front door onto the same application:
	// TLS, on its own port, published to the internet. It is separate from the
	// local one so that opening and closing it — which is what the connection
	// mode toggle does — never disturbs a phone already connected over Wi-Fi.
	remoteMu     sync.Mutex
	remoteServer *http.Server
	remoteLn     net.Listener
	remotePort   int
	remoteCertFP string
}
```

In `New`, keep the mux on the struct so the remote server can reuse it:

```go
	mux.HandleFunc("/ws", s.handleWebSocket)
	s.mux = mux
```

Add the lifecycle methods:

```go
// StartRemote opens a TLS listener on port and serves the same application on
// it. A port of 0 asks the OS for a free one. It returns the bound port.
//
// Calling it while a remote listener is already running is a no-op that returns
// the running port, so the config toggle and the startup path can both call it
// without coordinating.
func (s *Server) StartRemote(cert tls.Certificate, certFP string, port int) (int, error) {
	s.remoteMu.Lock()
	defer s.remoteMu.Unlock()

	if s.remoteLn != nil {
		return s.remotePort, nil
	}

	// #nosec G102 -- binding to all interfaces is the point: this listener is
	// the one the phone reaches from another network through the UPnP mapping.
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return 0, fmt.Errorf("listen on the remote port: %w", err)
	}
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		_ = ln.Close()
		return 0, fmt.Errorf("listen on the remote port: unexpected address type %T", ln.Addr())
	}

	tlsLn := tls.NewListener(ln, &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	})

	srv := &http.Server{
		// The handler chain is built separately from the local one because the
		// tls flag differs: HSTS and the socket origin have to describe the
		// listener actually answering, not the other one.
		Handler:           requireAddressHost(securityHeaders(s.mux, true), s.devMode),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
	}

	s.remoteServer = srv
	s.remoteLn = tlsLn
	s.remotePort = addr.Port
	s.remoteCertFP = certFP

	go func() {
		if err := srv.Serve(tlsLn); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Errorf("Remote listener stopped: %v", err)
		}
	}()

	log.Infof("HTTPS server bound to port %d (remote access)", addr.Port)
	return addr.Port, nil
}

// StopRemote closes the remote listener. It is safe to call when none is
// running.
//
// Sockets already open on it are closed with it: a phone connected from
// another network is exactly what turning remote access off is meant to cut.
// The local listener is untouched.
func (s *Server) StopRemote() {
	s.remoteMu.Lock()
	srv, ln := s.remoteServer, s.remoteLn
	s.remoteServer, s.remoteLn = nil, nil
	s.remotePort, s.remoteCertFP = 0, ""
	s.remoteMu.Unlock()

	if srv == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), remoteShutdownGrace)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Warnf("Remote listener did not shut down cleanly: %v", err)
		_ = ln.Close()
	}
	log.Info("Remote listener closed")
}

// remoteShutdownGrace bounds how long a closing remote listener waits for
// in-flight requests. A phone on the far end of a mobile connection is not
// worth blocking the toggle on.
const remoteShutdownGrace = 2 * time.Second

// RemotePort returns the port the remote listener is bound to, or 0 when it is
// not running.
func (s *Server) RemotePort() int {
	s.remoteMu.Lock()
	defer s.remoteMu.Unlock()
	return s.remotePort
}

// RemoteCertFP returns the fingerprint of the certificate the remote listener
// presents, empty when it is not running.
func (s *Server) RemoteCertFP() string {
	s.remoteMu.Lock()
	defer s.remoteMu.Unlock()
	return s.remoteCertFP
}
```

Add `"errors"` to the imports.

- [ ] **Step 4: Run the tests and watch them pass**

```
go test ./internal/server/ -v
```

Expected: PASS, including the pre-existing `TestWebSocketRejectsReboundHost` and `TestWebSocketOutlivesTheServerWriteTimeout`.

- [ ] **Step 5: Run the linter**

```
golangci-lint run ./internal/server/...
```

Expected: no findings. If `gosec` objects to the `net.Listen`, confirm the `#nosec G102` comment is on the line directly above it with its reason.

- [ ] **Step 6: Commit**

```bash
git add internal/server/server.go internal/server/remote_listener_test.go
git commit -m "Give remote access its own listener instead of moving the only one"
```

---

### Task 3: The `internal/remote` controller

**Files:**
- Create: `internal/remote/remote.go`
- Create: `internal/remote/remote_test.go`

**Interfaces:**
- Consumes: `network.IsCarrierGradeNAT` (Task 1); `server.StartRemote`/`StopRemote` (Task 2)
- Produces:
  - `type UPnPState string` with `UPnPOK`, `UPnPFailed`, `UPnPCarrierNAT`
  - `type State struct { Enabled bool; PublicURL, CertFP, Reason string; UPnP UPnPState; ManualPort int }`
  - `type Deps struct { Cert func(string) (tls.Certificate, string, error); OpenPort func(int) (PortMapping, error); PublicIP func() (string, error) }`
  - `func NewController(srv Listener, configDir string, port int, deps Deps) *Controller`
  - `func (c *Controller) Enable(ctx context.Context) State`
  - `func (c *Controller) Disable() State`
  - `func (c *Controller) State() State`
  - `type Listener interface { StartRemote(tls.Certificate, string, int) (int, error); StopRemote() }`
  - `type PortMapping interface { ExternalIP() (string, error); Close() error; KeepAlive(context.Context) }`

The two interfaces exist so the tests can drive every failure branch without a router or a network. `*server.Server` and `*network.PortMapping` satisfy them as they are.

- [ ] **Step 1: Write the failing test**

Create `internal/remote/remote_test.go`:

```go
package remote

import (
	"context"
	"crypto/tls"
	"errors"
	"testing"

	"github.com/leavesafe/leavesafe/internal/server"
)

type fakeListener struct {
	started int
	stopped int
	port    int
	err     error
}

func (f *fakeListener) StartRemote(_ tls.Certificate, _ string, port int) (int, error) {
	if f.err != nil {
		return 0, f.err
	}
	f.started++
	if port == 0 {
		port = 9443
	}
	f.port = port
	return port, nil
}

func (f *fakeListener) StopRemote() { f.stopped++; f.port = 0 }

type fakeMapping struct {
	externalIP string
	ipErr      error
	closed     int
}

func (m *fakeMapping) ExternalIP() (string, error)  { return m.externalIP, m.ipErr }
func (m *fakeMapping) Close() error                 { m.closed++; return nil }
func (m *fakeMapping) KeepAlive(_ context.Context)  {}

// workingDeps is the everything-succeeds case; each test bends one part of it.
func workingDeps(t *testing.T, mapping *fakeMapping, publicIP string) Deps {
	t.Helper()
	return Deps{
		Cert: func(dir string) (tls.Certificate, string, error) {
			return server.GenerateOrLoadCert(dir)
		},
		OpenPort:  func(int) (PortMapping, error) { return mapping, nil },
		PublicIP:  func() (string, error) { return publicIP, nil },
	}
}

func TestEnableReportsThePublicURL(t *testing.T) {
	ln := &fakeListener{}
	c := NewController(ln, t.TempDir(), 9443, workingDeps(t, &fakeMapping{}, "198.51.100.4"))

	got := c.Enable(context.Background())

	if !got.Enabled {
		t.Fatalf("Enabled = false, reason %q", got.Reason)
	}
	if got.PublicURL != "https://198.51.100.4:9443" {
		t.Errorf("PublicURL = %q, want https://198.51.100.4:9443", got.PublicURL)
	}
	if got.UPnP != UPnPOK {
		t.Errorf("UPnP = %q, want %q", got.UPnP, UPnPOK)
	}
	if got.CertFP == "" {
		t.Error("CertFP is empty, but the listener is serving a certificate")
	}
	if ln.started != 1 {
		t.Errorf("listener started %d times, want 1", ln.started)
	}
}

// Enable is called from three places — startup, the phone, the console — and
// none of them coordinates with the others.
func TestEnableTwiceStartsOneListener(t *testing.T) {
	ln := &fakeListener{}
	c := NewController(ln, t.TempDir(), 9443, workingDeps(t, &fakeMapping{}, "198.51.100.4"))

	c.Enable(context.Background())
	c.Enable(context.Background())

	if ln.started != 1 {
		t.Errorf("listener started %d times, want 1", ln.started)
	}
}

func TestDisableTwiceStopsOnce(t *testing.T) {
	ln := &fakeListener{}
	mapping := &fakeMapping{}
	c := NewController(ln, t.TempDir(), 9443, workingDeps(t, mapping, "198.51.100.4"))

	c.Enable(context.Background())
	c.Disable()
	c.Disable()

	if ln.stopped != 1 {
		t.Errorf("listener stopped %d times, want 1", ln.stopped)
	}
	if mapping.closed != 1 {
		t.Errorf("port mapping closed %d times, want 1", mapping.closed)
	}
	if c.State().Enabled {
		t.Error("still reporting enabled after Disable")
	}
}

// Publishing a port to the internet without a certificate would put the pairing
// key on the wire in cleartext. The listener must not come up at all.
func TestACertificateFailureLeavesRemoteAccessOff(t *testing.T) {
	ln := &fakeListener{}
	deps := workingDeps(t, &fakeMapping{}, "198.51.100.4")
	deps.Cert = func(string) (tls.Certificate, string, error) {
		return tls.Certificate{}, "", errors.New("disk is full")
	}
	c := NewController(ln, t.TempDir(), 9443, deps)

	got := c.Enable(context.Background())

	if got.Enabled {
		t.Fatal("remote access came up without a certificate")
	}
	if ln.started != 0 {
		t.Errorf("listener started %d times, want 0", ln.started)
	}
	if got.Reason == "" {
		t.Error("no reason given for refusing to start")
	}
}

// UPnP being off is the common case, not a fatal one: the user can forward the
// port by hand, and the listener has to be up for that to be worth doing.
func TestUPnPFailureKeepsTheListenerUpAndNamesThePort(t *testing.T) {
	ln := &fakeListener{}
	deps := workingDeps(t, &fakeMapping{}, "198.51.100.4")
	deps.OpenPort = func(int) (PortMapping, error) { return nil, errors.New("no IGD found") }
	c := NewController(ln, t.TempDir(), 9443, deps)

	got := c.Enable(context.Background())

	if !got.Enabled {
		t.Fatalf("listener was taken down over a UPnP failure, reason %q", got.Reason)
	}
	if got.UPnP != UPnPFailed {
		t.Errorf("UPnP = %q, want %q", got.UPnP, UPnPFailed)
	}
	if got.ManualPort != 9443 {
		t.Errorf("ManualPort = %d, want 9443", got.ManualPort)
	}
}

// Behind CGNAT there is nothing to forward and nothing to wait for. Leaving the
// listener up would be telling the user to keep trying.
func TestCarrierNATStopsTheListenerAndSaysWhy(t *testing.T) {
	ln := &fakeListener{}
	c := NewController(ln, t.TempDir(), 9443, workingDeps(t, &fakeMapping{}, "100.100.50.7"))

	got := c.Enable(context.Background())

	if got.Enabled {
		t.Fatal("remote access stayed on behind CGNAT")
	}
	if got.UPnP != UPnPCarrierNAT {
		t.Errorf("UPnP = %q, want %q", got.UPnP, UPnPCarrierNAT)
	}
	if ln.stopped != 1 {
		t.Errorf("listener stopped %d times, want 1", ln.stopped)
	}
	if got.Reason == "" {
		t.Error("no reason given for the CGNAT refusal")
	}
}

// No public address is not a failure of the listener: someone on a network with
// no outbound STUN can still reach it by an address they know.
func TestAnUnknownPublicAddressLeavesTheListenerUp(t *testing.T) {
	ln := &fakeListener{}
	deps := workingDeps(t, &fakeMapping{ipErr: errors.New("no answer")}, "")
	deps.PublicIP = func() (string, error) { return "", errors.New("STUN timed out") }
	c := NewController(ln, t.TempDir(), 9443, deps)

	got := c.Enable(context.Background())

	if !got.Enabled {
		t.Fatalf("listener came down over an unknown address, reason %q", got.Reason)
	}
	if got.PublicURL != "" {
		t.Errorf("PublicURL = %q, want empty", got.PublicURL)
	}
	if got.Reason == "" {
		t.Error("no reason given for the missing URL")
	}
}
```

- [ ] **Step 2: Run the test and watch it fail**

```
go test ./internal/remote/ -v
```

Expected: the package does not exist yet.

- [ ] **Step 3: Implement the controller**

Create `internal/remote/remote.go`:

```go
// Package remote owns the lifecycle of LeaveSafe's internet-facing listener:
// the certificate it needs, the port mapping that makes it reachable, and the
// public address it is reachable at.
//
// It exists as a package because three callers drive it — the startup path, the
// phone's settings screen and the console's `mode` command — and the sequence
// has enough failure branches that having three copies of it would mean three
// slightly different answers to "did that work?".
package remote

import (
	"context"
	"crypto/tls"
	"fmt"
	"sync"

	log "github.com/sirupsen/logrus"

	"github.com/leavesafe/leavesafe/internal/network"
	"github.com/leavesafe/leavesafe/internal/safe"
)

// UPnPState says how the port mapping went, which is the difference between
// "this works", "forward a port yourself" and "this cannot work here".
type UPnPState string

const (
	UPnPUnknown    UPnPState = ""
	UPnPOK         UPnPState = "ok"
	UPnPFailed     UPnPState = "failed"
	UPnPCarrierNAT UPnPState = "cgnat"
)

// State is what remote access is actually doing, as opposed to what the config
// says the user asked for. The two are kept apart deliberately: a failure has
// to be reportable without being mistaken for the user changing their mind.
type State struct {
	Enabled    bool      `json:"enabled"`
	PublicURL  string    `json:"public_url,omitempty"`
	CertFP     string    `json:"cert_fp,omitempty"`
	UPnP       UPnPState `json:"upnp,omitempty"`
	ManualPort int       `json:"manual_port,omitempty"`
	Reason     string    `json:"reason,omitempty"`
}

// Listener is the part of *server.Server this package drives.
type Listener interface {
	StartRemote(cert tls.Certificate, certFP string, port int) (int, error)
	StopRemote()
}

// PortMapping is the part of *network.PortMapping this package drives.
type PortMapping interface {
	ExternalIP() (string, error)
	Close() error
	KeepAlive(ctx context.Context)
}

// Deps are the outside-world calls, injected so the failure branches are
// testable without a router, a certificate authority or a network.
type Deps struct {
	Cert     func(configDir string) (tls.Certificate, string, error)
	OpenPort func(port int) (PortMapping, error)
	PublicIP func() (string, error)
}

// Controller starts and stops remote access.
type Controller struct {
	mu        sync.Mutex
	srv       Listener
	configDir string
	port      int
	deps      Deps

	state      State
	mapping    PortMapping
	keepCancel context.CancelFunc
}

func NewController(srv Listener, configDir string, port int, deps Deps) *Controller {
	return &Controller{srv: srv, configDir: configDir, port: port, deps: deps}
}

// State returns what remote access is currently doing.
func (c *Controller) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

// Enable brings remote access up and returns what it managed to achieve.
//
// It does not return an error: every failure here is one the user has to be
// told about in words rather than one a caller can retry, so the reason travels
// in State.Reason to the phone and the dashboard alike.
func (c *Controller) Enable(ctx context.Context) State {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.state.Enabled {
		return c.state
	}

	cert, fp, err := c.deps.Cert(c.configDir)
	if err != nil {
		// Serving an internet-facing port over plain HTTP would put the pairing
		// key — the only thing guarding the alarm — in cleartext on the wire.
		// Staying on the LAN is the safe failure.
		log.Errorf("TLS certificate error: %v", err)
		c.state = State{Reason: fmt.Sprintf("Could not create the TLS certificate: %v. "+
			"Remote access will not run without one; the local network is unaffected.", err)}
		return c.state
	}

	boundPort, err := c.srv.StartRemote(cert, fp, c.port)
	if err != nil {
		log.Errorf("Remote listener error: %v", err)
		c.state = State{Reason: fmt.Sprintf("Could not open port %d: %v", c.port, err)}
		return c.state
	}

	next := State{Enabled: true, CertFP: fp, UPnP: UPnPOK}

	mapping, err := c.deps.OpenPort(boundPort)
	if err != nil {
		log.Warnf("UPnP failed: %v — manual port forwarding required (port %d)", err, boundPort)
		next.UPnP = UPnPFailed
		next.ManualPort = boundPort
		next.Reason = fmt.Sprintf("Your router did not accept an automatic port mapping. "+
			"Forward TCP port %d to this machine in the router's admin page.", boundPort)
	} else {
		c.mapping = mapping
		keepCtx, cancel := context.WithCancel(ctx)
		c.keepCancel = cancel
		safe.Go("upnp-keepalive", func() { mapping.KeepAlive(keepCtx) })
	}

	// Asked of the internet first, and of the router only if that fails. The
	// router's answer arrives over unauthenticated SSDP from whatever replied
	// fastest on the local network, and this address goes into the QR code with
	// the pairing key beside it.
	publicIP, err := c.deps.PublicIP()
	if err != nil && c.mapping != nil {
		if ip, mapErr := c.mapping.ExternalIP(); mapErr == nil {
			log.Infof("Using the address the router reports, %s, as the public one", ip)
			publicIP = ip
		}
	}

	switch {
	case publicIP == "":
		next.Reason = "No public address could be found, so there is no URL to scan from " +
			"another network. The local network is unaffected."
	case network.IsCarrierGradeNAT(publicIP):
		// Nothing the user can do to their own router changes this, so leaving
		// the listener up would be inviting them to keep trying.
		c.teardownLocked()
		c.state = State{
			UPnP: UPnPCarrierNAT,
			Reason: fmt.Sprintf("Your ISP puts this connection behind carrier-grade NAT (%s). "+
				"Nothing on this machine or your router can make it reachable from the internet, "+
				"so remote access has been stopped. The local network is unaffected.", publicIP),
		}
		return c.state
	default:
		next.PublicURL = fmt.Sprintf("https://%s:%d", publicIP, boundPort)
	}

	c.state = next
	return c.state
}

// Disable takes remote access down. Safe to call when it is already down.
func (c *Controller) Disable() State {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.state.Enabled && c.mapping == nil {
		c.state = State{}
		return c.state
	}
	c.teardownLocked()
	c.state = State{}
	return c.state
}

// teardownLocked closes the listener and the port mapping. The caller holds mu.
func (c *Controller) teardownLocked() {
	if c.keepCancel != nil {
		c.keepCancel()
		c.keepCancel = nil
	}
	if c.mapping != nil {
		_ = c.mapping.Close()
		c.mapping = nil
	}
	c.srv.StopRemote()
}
```

- [ ] **Step 4: Run the tests and watch them pass**

```
go test ./internal/remote/ -v
```

Expected: PASS, all seven tests.

- [ ] **Step 5: Run the linter**

```
golangci-lint run ./internal/remote/...
```

Expected: no findings.

- [ ] **Step 6: Commit**

```bash
git add internal/remote/
git commit -m "Give remote access a lifecycle of its own"
```

---

### Task 4: Ask on every interactive start

**Files:**
- Modify: `cmd/leavesafe/main.go:379-390` (the `RemoteAccess == nil` block) and `cmd/leavesafe/main.go:1063-1090` (`promptRemoteAccess`)
- Test: `cmd/leavesafe/prompt_test.go` (create)

**Interfaces:**
- Consumes: nothing
- Produces: `func askConnectionMode(in io.Reader, out io.Writer, current bool) bool` — the pure decision, so it is testable without stdin

- [ ] **Step 1: Write the failing test**

Create `cmd/leavesafe/prompt_test.go`:

```go
package main

import (
	"io"
	"strings"
	"testing"
)

// The question is asked on every start now, so a user who has already chosen
// must be able to keep their choice by pressing Enter. Anything else turns a
// convenience into a chore, and — worse — a habit of pressing Enter would
// silently reset a setting the phone had turned on.
func TestEnterKeepsTheSavedChoice(t *testing.T) {
	cases := map[string]struct {
		typed   string
		current bool
		want    bool
	}{
		"enter keeps remote on":       {"\n", true, true},
		"enter keeps wifi only":       {"\n", false, false},
		"blank line keeps remote on":  {"   \n", true, true},
		"eof keeps the saved choice":  {"", true, true},
		"1 selects wifi":              {"1\n", true, false},
		"2 selects remote":            {"2\n", false, true},
		"nonsense keeps saved choice": {"banana\n", true, true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := askConnectionMode(strings.NewReader(tc.typed), io.Discard, tc.current)
			if got != tc.want {
				t.Errorf("askConnectionMode(%q, current=%v) = %v, want %v",
					tc.typed, tc.current, got, tc.want)
			}
		})
	}
}

// The saved choice has to be visible in the prompt, or "press Enter to keep it"
// is asking the user to remember what they picked last time.
func TestThePromptShowsWhichModeIsCurrent(t *testing.T) {
	var out strings.Builder
	askConnectionMode(strings.NewReader("\n"), &out, true)

	printed := out.String()
	if !strings.Contains(printed, "[2]:") {
		t.Errorf("prompt does not show 2 as the default:\n%s", printed)
	}
	if !strings.Contains(printed, "Mobil veri") {
		t.Errorf("prompt lost its Turkish text:\n%s", printed)
	}
	if !strings.Contains(printed, "Remote Access") {
		t.Errorf("prompt lost its English text:\n%s", printed)
	}
}
```

- [ ] **Step 2: Run the test and watch it fail**

```
go test ./cmd/leavesafe/ -run ConnectionMode -v
go test ./cmd/leavesafe/ -run ThePromptShows -v
```

Expected: compile failure — `undefined: askConnectionMode`.

- [ ] **Step 3: Rewrite the prompt**

Replace `promptRemoteAccess` in `cmd/leavesafe/main.go` with:

```go
// askConnectionMode prints the connection-mode question and returns the chosen
// setting. current is what the config holds, and it is the answer to a bare
// Enter.
//
// It takes its reader and writer rather than reaching for os.Stdin so the
// decision can be tested without a terminal — which matters more than usual
// here, because getting the default wrong would silently switch off a setting
// the user had turned on from their phone.
func askConnectionMode(in io.Reader, out io.Writer, current bool) bool {
	def := "1"
	if current {
		def = "2"
	}

	fmt.Fprintf(out, "\n  %s%sBağlantı modu seçin / Select connection mode:%s\n\n", cBold, cCyan, cReset)
	fmt.Fprintf(out, "  %s[1]%s WiFi (aynı ağ / same network)%s\n", cBold, cReset, currentMark(!current))
	fmt.Fprintf(out, "      %sYalnızca aynı WiFi ağından bağlantı%s\n\n", cDim, cReset)
	fmt.Fprintf(out, "  %s[2]%s Uzaktan Erişim / Remote Access%s\n", cBold, cReset, currentMark(current))
	fmt.Fprintf(out, "      %sMobil veri veya farklı ağdan bağlantı (UPnP gerekir)%s\n\n", cDim, cReset)
	fmt.Fprintf(out, "  %sEnter = mevcut ayarı koru / keep the current setting%s\n", cDim, cReset)
	fmt.Fprintf(out, "  Seçiminiz / Your choice (1/2) [%s]: ", def)

	scanner := bufio.NewScanner(in)
	if !scanner.Scan() {
		// No answer at all — a closed or redirected stdin. The saved choice
		// stands; it is the one thing here that is certainly not a guess.
		fmt.Fprintln(out)
		return current
	}
	switch strings.TrimSpace(scanner.Text()) {
	case "1":
		return false
	case "2":
		return true
	default:
		return current
	}
}

// currentMark labels the option the config currently holds.
func currentMark(isCurrent bool) string {
	if !isCurrent {
		return ""
	}
	return fmt.Sprintf("  %s← şu an aktif / current%s", cGreen, cReset)
}

// promptRemoteAccess asks the connection-mode question and saves the answer.
//
// This runs on every interactive start rather than only the first, because the
// first-run-only version was answered by accident: the phone's settings screen
// sends remote_access on every save, so saving any unrelated setting turned the
// unset value into a definite false and the question never came back.
func promptRemoteAccess(cfg *config.Config) {
	current := cfg.RemoteAccess != nil && *cfg.RemoteAccess
	remote := askConnectionMode(os.Stdin, os.Stdout, current)
	cfg.RemoteAccess = &remote

	if err := config.Save(cfg); err != nil {
		log.Warnf("Failed to save config: %v", err)
	}

	if remote {
		fmt.Printf("\n  %s✓ Uzaktan erişim aktif — Remote access enabled%s\n\n", cGreen, cReset)
	} else {
		fmt.Printf("\n  %s✓ WiFi modu seçildi — WiFi mode selected%s\n\n", cGreen, cReset)
	}
}
```

Add `"io"` to the imports if it is not already there (it is — `statusBar.out` is an `io.Writer`).

- [ ] **Step 4: Make it run on every start**

Replace the block at `cmd/leavesafe/main.go:379-390`:

```go
	// The connection mode is asked on every interactive start, with the saved
	// value as the default, so a user who has changed it from their phone sees
	// what is in force and can change it back without hunting for the setting.
	//
	// A headless start has nobody to answer and blocking on stdin there would
	// hang the service forever, so it takes what is stored. Every autostart
	// entry this program writes passes -headless, so no unattended start can
	// reach the prompt.
	if *headless {
		if cfg.RemoteAccess == nil {
			local := false
			cfg.RemoteAccess = &local
			if err := config.Save(cfg); err != nil {
				log.Warnf("Failed to save config: %v", err)
			}
		}
		log.Infof("Connection mode: %s (from config, no terminal to ask)",
			connectionModeName(*cfg.RemoteAccess))
	} else {
		promptRemoteAccess(cfg)
	}
```

And add the helper next to `promptRemoteAccess`:

```go
// connectionModeName names a connection mode for a log line.
func connectionModeName(remote bool) string {
	if remote {
		return "remote access"
	}
	return "local network only"
}
```

- [ ] **Step 5: Run the tests and watch them pass**

```
go test ./cmd/leavesafe/ -v
```

Expected: PASS, including the pre-existing `pairing_url_test.go` and the service tests.

- [ ] **Step 6: Commit**

```bash
git add cmd/leavesafe/main.go cmd/leavesafe/prompt_test.go
git commit -m "Ask the connection mode on every start, not only the first"
```

---

### Task 5: Wire the controller into startup, and let the dashboard change its URLs

**Files:**
- Modify: `cmd/leavesafe/main.go:488-556` (the inline remote block), `:750-760` (`reachableURLs`), `:660-690` (the `upnp-keepalive` supervision and `cleanup`)
- Modify: `cmd/leavesafe/main.go:62-95` (`statusBar`) — add `setURLs`
- Test: `cmd/leavesafe/dashboard_urls_test.go` (create)

**Interfaces:**
- Consumes: `remote.NewController`, `remote.State` (Task 3)
- Produces:
  - `func (sb *statusBar) setURLs(urls []string)` — replaces the URL list and re-renders the QR block
  - a package-level `remoteCtl *remote.Controller` handed to the hub and the console

- [ ] **Step 1: Write the failing test**

Create `cmd/leavesafe/dashboard_urls_test.go`:

```go
package main

import "testing"

// Turning remote access on adds a public URL, and turning it off takes it away.
// The QR codes are built from the URL list, so a list that does not change is a
// QR code that still points at an address nothing is listening on.
func TestSetURLsReplacesTheListAndItsQRCodes(t *testing.T) {
	sb := newHeadlessStatusBar(nil, nil, "1234567890123456",
		[]string{"http://192.168.1.10:59353"}, "", "")

	sb.setURLs([]string{
		"https://198.51.100.4:9443",
		"http://192.168.1.10:59353",
	})

	got := sb.urlList()
	if len(got) != 2 {
		t.Fatalf("urlList() has %d entries, want 2: %v", len(got), got)
	}
	if got[0] != "https://198.51.100.4:9443" {
		t.Errorf("public URL is not first: %v", got)
	}

	sb.setURLs([]string{"http://192.168.1.10:59353"})

	if got := sb.urlList(); len(got) != 1 {
		t.Errorf("urlList() has %d entries after the public URL went away, want 1: %v", len(got), got)
	}
}
```

- [ ] **Step 2: Run the test and watch it fail**

```
go test ./cmd/leavesafe/ -run SetURLs -v
```

Expected: compile failure — `sb.setURLs undefined`, `sb.urlList undefined`.

- [ ] **Step 3: Add the accessors to `statusBar`**

In `cmd/leavesafe/main.go`, next to the other `statusBar` methods:

```go
// setURLs replaces the addresses the dashboard offers and rebuilds their QR
// codes.
//
// The list is not fixed for the life of the process any more: turning remote
// access on adds a public URL and turning it off removes one. A stale QR code
// is worse than none — it is an address the user will scan and wait at.
func (sb *statusBar) setURLs(urls []string) {
	codes := make([][]string, 0, len(urls))
	for _, u := range urls {
		lines, err := qr.Lines(pairingURL(u, sb.key, sb.certFP))
		if err != nil {
			log.Warnf("Could not render QR code for %s: %v", u, err)
			lines = nil
		}
		codes = append(codes, lines)
	}

	sb.mu.Lock()
	sb.urls = urls
	sb.qrCodes = codes
	if sb.qrURLIdx >= len(urls) {
		sb.qrURLIdx = 0
	}
	sb.mu.Unlock()

	sb.refresh()
}

// urlList returns the addresses currently on offer.
func (sb *statusBar) urlList() []string {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return append([]string(nil), sb.urls...)
}
```

Note: `newHeadlessStatusBar` does not set `key` from the raw pairing key — check the call site. It is passed `authMgr.PairingKey()`, and `buildDashboard` uses `authMgr.RawPairingKey()` for the QR. Use `sb.key` here and change `newHeadlessStatusBar`'s caller in Step 5 to pass the raw key if the rendered QR is wrong; the headless bar renders nothing, so this only matters for the interactive path where `buildDashboard` already holds the raw key. To keep one source of truth, add a `rawKey` field to `statusBar`, set it in both constructors, and use it in `setURLs`.

- [ ] **Step 4: Run the test and watch it pass**

```
go test ./cmd/leavesafe/ -run SetURLs -v
```

Expected: PASS.

- [ ] **Step 5: Replace the inline remote block with the controller**

In `cmd/leavesafe/main.go`, delete the `if remoteEnabled { ... }` certificate block at `:494-518`, the `network.OpenPort`/`GetPublicIP` block at `:527-555`, the `if portMapping != nil { safe.Supervise(...) }` line, and the `portMapping.Close()` in `cleanup`. The local server is now always plain HTTP:

```go
	srvCfg := server.Config{Hub: hub, Port: port, DevMode: *devMode}
	srv := server.New(srvCfg)
	if err := srv.Listen(); err != nil {
		log.Fatalf("Failed to bind port: %v", err)
	}

	// Remote access is a second listener rather than a different mode for the
	// only one. The local listener above is opened once and never closed, which
	// is what lets the phone that turns remote access on stay connected while
	// it happens.
	remoteCtl := remote.NewController(srv, config.ConfigDir(), cfg.RemotePort, remote.Deps{
		Cert: server.GenerateOrLoadCert,
		OpenPort: func(p int) (remote.PortMapping, error) {
			return network.OpenPort(p)
		},
		PublicIP: network.GetPublicIP,
	})
```

`ctx` is created further down; move its creation above this block so `remoteCtl.Enable(ctx)` can use it, then:

```go
	if remoteEnabled {
		if st := remoteCtl.Enable(ctx); st.Reason != "" {
			log.Warn(st.Reason)
		}
	}
```

- [ ] **Step 6: Make the URL list follow the controller**

Replace `reachableURLs`:

```go
// reachableURLs returns every address a phone can pair at, public one first.
//
// The dashboard renders the first of these as a QR code. With remote access on
// the public URL only works from outside the network — scanning it from a phone
// on the same Wi-Fi needs NAT hairpinning, which plenty of routers do not do —
// so `qr <n>` switches to a local one.
func reachableURLs(srv *server.Server, st remote.State) []string {
	urls := srv.URLs()
	if st.PublicURL == "" {
		return urls
	}
	return append([]string{st.PublicURL}, urls...)
}
```

Update both call sites (`newHeadlessStatusBar` and `buildDashboard`) to pass `remoteCtl.State()` instead of `publicIP`, and `buildDashboard`'s `certFP` parameter to `remoteCtl.State().CertFP`.

Add a single place the three callers use to push a new state onto the dashboard and the phones:

```go
// applyRemoteState is what every path that changes the connection mode ends
// with: the addresses on the dashboard, the status the phones hold, and the
// reason if there is one, all updated from the same State.
func applyRemoteState(sb *statusBar, hub *ws.Hub, srv *server.Server, st remote.State) {
	sb.setURLs(reachableURLs(srv, st))
	sb.setRemoteStatus(remoteStatusLine(st))
	hub.SetCertFingerprint(st.CertFP)
	hub.SetRemoteState(st)
	if st.Reason != "" {
		sb.writeLine("  %s[NET]%s %s", cYellow, cReset, st.Reason)
	}
}

// remoteStatusLine is the dashboard's one-line summary of remote access.
func remoteStatusLine(st remote.State) string {
	switch {
	case !st.Enabled && st.UPnP == remote.UPnPCarrierNAT:
		return "OFF — carrier-grade NAT"
	case !st.Enabled:
		return ""
	case st.UPnP == remote.UPnPFailed:
		return fmt.Sprintf("ACTIVE — forward TCP %d by hand", st.ManualPort)
	case st.PublicURL == "":
		return "ACTIVE — public address unknown"
	default:
		return "ACTIVE — " + strings.TrimPrefix(st.PublicURL, "https://")
	}
}
```

Add `setRemoteStatus` beside `setURLs`:

```go
// setRemoteStatus replaces the dashboard's remote-access line.
func (sb *statusBar) setRemoteStatus(status string) {
	sb.mu.Lock()
	sb.remoteStatus = status
	sb.mu.Unlock()
	sb.refresh()
}
```

Call `applyRemoteState(sb, hub, srv, remoteCtl.State())` once after the dashboard is built.

- [ ] **Step 7: Close remote access on shutdown**

In `cleanup`, replace the `portMapping` block with:

```go
		remoteCtl.Disable()
```

- [ ] **Step 8: Build and run everything**

```
go build ./...
go test ./... 
```

Expected: PASS. `internal/ws` will not compile until Task 6 adds `SetRemoteState` — if you are running tasks in order, add a stub `func (h *Hub) SetRemoteState(remote.State) {}` in Task 6 first, or land Task 6 before this step.

- [ ] **Step 9: Commit**

```bash
git add cmd/leavesafe/main.go cmd/leavesafe/dashboard_urls_test.go
git commit -m "Drive remote access from the controller instead of inline at startup"
```

---

### Task 6: Apply the change live when the phone asks

**Files:**
- Modify: `internal/ws/hub.go:1199-1210` (the `needsRestart` computation), `:1245-1250`, `:1336-1338` (the restart alert), `:1424-1460` (`configToPayload`)
- Modify: `internal/ws/messages.go:145-166` (`ConfigPayload`)
- Test: `internal/ws/remote_toggle_test.go` (create)

**Interfaces:**
- Consumes: `remote.State` (Task 3)
- Produces:
  - `func (h *Hub) SetRemoteState(st remote.State)` — records what to report to phones
  - `func (h *Hub) SetRemoteToggle(fn func(enable bool))` — the callback `main.go` installs to actually apply a change
  - `ConfigPayload.RemoteState *remote.State` with json tag `remote_state,omitempty`

- [ ] **Step 1: Write the failing test**

Create `internal/ws/remote_toggle_test.go`:

```go
package ws

import (
	"testing"

	"github.com/leavesafe/leavesafe/internal/config"
	"github.com/leavesafe/leavesafe/internal/remote"
)

// Turning remote access on from the phone has to actually turn it on, not
// record a wish and tell the user to restart.
func TestChangingRemoteAccessCallsTheToggle(t *testing.T) {
	h := newTestHub(t)
	var got []bool
	h.SetRemoteToggle(func(enable bool) { got = append(got, enable) })

	off := false
	cfg := &config.Config{RemoteAccess: &off, RemotePort: 9443}
	h.SetConfig(cfg)

	h.applyRemoteAccessChange(true)

	if len(got) != 1 || !got[0] {
		t.Errorf("toggle calls = %v, want [true]", got)
	}
}

// Saving the settings screen without touching the connection mode must not
// bounce the listener — every save sends the whole config back.
func TestAnUnchangedRemoteAccessDoesNotTouchTheListener(t *testing.T) {
	h := newTestHub(t)
	var calls int
	h.SetRemoteToggle(func(bool) { calls++ })

	on := true
	cfg := &config.Config{RemoteAccess: &on, RemotePort: 9443}
	h.SetConfig(cfg)

	h.applyRemoteAccessChange(true)

	if calls != 0 {
		t.Errorf("toggle called %d times for an unchanged setting, want 0", calls)
	}
}

// The phone has to be able to see whether remote access is actually working,
// which is the whole reason State exists separately from the config value.
func TestTheStateReachesTheConfigPayload(t *testing.T) {
	h := newTestHub(t)
	h.SetRemoteState(remote.State{
		Enabled:    true,
		UPnP:       remote.UPnPFailed,
		ManualPort: 9443,
		Reason:     "Your router did not accept an automatic port mapping.",
	})

	on := true
	payload := h.configPayloadWithRemoteState(&config.Config{RemoteAccess: &on, RemotePort: 9443})

	if payload.RemoteState == nil {
		t.Fatal("RemoteState is nil, so the phone cannot tell working from broken")
	}
	if payload.RemoteState.ManualPort != 9443 {
		t.Errorf("ManualPort = %d, want 9443", payload.RemoteState.ManualPort)
	}
}
```

`newTestHub` already exists in `internal/ws` tests — check `hub_test.go` for its exact name and signature and use that; if there is no such helper, add one that mirrors how the existing tests build a hub.

- [ ] **Step 2: Run the test and watch it fail**

```
go test ./internal/ws/ -run Remote -v
```

Expected: compile failure — `SetRemoteToggle`, `SetRemoteState`, `applyRemoteAccessChange`, `configPayloadWithRemoteState` undefined.

- [ ] **Step 3: Add the hub plumbing**

In `internal/ws/hub.go`, add to the `Hub` struct:

```go
	// remoteState is what remote access is actually doing, as opposed to what
	// the config says was asked for. The phone shows both: the toggle reflects
	// the request, this reflects reality.
	remoteState  remote.State
	onRemoteToggle func(enable bool)
```

And the methods:

```go
// SetRemoteState records what remote access is currently doing so it can be
// reported to every phone.
func (h *Hub) SetRemoteState(st remote.State) {
	h.mu.Lock()
	h.remoteState = st
	h.mu.Unlock()
	h.broadcastStatus()
}

// SetRemoteToggle installs the callback that actually starts or stops remote
// access. Without one, a change from the phone is recorded and nothing else —
// which is what tests and embeddings that have no listener want.
func (h *Hub) SetRemoteToggle(fn func(enable bool)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onRemoteToggle = fn
}

// applyRemoteAccessChange starts or stops remote access when want differs from
// what the config already held. The settings screen sends the whole config on
// every save, so the comparison is what keeps an unrelated change from bouncing
// the listener and dropping whoever is connected through it.
func (h *Hub) applyRemoteAccessChange(want bool) {
	h.mu.Lock()
	current := h.cfg != nil && h.cfg.RemoteAccess != nil && *h.cfg.RemoteAccess
	fn := h.onRemoteToggle
	h.mu.Unlock()

	if fn == nil || want == current {
		return
	}
	fn(want)
}

// configPayloadWithRemoteState is configToPayload plus what remote access is
// actually doing.
func (h *Hub) configPayloadWithRemoteState(cfg *config.Config) ConfigPayload {
	payload := configToPayload(cfg)
	h.mu.RLock()
	st := h.remoteState
	h.mu.RUnlock()
	payload.RemoteState = &st
	return payload
}
```

In `messages.go`, add to `ConfigPayload`:

```go
	RemoteState            *remote.State         `json:"remote_state,omitempty"`
```

Import `"github.com/leavesafe/leavesafe/internal/remote"` in both files. **Check for an import cycle**: `internal/remote` imports `internal/network` and `internal/safe`, neither of which imports `internal/ws`, so this is fine. If a cycle appears, move `State` and `UPnPState` into their own leaf package rather than weakening the types.

- [ ] **Step 4: Call it from `handleUpdateConfig`**

At `hub.go:1202`, drop the remote fields from `needsRestart`:

```go
	needsRestart := p.Port != cfg.Port ||
		(p.ConnectionMode != "" && p.ConnectionMode != cfg.ConnectionMode)
```

Capture the wanted value before `cfg.RemoteAccess` is overwritten, and apply it after the config is saved and the lock released. At `:1245`:

```go
	remoteWanted := p.RemoteAccess
	remoteChanged := remoteWanted != oldRemote
	ra := remoteWanted
	cfg.RemoteAccess = &ra
	if p.RemotePort > 0 {
		cfg.RemotePort = p.RemotePort
	}
```

At `:1336`, correct the message and add the live application. `applyRemoteAccessChange` compares against `h.cfg`, which has already been updated by this point, so call the callback directly here:

```go
	if needsRestart {
		h.PushAlert(NewAlert("system", "warning",
			"The port or the Bluetooth mode changed — restart required to take effect"))
	}
	if remoteChanged {
		h.mu.RLock()
		fn := h.onRemoteToggle
		h.mu.RUnlock()
		if fn != nil {
			// Applied outside the hub's lock: bringing the listener up asks the
			// router for a port mapping and the internet for an address, and
			// neither is quick enough to hold every phone's status behind.
			go fn(remoteWanted)
		}
	}
```

Replace the `configToPayload(cfg)` call in the `get_config` handler with `h.configPayloadWithRemoteState(cfg)`.

- [ ] **Step 5: Install the callback in `main.go`**

After `applyRemoteState` is defined (Task 5):

```go
	hub.SetRemoteToggle(func(enable bool) {
		st := remoteCtl.Disable()
		if enable {
			st = remoteCtl.Enable(ctx)
		}
		applyRemoteState(sb, hub, srv, st)
	})
```

Disabling first makes the callback idempotent for the enable case too: a change of `remote_port` arrives as the same signal and has to rebind rather than return the old port.

- [ ] **Step 6: Run the tests and watch them pass**

```
go test ./internal/ws/ ./cmd/leavesafe/ -v
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/ws/hub.go internal/ws/messages.go internal/ws/remote_toggle_test.go cmd/leavesafe/main.go
git commit -m "Apply a connection mode change instead of asking for a restart"
```

---

### Task 7: The `mode` console command

**Files:**
- Modify: `cmd/leavesafe/main.go:1092+` (`runConsole`), and the `help` output
- Test: covered by Task 6's hub tests plus a manual check; the console loop reads `os.Stdin` directly and is not unit-tested in this codebase

**Interfaces:**
- Consumes: `remote.Controller`, `applyRemoteState` (Task 5)

- [ ] **Step 1: Add the command**

`runConsole` needs the controller, the server and the config. Extend its signature:

```go
func runConsole(hub *ws.Hub, sb *statusBar, localAlarm *alarm.Alarm, authMgr *auth.Manager,
	installMethod update.Method, updateLedger *update.Ledger,
	srv *server.Server, remoteCtl *remote.Controller, cfg *config.Config,
) {
```

Update the call site in `safe.Supervise("console", ...)` to match.

Add the case, next to `case line == "status":`:

```go
		case line == "mode":
			st := remoteCtl.State()
			cur := 1
			if st.Enabled {
				cur = 2
			}
			sb.writeLine("  [1] WiFi only   [2] Remote access   (currently %d)", cur)
			sb.writeLine("  Type 1 or 2, or press enter to leave it alone:")
			if !scanner.Scan() {
				break
			}
			var want bool
			switch strings.TrimSpace(scanner.Text()) {
			case "1":
				want = false
			case "2":
				want = true
			default:
				sb.writeLine("  Left unchanged")
				break
			}
			if want == st.Enabled {
				sb.writeLine("  Already in that mode")
				break
			}
			enabled := want
			cfg.RemoteAccess = &enabled
			if err := config.Save(cfg); err != nil {
				sb.writeLine("  %s[NET]%s Could not save the setting: %v", cRed, cReset, err)
				break
			}
			next := remoteCtl.Disable()
			if want {
				next = remoteCtl.Enable(context.Background())
			}
			applyRemoteState(sb, hub, srv, next)
			sb.writeLine("  %s[NET]%s Connection mode: %s", cGreen, cReset, connectionModeName(want))
```

Note the `break` inside the `switch` inside the `case` breaks the inner switch, not the outer one — restructure using a labelled block or an early `continue` on the outer `for`. The simplest correct form is to pull the parse into a small helper that returns `(want bool, ok bool)` and `break` once on `!ok`.

- [ ] **Step 2: Add it to `help`**

Find the `help` case in `runConsole` and add a line matching the existing format:

```
  mode          switch between Wi-Fi only and remote access
```

- [ ] **Step 3: Build and check by hand**

```
go build -o leavesafe.exe ./cmd/leavesafe
./leavesafe.exe
```

Then in the running dashboard: type `help` and confirm `mode` is listed; type `mode`, choose 2, and confirm the URL list gains a public URL (or a clear reason why it did not) without the process restarting. Type `mode` again, choose 1, and confirm it goes away. Confirm a phone connected over Wi-Fi throughout stayed connected.

- [ ] **Step 4: Commit**

```bash
git add cmd/leavesafe/main.go
git commit -m "Let the terminal change the connection mode without a restart"
```

---

### Task 8: Show the phone whether it is actually working

**Files:**
- Modify: `web/src/lib/protocol.ts:99-105` (`AppConfig`)
- Modify: `web/src/components/SettingsSheet.tsx:130-144`
- Modify: `web/src/lib/store.ts` if `config` needs the new field threaded through — check first
- Build: `web/dist/app.js` is committed; rebuild it

**Interfaces:**
- Consumes: `remote_state` in the config payload (Task 6)

- [ ] **Step 1: Add the type**

In `web/src/lib/protocol.ts`, beside `remote_access`:

```ts
export interface RemoteState {
    enabled: boolean;
    public_url?: string;
    cert_fp?: string;
    upnp?: 'ok' | 'failed' | 'cgnat';
    manual_port?: number;
    reason?: string;
}
```

and in `AppConfig`:

```ts
    remote_access: boolean;
    remote_port: number;
    remote_state?: RemoteState;
```

- [ ] **Step 2: Rename the toggle and show the state**

In `SettingsSheet.tsx`, replace the `Toggle` at line 130 and add a status block under it:

```tsx
                            <Toggle
                                label="Reach it from anywhere"
                                hint="Lets your phone connect over mobile data or another network, over HTTPS."
                                value={draft.remote_access}
                                onChange={(remote_access) => patch({ remote_access })}
                            />
                            <RemoteStatus state={current?.remote_state} on={draft.remote_access} />
```

Note the hint no longer says "Restart required" — that is the point of this change. `current` rather than `draft` is deliberate: the state describes what the laptop is doing now, not what the unsaved draft asks for.

Add the component beside `UpdateStatus`:

```tsx
/**
 * Whether remote access is actually reachable, which the toggle alone cannot
 * say.
 *
 * A user who turns this on and gets nothing has no way to tell a router that
 * refused the port mapping from an ISP that cannot offer one at all, and the
 * two need entirely different responses — so the laptop names which it is
 * rather than leaving the phone to guess.
 */
function RemoteStatus({ state, on }: { state?: RemoteState; on: boolean }) {
    if (!on) return null;
    if (!state) return <p class="group-note">Checking…</p>;

    if (state.upnp === 'cgnat') {
        return (
            <p class="group-note">
                Your internet provider puts this connection behind carrier-grade NAT, so the
                laptop cannot be reached from outside your network. Nothing on the laptop or the
                router changes that. The local network still works normally.
            </p>
        );
    }

    return (
        <>
            {state.public_url ? (
                <Field label="Reachable at">
                    <span class="field-readout figure">{state.public_url}</span>
                </Field>
            ) : (
                <p class="group-note">No public address found yet.</p>
            )}
            {state.upnp === 'failed' && (
                <p class="group-note">
                    Your router refused an automatic port mapping. Forward TCP port{' '}
                    {state.manual_port} to this laptop in the router's admin page.
                </p>
            )}
            {state.cert_fp && (
                <Field label="Certificate">
                    <span class="field-readout figure">{state.cert_fp.slice(0, 11)}…</span>
                </Field>
            )}
        </>
    );
}
```

Import `RemoteState` from `../lib/protocol`.

- [ ] **Step 3: Drop the restart hint from the port field**

The `Num` at line 136 says `hint="restart required"`. It no longer is — the toggle callback rebinds. Change it to `hint="used when reaching it from anywhere"`.

- [ ] **Step 4: Build and typecheck**

```
cd web && npm run build
```

Expected: no TypeScript errors, and `web/dist/app.js` rewritten.

- [ ] **Step 5: Check it renders**

```
go build -o leavesafe.exe ./cmd/leavesafe && ./leavesafe.exe
```

Pair a phone or a browser at a local URL, open Settings, toggle "Reach it from anywhere", save, and confirm the status block appears with either a public URL, a manual-port instruction, or the CGNAT explanation — and that the page never disconnected.

- [ ] **Step 6: Commit**

```bash
git add web/src/lib/protocol.ts web/src/components/SettingsSheet.tsx web/dist/
git commit -m "Tell the phone whether remote access is actually reachable"
```

---

### Task 9: Documentation

**Files:**
- Modify: `SECURITY.md`
- Modify: `README.md:388-420` (the "Remote access" section) and `:320-322` (the feature list)
- Modify: `CHANGELOG.md`

- [ ] **Step 1: Record the transport change in `SECURITY.md`**

Find the section describing the TLS posture and add:

> Remote access runs on a second listener rather than converting the only one.
> The local-network listener stays on plain HTTP whether remote access is on or
> off, which is the same posture as the default Wi-Fi-only mode: a pairing key
> sent over the LAN is not encrypted, and a hostile machine on the same Wi-Fi
> can read it. Earlier versions upgraded the local listener to TLS whenever
> remote access was on, at the cost of a self-signed certificate warning on
> every local connection. The internet-facing listener is TLS-only and will not
> start without a certificate.

- [ ] **Step 2: Update the README**

In "Remote access (over mobile data)":
- Replace "You are asked which mode you want on first run, and can change it later from the phone's settings screen. Either way it takes a restart." with a statement that the terminal asks on every start with the current setting as the default, that the phone can change it at any time, and that **the change takes effect immediately — nothing restarts and a phone connected over Wi-Fi stays connected.**
- Add carrier-grade NAT to "If your router has no UPnP" as a separate case that no port forwarding can fix, and say that LeaveSafe detects and reports it.
- In the feature list at line 322, change "Optional remote access over HTTPS for reaching the laptop from another network" to say it can be switched on and off while running.

- [ ] **Step 3: Add a CHANGELOG entry**

Match the existing format at the top of `CHANGELOG.md`. Cover: the question is asked on every start with the saved value as the default; the mode applies immediately from the phone or the new `mode` console command; the local connection is never interrupted; the phone now reports UPnP and CGNAT problems by name.

- [ ] **Step 4: Commit**

```bash
git add SECURITY.md README.md CHANGELOG.md
git commit -m "Record the connection mode changes in the docs"
```

---

### Task 10: Full verification and the PR

- [ ] **Step 1: Run everything**

```
go build ./...
go test ./...
golangci-lint run
cd web && npm run build && cd ..
```

All four must pass. Paste the actual output — do not summarise it.

- [ ] **Step 2: Confirm the central claim by hand**

Start the binary, pair a phone over Wi-Fi, then toggle "Reach it from anywhere" on and off twice from the phone. The phone must stay connected through all four transitions. If it drops, the design's main premise is not met and the cause has to be found before the PR goes up — check that nothing in the toggle path touches the local listener.

- [ ] **Step 3: Push and open the PR**

```bash
git push -u origin feature/live-connection-mode
gh pr create --base main --title "Switch the connection mode without a restart" --body "..."
```

The body should state what changed, that the local listener no longer moves to TLS when remote access is on (pointing at the `SECURITY.md` update), and the manual verification from Step 2. No AI attribution, no session links.

---

## Self-Review

**Spec coverage**

| Spec requirement | Task |
|---|---|
| Two listeners, local never closes | 2 |
| `internal/remote` controller with the named `State` | 3 |
| Prompt every interactive start, saved value as default | 4 |
| `-headless` / unreadable stdin skip the prompt | 4 |
| Three entry points, one code path | 3 (controller), 5 (startup), 6 (phone), 7 (console) |
| `statusBar.setURLs` | 5 |
| `needsRestart` drops remote fields; message corrected | 6 |
| Phone sees public URL, UPnP state, cert fingerprint | 6 (payload), 8 (UI) |
| CGNAT detection | 1 (predicate), 3 (policy), 8 (message) |
| Error table: cert / UPnP / no IP / CGNAT | 3 |
| `SECURITY.md` updated for the plain-HTTP local listener | 9 |
| Test: local WebSocket survives remote start/stop | 2 |

Every row has a task.

**Known rough edges, called out rather than left to be discovered**

- Task 5 Step 3 has a real ambiguity in the existing code: `newHeadlessStatusBar` is passed `authMgr.PairingKey()` while `buildDashboard` uses `authMgr.RawPairingKey()` for the QR. The step says to add a `rawKey` field and set it in both constructors. Do that rather than guessing which one `setURLs` should use.
- Task 7 Step 1 contains a `break` inside a `switch` inside a `case`, which in Go breaks the inner switch. The step names the bug and says to restructure. Do not copy the snippet verbatim.
- Task 5 Step 8 will not compile until Task 6 adds `SetRemoteState`. Run Task 6 before Task 5's final build, or add the stub as the step says.
- `test/harness/app.go:240` and `test/e2e/remote_test.go:24` seed `remote_access`. They should keep passing untouched, since the harness starts the app with `-headless`, which now skips the prompt rather than forcing `false`. Confirm this in Task 10 Step 1 rather than assuming it.
