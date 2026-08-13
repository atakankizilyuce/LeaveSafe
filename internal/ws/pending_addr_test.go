package ws

import (
	"testing"

	"github.com/leavesafe/leavesafe/internal/auth"
)

// A phone reconnects every time its screen locks, so the slot it took has to
// come back to the address it was taken for. The cap keyed the per-address
// count on the bare host and gave it back under the host:port the socket
// arrived with, so the count only ever went up: after four reconnects the
// owner's phone was refused by its own laptop until the process was restarted.
func TestAPhoneCanReconnectPastItsShare(t *testing.T) {
	hub := testHub(t)
	const addr = "192.0.2.20:41000"

	for i := range maxPendingConnsPerAdr * 3 {
		client := &Client{hub: hub, remoteAddr: addr}
		if !hub.pending.acquire(auth.NormalizeAddr(addr)) {
			t.Fatalf("reconnect %d was refused: the phone's own slots were never given back", i)
		}
		client.pendingHeld = true
		hub.releasePending(client)
	}
}

// The per-address table is also the thing that must not grow without bound. A
// stream of connections from changing sources leaves an entry behind for every
// one of them when release does not name the same peer acquire did — which is
// memory an unauthenticated stranger gets to spend.
func TestReleasingLeavesNoAddressBehind(t *testing.T) {
	hub := testHub(t)

	for i := range 200 {
		addr := netAddr(i)
		client := &Client{hub: hub, remoteAddr: addr}
		if !hub.pending.acquire(auth.NormalizeAddr(addr)) {
			t.Fatalf("connection %d was refused while the table should have been empty", i)
		}
		client.pendingHeld = true
		hub.releasePending(client)
	}

	hub.pending.mu.Lock()
	left := len(hub.pending.byAddr)
	hub.pending.mu.Unlock()
	if left != 0 {
		t.Errorf("the per-address table kept %d entries for sockets that are gone", left)
	}
}

func netAddr(i int) string {
	return "198.51.100." + itoa(i%256) + ":" + itoa(40000+i)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [8]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}
