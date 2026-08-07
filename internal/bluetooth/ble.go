package bluetooth

import (
	"errors"
	"sync"
)

// Custom 128-bit UUIDs for LeaveSafe BLE GATT service.
const (
	ServiceUUIDString = "4c454156-4553-4146-452d-424c45000001"
	TxCharUUIDString  = "4c454156-4553-4146-452d-424c45000002" // server -> client (notify)
	RxCharUUIDString  = "4c454156-4553-4146-452d-424c45000003" // client -> server (write)
)

// ErrNoCentralIdentity says this platform's BLE stack does not report which
// central sent a write, so the transport cannot be offered here.
//
// Everything above this package assumes one connection is one device: a client
// authenticates, and what it may do afterwards follows from that. BLE has no
// accept step to hang an identity on, so the identity has to come from the
// stack — and on Linux and Windows it does not come at all. tinygo's bluetooth
// package passes a connection ID of zero for every write on both (BlueZ "doesn't
// seem to tell who did the write"; the Windows backend has a comment where the
// connection should be). Every central in radio range therefore collapses
// into a single client, and the moment the owner's phone pairs, so has everyone
// else's — no key required, from anywhere in radio range.
//
// Refusing the transport is the safe failure, and the same one the TLS path
// takes when it cannot get a certificate: the Wi-Fi server is unaffected and
// pairing still works over it. macOS reports a real per-central identifier and
// is unaffected.
var ErrNoCentralIdentity = errors.New(
	"this platform's Bluetooth stack does not report which device sent a message, " +
		"so a paired session could not be kept to one phone")

// BLETransport implements ws.Transport for BLE clients.
type BLETransport struct {
	mu       sync.Mutex
	sendFunc func(data []byte) error
}

func (t *BLETransport) Send(data []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.sendFunc != nil {
		return t.sendFunc(data)
	}
	return nil
}

func (t *BLETransport) Close() error {
	return nil
}

// Server is declared per platform rather than here, because what it has to
// keep differs: the macOS backend tracks one ws.Client per central, and the
// platforms that refuse to advertise have nothing to track.
