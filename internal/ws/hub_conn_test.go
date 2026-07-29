package ws

import (
	"context"
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
