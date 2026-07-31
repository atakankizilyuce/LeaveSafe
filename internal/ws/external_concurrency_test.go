package ws

import (
	"sync"
	"testing"

	"github.com/leavesafe/leavesafe/internal/eventlog"
)

// recordingTransport stands in for a non-WebSocket transport and counts what
// the hub wrote to it.
type recordingTransport struct {
	mu   sync.Mutex
	sent int
}

func (t *recordingTransport) Send([]byte) error {
	t.mu.Lock()
	t.sent++
	t.mu.Unlock()
	return nil
}

func (t *recordingTransport) Close() error { return nil }

func (t *recordingTransport) count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.sent
}

// A client's own state — whether it has paired, its session token, and the two
// token buckets that meter it — is kept without a lock, on the understanding
// that one client's messages are handled one at a time. A WebSocket gets that
// for free: the read loop is a single goroutine.
//
// A transport that has no such loop does not, and the BLE backend is one: it
// hands each incoming write to a fresh goroutine, so several of a phone's
// messages can be inside handleMessage at once. Nothing above notices, because
// the damage is not a crash — it is the pairing-attempt bucket being created
// twice and losing whichever copy counted the attempts. That bucket is what
// keeps refused attempts from flooding the size-rotated security log, and it is
// reachable from anything in radio range without a key.
//
// So the serialization is provided at the hub boundary rather than assumed. Run
// under -race, this is the test that says so.
func TestOneExternalClientIsHandledOneMessageAtATime(t *testing.T) {
	hub, path := hubWithEventLog(t)
	transport := &recordingTransport{}
	client := hub.RegisterExternalClient(transport, nil)

	const attempts = 400
	var wg sync.WaitGroup
	for range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			hub.HandleExternalMessage(client, ClientMessage{Type: MsgTypeAuth, Key: "0000000000000000"})
		}()
	}
	wg.Wait()

	// The bucket starts full at max_auth_attempts plus the margin and refills at
	// two a second, so a flood that lands in well under a second cannot get more
	// than that many attempts answered.
	allowed := hub.authManager.MaxAttempts() + authAttemptMargin
	if got := transport.count(); got > allowed {
		t.Errorf("%d of %d concurrent pairing attempts were answered, want at most %d: "+
			"the per-client meter did not hold", got, attempts, allowed)
	}
	if written := countEvents(t, path, eventlog.EventAuthFail); written > allowed {
		t.Errorf("%d refused attempts reached the security log, want at most %d", written, allowed)
	}
}

// Serializing must not turn into a deadlock when a message retires the client
// it arrived on, which is what an expired session does.
func TestAnExternalClientCanBeRemovedFromItsOwnMessage(t *testing.T) {
	hub := testHub(t)
	transport := &recordingTransport{}

	removed := make(chan struct{}, 1)
	client := hub.RegisterExternalClient(transport, func() { removed <- struct{}{} })

	hub.HandleExternalMessage(client, ClientMessage{Type: MsgTypeAuth, Key: hub.authManager.RawPairingKey()})
	if !client.authenticated {
		t.Fatal("the client did not pair")
	}

	hub.RemoveExternalClient(client)
	select {
	case <-removed:
	default:
		t.Error("the transport was never told its client had been let go")
	}
}
