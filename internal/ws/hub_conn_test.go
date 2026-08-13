package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/leavesafe/leavesafe/internal/auth"
	"github.com/leavesafe/leavesafe/internal/monitor"
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

// The greeting names the version, which is what a phone shows before it has
// paired and the only thing it is told at that point.
func TestHelloCarriesTheVersion(t *testing.T) {
	srv := hubServer(t, testHub(t))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL(srv), nil) //nolint:bodyclose // response body is not used
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	if hello := readHello(t, ctx, conn); hello.Version != "test" {
		t.Errorf("hello carried version %q, want %q", hello.Version, "test")
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
	const deadline = 250 * time.Millisecond

	hub := testHub(t)
	hub.SetAuthDeadline(deadline)
	srv := hubServer(t, hub)

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
		t.Errorf("connection closed after %v, expected near the %v deadline", elapsed, deadline)
	}
}

// TestAuthenticatedConnectionSurvivesDeadline proves the deadline only applies
// before authentication: a paired client stays connected well past it.
func TestAuthenticatedConnectionSurvivesDeadline(t *testing.T) {
	const deadline = 250 * time.Millisecond

	hub := testHub(t)
	hub.SetAuthDeadline(deadline)
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
	time.Sleep(2 * deadline)
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"ping"}`)); err != nil {
		t.Fatalf("write ping after deadline: %v", err)
	}
	if _, _, err := conn.Read(ctx); err != nil {
		t.Fatalf("authenticated connection was dropped after the deadline: %v", err)
	}
}
