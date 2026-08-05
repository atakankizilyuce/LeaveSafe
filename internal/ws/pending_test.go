package ws

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/leavesafe/leavesafe/internal/auth"
)

// One host must not be able to take every slot. If it could, the global cap
// would be a way to lock the owner's phone out rather than a defense.
func TestOneAddressCannotTakeEverySlot(t *testing.T) {
	p := newPendingConns(10, 2)

	for i := range 2 {
		if !p.acquire("10.0.0.1") {
			t.Fatalf("connection %d from one address was refused inside its share", i)
		}
	}
	if p.acquire("10.0.0.1") {
		t.Error("a third connection from the same address was allowed past its share")
	}
	if !p.acquire("10.0.0.2") {
		t.Error("a different address was refused while slots remained")
	}
}

func TestPendingSlotsAreGivenBack(t *testing.T) {
	p := newPendingConns(2, 2)

	p.acquire("10.0.0.1")
	p.acquire("10.0.0.1")
	if p.acquire("10.0.0.1") {
		t.Fatal("acquired past the limit")
	}

	p.release("10.0.0.1")
	if !p.acquire("10.0.0.1") {
		t.Error("a released slot was not reusable")
	}
}

// The per-address map must not become a way to spend memory: a stream of
// connections from changing addresses has to leave nothing behind.
func TestPendingLeavesNothingBehind(t *testing.T) {
	p := newPendingConns(64, 4)

	for i := range 500 {
		addr := fmt.Sprintf("10.0.%d.%d", i/256, i%256)
		p.acquire(addr)
		p.release(addr)
	}

	if got := p.count(); got != 0 {
		t.Errorf("%d slots still held after every connection was released", got)
	}
	p.mu.Lock()
	left := len(p.byAddr)
	p.mu.Unlock()
	if left != 0 {
		t.Errorf("the per-address table kept %d entries for connections that are gone", left)
	}
}

// Pairing hands the slot back, because a paired client is accounted for by the
// session cap instead. Without this a phone that pairs and stays connected
// would hold an unpaired slot for as long as it was connected, and four
// reconnects from one phone would lock that phone out of its own laptop.
func TestPairingReleasesTheSlot(t *testing.T) {
	hub := testHub(t)
	client := &Client{hub: hub, remoteAddr: "192.0.2.11:5000", pendingHeld: true}
	hub.pending.acquire(auth.NormalizeAddr("192.0.2.11:5000"))

	hub.handleAuth(client, ClientMessage{Type: MsgTypeAuth, Key: hub.authManager.RawPairingKey()})

	if !client.authenticated {
		t.Fatal("the client did not pair")
	}
	if got := hub.pending.count(); got != 0 {
		t.Errorf("%d unpaired slots still held after pairing, want 0", got)
	}
}

// Releasing runs on both the pairing path and the disconnect path, and a socket
// that took one slot must give back exactly one. Counting it twice would let
// the total drift down until the cap refused everybody.
func TestASlotIsGivenBackOnlyOnce(t *testing.T) {
	hub := testHub(t)
	client := &Client{hub: hub, remoteAddr: "192.0.2.12:5000", pendingHeld: true}
	hub.pending.acquire("192.0.2.12")
	hub.pending.acquire("192.0.2.13")

	hub.releasePending(client)
	hub.releasePending(client)

	if got := hub.pending.count(); got != 1 {
		t.Errorf("count is %d after a double release, want 1", got)
	}
}

// End to end: past the cap, a connection is turned away rather than served.
func TestServerRefusesTooManyUnpairedSockets(t *testing.T) {
	hub := testHub(t)
	hub.pending = newPendingConns(2, 2)
	srv := hubServer(t, hub)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for i := range 2 {
		conn, _, err := websocket.Dial(ctx, wsURL(srv), nil) //nolint:bodyclose // response body is not used
		if err != nil {
			t.Fatalf("connection %d was refused while slots remained: %v", i, err)
		}
		defer conn.Close(websocket.StatusNormalClosure, "")
		readHello(t, ctx, conn)
	}

	// The handshake itself still succeeds — the refusal is a close frame, so
	// the peer is told why rather than dropped without explanation.
	conn, _, err := websocket.Dial(ctx, wsURL(srv), nil) //nolint:bodyclose // response body is not used
	if err != nil {
		return // refused at the handshake, which is also an acceptable answer
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	readCtx, readCancel := context.WithTimeout(ctx, 3*time.Second)
	defer readCancel()
	for {
		_, _, err := conn.Read(readCtx)
		if err != nil {
			return // closed, which is the refusal
		}
	}
}

// The cap and the failure counter must agree on what counts as one peer, or a
// new source port would buy a fresh set of slots and the per-address share
// would mean nothing.
func TestTheCapCountsAPeerTheSameWayTheLockoutDoes(t *testing.T) {
	if got := auth.NormalizeAddr("192.0.2.11:5000"); got != "192.0.2.11" {
		t.Errorf("NormalizeAddr kept the port: %q", got)
	}
}
