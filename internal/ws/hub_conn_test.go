package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/leavesafe/leavesafe/internal/auth"
	"github.com/leavesafe/leavesafe/internal/monitor"
	"nhooyr.io/websocket"
)

func testHub(t *testing.T) *Hub {
	t.Helper()
	authMgr, err := auth.NewManager()
	if err != nil {
		t.Fatalf("auth manager: %v", err)
	}
	return NewHub(authMgr, monitor.NewManager(), "test")
}

// hubServer stands up an httptest server that feeds accepted sockets into the
// hub, the same wiring the real server uses.
func hubServer(t *testing.T, hub *Hub) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		hub.HandleConnection(r.Context(), conn, r.RemoteAddr)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func wsURL(s *httptest.Server) string {
	return "ws" + strings.TrimPrefix(s.URL, "http")
}

// readHello consumes the greeting the server sends on every new connection and
// returns it. Every test that reads from a socket has to get past this first.
func readHello(t *testing.T, ctx context.Context, conn *websocket.Conn) ServerMessage {
	t.Helper()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read hello: %v", err)
	}
	var msg ServerMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("decode hello: %v", err)
	}
	if msg.Type != MsgTypeHello {
		t.Fatalf("first message was %q, want %q", msg.Type, MsgTypeHello)
	}
	return msg
}

// The greeting is what lets a phone that arrived by QR code check it reached
// the server the code was printed for, before it hands over the pairing key.
func TestHelloCarriesTheCertificateFingerprint(t *testing.T) {
	const fingerprint = "AA:BB:CC:DD"

	hub := testHub(t)
	hub.SetCertFingerprint(fingerprint)
	srv := hubServer(t, hub)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL(srv), nil) //nolint:bodyclose // response body is not used
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	hello := readHello(t, ctx, conn)
	if hello.CertFP != fingerprint {
		t.Errorf("hello carried fingerprint %q, want %q", hello.CertFP, fingerprint)
	}
	if hello.Version != "test" {
		t.Errorf("hello carried version %q, want %q", hello.Version, "test")
	}
}

// On the plain-HTTP local path there is no certificate, and claiming a
// fingerprint that does not exist would give the phone something meaningless to
// compare against.
func TestHelloOmitsTheFingerprintWithoutTLS(t *testing.T) {
	srv := hubServer(t, testHub(t))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL(srv), nil) //nolint:bodyclose // response body is not used
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	if hello := readHello(t, ctx, conn); hello.CertFP != "" {
		t.Errorf("hello carried fingerprint %q on a plain-HTTP server", hello.CertFP)
	}
}

// The greeting must not become a way to learn anything that pairing protects.
func TestHelloRevealsNothingBeforeAuthentication(t *testing.T) {
	hub := testHub(t)
	srv := hubServer(t, hub)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL(srv), nil) //nolint:bodyclose // response body is not used
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read hello: %v", err)
	}
	body := string(data)

	if strings.Contains(body, hub.authManager.RawPairingKey()) {
		t.Error("the greeting leaks the pairing key")
	}
	var msg ServerMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("decode hello: %v", err)
	}
	if msg.Token != "" {
		t.Error("the greeting carries a session token")
	}
	if msg.Sensors != nil || msg.Armed != nil {
		t.Error("the greeting describes the machine before the client has paired")
	}
}

// TestUnauthenticatedConnectionDropped proves a socket that never authenticates
// is closed on the auth deadline, so unauthenticated peers cannot pin
// connections open and exhaust the server.
func TestUnauthenticatedConnectionDropped(t *testing.T) {
	prev := authDeadline
	authDeadline = 250 * time.Millisecond
	defer func() { authDeadline = prev }()

	srv := hubServer(t, testHub(t))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL(srv), nil) //nolint:bodyclose // response body is not used
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// The greeting arrives immediately and says nothing about whether the
	// socket will be allowed to linger; the next read is the one under test.
	readHello(t, ctx, conn)

	readCtx, readCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer readCancel()
	start := time.Now()
	if _, _, err := conn.Read(readCtx); err == nil {
		t.Fatal("connection stayed open past the auth deadline")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("connection closed after %v, expected near the %v deadline", elapsed, authDeadline)
	}
}

// TestAuthenticatedConnectionSurvivesDeadline proves the deadline only applies
// before authentication: a paired client stays connected well past it.
func TestAuthenticatedConnectionSurvivesDeadline(t *testing.T) {
	prev := authDeadline
	authDeadline = 250 * time.Millisecond
	defer func() { authDeadline = prev }()

	hub := testHub(t)
	srv := hubServer(t, hub)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL(srv), nil) //nolint:bodyclose // response body is not used
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	readHello(t, ctx, conn)

	authMsg := `{"type":"auth","key":"` + hub.authManager.RawPairingKey() + `"}`
	if err := conn.Write(ctx, websocket.MessageText, []byte(authMsg)); err != nil {
		t.Fatalf("write auth: %v", err)
	}
	if _, _, err := conn.Read(ctx); err != nil {
		t.Fatalf("expected auth_ok: %v", err)
	}

	// Past the (short) deadline, an authenticated socket must still be usable.
	time.Sleep(2 * authDeadline)
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"ping"}`)); err != nil {
		t.Fatalf("write ping after deadline: %v", err)
	}
	if _, _, err := conn.Read(ctx); err != nil {
		t.Fatalf("authenticated connection was dropped after the deadline: %v", err)
	}
}
