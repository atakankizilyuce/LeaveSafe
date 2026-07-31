package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/leavesafe/leavesafe/internal/auth"
	"github.com/leavesafe/leavesafe/internal/monitor"
	"github.com/leavesafe/leavesafe/internal/ws"
	"nhooyr.io/websocket"
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
