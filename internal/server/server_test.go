package server

import (
	"context"
	"fmt"
	"net/http"
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
