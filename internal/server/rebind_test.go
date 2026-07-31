package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"nhooyr.io/websocket"
)

// dialThrough returns an HTTP client that connects to addr no matter what host
// the URL names, which is what DNS rebinding does to a browser.
func dialThrough(addr string) *http.Client {
	dialer := &net.Dialer{}
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				return dialer.DialContext(ctx, network, addr)
			},
		},
	}
}

// TestWebSocketRejectsReboundHost is the test the Origin check does not cover.
//
// The Origin check compares Origin against Host. In a DNS rebinding attack the
// browser sends the attacker's own domain as both — the page came from
// evil.example, and evil.example now resolves to this machine — so they match
// and the socket opens. Only the Host check refuses it, and this proves it does:
// without requireAddressHost this handshake succeeds.
func TestWebSocketRejectsReboundHost(t *testing.T) {
	srv := startTestServer(t)
	addr := fmt.Sprintf("127.0.0.1:%d", srv.Port())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, resp, err := websocket.Dial(ctx, "ws://evil.example/ws", &websocket.DialOptions{ //nolint:bodyclose // closed below when non-nil
		HTTPClient: dialThrough(addr),
		HTTPHeader: http.Header{"Origin": []string{"http://evil.example"}},
	})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err == nil {
		_ = conn.Close(websocket.StatusNormalClosure, "")
		t.Fatal("a rebound host opened a WebSocket to the alarm")
	}
}

// TestPageRejectsReboundHost covers the same attack against the served page.
//
// Refusing only the socket would leave the attacker's page able to read the UI
// and whatever a future version puts in it. The Host check sits in front of
// everything for that reason.
func TestPageRejectsReboundHost(t *testing.T) {
	srv := startTestServer(t)
	addr := fmt.Sprintf("127.0.0.1:%d", srv.Port())

	resp, err := dialThrough(addr).Get("http://evil.example/")
	if err != nil {
		t.Fatalf("get /: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMisdirectedRequest {
		t.Errorf("a rebound host got HTTP %d, want %d", resp.StatusCode, http.StatusMisdirectedRequest)
	}
}

// The refusal must not cost the ordinary case: the QR code, `urls` and the
// certificate SANs are all address literals, and localhost is what the Vite dev
// proxy presents. Every one of these has to keep working.
func TestAddressHostsAreAccepted(t *testing.T) {
	for _, host := range []string{
		"127.0.0.1", "127.0.0.1:9443", "192.168.1.20:9443", "85.1.2.3:9443",
		"localhost", "localhost:5173", "LOCALHOST:9443",
		"[::1]", "[::1]:9443", "::1",
	} {
		if !isAddressHost(host) {
			t.Errorf("host %q was refused; it is an address LeaveSafe hands out", host)
		}
	}
	for _, host := range []string{
		"evil.example", "evil.example:9443", "laptop.local", "leavesafe.attacker.com:80",
	} {
		if isAddressHost(host) {
			t.Errorf("host %q was accepted; a DNS name is how rebinding arrives", host)
		}
	}
}
