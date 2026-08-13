package server

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/leavesafe/leavesafe/internal/auth"
	"github.com/leavesafe/leavesafe/internal/monitor"
	"github.com/leavesafe/leavesafe/internal/ws"
)

func startTestServer(t *testing.T) *Server {
	t.Helper()
	authMgr, err := auth.NewManager()
	if err != nil {
		t.Fatalf("auth manager: %v", err)
	}
	hub := ws.NewHub(authMgr, monitor.NewManager(), "test")

	srv := New(Config{Hub: hub, Port: 0})
	if err := srv.Listen(); err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = srv.Start() }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
	return srv
}

// TestWebSocketRejectsForeignOrigin proves a browser page served from another
// origin cannot open a socket to this server — the cross-site WebSocket
// hijacking and DNS-rebinding path.
func TestWebSocketRejectsForeignOrigin(t *testing.T) {
	srv := startTestServer(t)
	url := fmt.Sprintf("ws://127.0.0.1:%d/ws", srv.Port())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, resp, err := websocket.Dial(ctx, url, &websocket.DialOptions{ //nolint:bodyclose // closed below when non-nil
		HTTPHeader: http.Header{"Origin": []string{"https://evil.example"}},
	})
	if err == nil {
		_ = conn.Close(websocket.StatusNormalClosure, "")
		t.Fatal("handshake with a foreign Origin succeeded")
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
}

// TestWebSocketAcceptsSameOriginAndNoOrigin proves the check does not break the
// two legitimate callers: the embedded UI (same origin) and native clients
// that send no Origin at all.
func TestWebSocketAcceptsSameOriginAndNoOrigin(t *testing.T) {
	srv := startTestServer(t)
	url := fmt.Sprintf("ws://127.0.0.1:%d/ws", srv.Port())

	headers := []http.Header{
		nil, // native client, no Origin
		{"Origin": []string{fmt.Sprintf("http://127.0.0.1:%d", srv.Port())}},
	}
	for _, h := range headers {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		conn, resp, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: h}) //nolint:bodyclose // response body is not used
		if err != nil {
			cancel()
			t.Fatalf("handshake with headers %v failed: %v", h, err)
		}
		_ = conn.Close(websocket.StatusNormalClosure, "")
		_ = resp
		cancel()
	}
}

// TestWebSocketOutlivesTheServerWriteTimeout is the test that has to exist
// alongside the http.Server read and write timeouts.
//
// Those timeouts are sized for fetching a page, and a paired phone holds its
// socket open for hours. What keeps the two compatible is that net/http clears
// a connection's deadlines when a handler hijacks it, which is what the
// WebSocket upgrade does — so the timeouts bound the HTTP path and stop at the
// upgrade. If that ever stops being true, or someone reintroduces a deadline
// after the upgrade, every phone gets cut off seconds after pairing and the
// laptop has no way to raise the alarm. The deadlines here are deliberately
// shorter than the sleep that follows them.
func TestWebSocketOutlivesTheServerWriteTimeout(t *testing.T) {
	const deadline = 300 * time.Millisecond

	authMgr, err := auth.NewManager()
	if err != nil {
		t.Fatalf("auth manager: %v", err)
	}
	hub := ws.NewHub(authMgr, monitor.NewManager(), "test")
	s := &Server{hub: hub}

	ts := httptest.NewUnstartedServer(http.HandlerFunc(s.handleWebSocket))
	ts.Config.ReadTimeout = deadline
	ts.Config.WriteTimeout = deadline
	ts.Start()
	t.Cleanup(ts.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http"), nil) //nolint:bodyclose // response body is not used
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	_ = resp
	defer conn.Close(websocket.StatusNormalClosure, "")

	// The greeting arrives at once; it proves nothing about the deadlines.
	if _, _, err := conn.Read(ctx); err != nil {
		t.Fatalf("read hello: %v", err)
	}

	time.Sleep(3 * deadline)

	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"ping"}`)); err != nil {
		t.Fatalf("writing past the server write timeout failed: %v", err)
	}
	if _, _, err := conn.Read(ctx); err != nil {
		t.Fatalf("the socket was closed by the server timeouts: %v", err)
	}
}

// TestSecurityHeaders proves the static pages go out with the hardening
// headers attached.
func TestSecurityHeaders(t *testing.T) {
	srv := startTestServer(t)

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/", srv.Port()))
	if err != nil {
		t.Fatalf("get /: %v", err)
	}
	defer resp.Body.Close()

	for _, header := range []string{"Content-Security-Policy", "X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy"} {
		if resp.Header.Get(header) == "" {
			t.Errorf("response is missing the %s header", header)
		}
	}
}

// connect-src is the last channel out of this page. Everything else is closed
// by default-src 'self', so a bare `ws:` — which permits a socket to any host
// on the internet — would be the one way script here could ship the pairing
// key somewhere. The UI only ever dials its own origin.
func TestCSPLimitsSocketsToThisHost(t *testing.T) {
	srv := startTestServer(t)
	host := fmt.Sprintf("127.0.0.1:%d", srv.Port())

	resp, err := http.Get("http://" + host + "/")
	if err != nil {
		t.Fatalf("get /: %v", err)
	}
	defer resp.Body.Close()

	csp := resp.Header.Get("Content-Security-Policy")
	connectSrc := ""
	for _, directive := range strings.Split(csp, ";") {
		if trimmed := strings.TrimSpace(directive); strings.HasPrefix(trimmed, "connect-src") {
			connectSrc = trimmed
		}
	}
	if connectSrc == "" {
		t.Fatalf("no connect-src directive in %q", csp)
	}

	for _, wildcard := range []string{"ws:", "wss:", "*"} {
		for _, source := range strings.Fields(connectSrc)[1:] {
			if source == wildcard {
				t.Errorf("connect-src allows every host via %q: %q", wildcard, connectSrc)
			}
		}
	}
	if !strings.Contains(connectSrc, host) {
		t.Errorf("connect-src does not permit this server's own socket: %q", connectSrc)
	}
}

// The same directive, reached from the other side: the Host header used to be
// copied into it as it arrived. A semicolon is a legal byte in a Host and the
// directive separator in a policy, so a request could append directives of its
// own to the policy the page was served with — the one control standing between
// a bug in the UI and the pairing key leaving the phone.
//
// Written as a raw request because an HTTP client would normalize what is being
// tested away.
func TestAHostCannotAppendToThePolicy(t *testing.T) {
	srv := startTestServer(t)

	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", srv.Port()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	smuggled := fmt.Sprintf("127.0.0.1:%d;script-src", srv.Port())
	if _, err := fmt.Fprintf(conn,
		"GET / HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", smuggled); err != nil {
		t.Fatalf("write request: %v", err)
	}

	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMisdirectedRequest {
		t.Errorf("a host carrying a policy directive got HTTP %d, want %d",
			resp.StatusCode, http.StatusMisdirectedRequest)
	}
	if csp := resp.Header.Get("Content-Security-Policy"); strings.Contains(csp, ";script-src ") {
		t.Errorf("the request wrote a directive into the policy: %q", csp)
	}
}

// The addresses in the QR code are the ones a phone will dial. An interface
// that is down, one that only talks to this machine, and the bridges Docker,
// WSL and the VM tools leave behind all answer here and none of them can be
// reached from a phone — a code carrying one of those addresses sends the user
// to a network they are not on.
func TestOnlyReachableInterfacesGoIntoTheCode(t *testing.T) {
	tests := []struct {
		name  string
		iface net.Interface
	}{
		{"an interface that is down", net.Interface{Name: "eth0"}},
		{"the loopback", net.Interface{Name: "lo", Flags: net.FlagUp | net.FlagLoopback}},
		{"a Docker bridge", net.Interface{Name: "docker0", Flags: net.FlagUp}},
		{"a Hyper-V or WSL switch", net.Interface{Name: "vEthernet (WSL)", Flags: net.FlagUp}},
		{"a VirtualBox host adapter", net.Interface{Name: "vboxnet0", Flags: net.FlagUp}},
		// An interface the operating system will not describe is not one a
		// phone can be sent to either.
		{"an interface the system will not describe", net.Interface{Index: 1 << 20, Name: "ghost0", Flags: net.FlagUp}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := reachableIPv4(tt.iface); got != nil {
				t.Errorf("offered %v from %s", got, tt.iface.Name)
			}
		})
	}
}

// Whatever this machine has, the list is never empty: a code with no address in
// it is of no use to anyone, and the loopback at least works where the laptop
// itself is.
func TestThereIsAlwaysAnAddressToPrint(t *testing.T) {
	if len(getLocalIPs()) == 0 {
		t.Error("no address at all was offered for the pairing code")
	}
}

// Each address is offered as a whole URL, on the port actually bound rather
// than the one that was asked for — a busy port sends Listen to pick a free one,
// and a QR code built from the requested port would point at nothing.
func TestURLsNameTheBoundPort(t *testing.T) {
	srv := startTestServer(t)

	urls := srv.URLs()
	if len(urls) == 0 {
		t.Fatal("no address was offered to pair with")
	}
	want := fmt.Sprintf(":%d", srv.Port())
	for _, u := range urls {
		if !strings.HasPrefix(u, "http://") {
			t.Errorf("URL %q does not name the scheme this server actually speaks", u)
		}
		if !strings.HasSuffix(u, want) {
			t.Errorf("URL %q does not end in the bound port %s", u, want)
		}
	}
}

// A machine with nothing dialable — every interface down, or a laptop whose
// only "networks" are the bridges a container runtime left behind — still has
// to produce a code. It just says loopback, and works where the laptop is.
func TestAMachineWithNoUsableInterfaceStillGetsACode(t *testing.T) {
	nothingReachable := [][]net.Interface{
		nil,
		{{Name: "eth0"}},
		{{Name: "docker0", Flags: net.FlagUp}, {Name: "lo", Flags: net.FlagUp | net.FlagLoopback}},
	}

	for _, ifaces := range nothingReachable {
		got := localIPsFrom(ifaces)
		if len(got) != 1 || !got[0].IsLoopback() {
			t.Errorf("localIPsFrom(%v) = %v, want just the loopback", ifaces, got)
		}
	}
}
