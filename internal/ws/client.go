package ws

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/coder/websocket"
	log "github.com/sirupsen/logrus"
)

const writeTimeout = 5 * time.Second

// Transport abstracts the underlying connection (WebSocket, BLE, etc.).
type Transport interface {
	Send(data []byte) error
	Close() error
}

// Client represents a single connected device.
type Client struct {
	hub        *Hub
	conn       *websocket.Conn // nil for non-WebSocket transports
	transport  Transport       // nil for WebSocket clients (uses conn)
	remoteAddr string          // peer address, used to rate-limit pairing per source
	// handling serializes message handling for this client. Everything below it
	// in this struct is per-connection state kept without a lock of its own, on
	// the understanding that one client's messages are handled one at a time.
	//
	// A WebSocket gets that for free — the read loop is a single goroutine — and
	// a transport with no such loop does not. Hub.HandleExternalMessage holds
	// this so the understanding is a guarantee the hub makes rather than one it
	// hopes each transport happens to keep.
	handling      sync.Mutex
	authenticated bool
	token         string
	// serverNonce is this connection's half of the pairing challenge, minted
	// once when the greeting goes out and never reused for another connection.
	// A client's proof is checked against it, so a connection that was never
	// greeted holds none and no proof can be believed on it — which is the
	// case for transports with no accept step of their own, such as BLE.
	serverNonce string
	// pendingHeld records that this client is occupying one of the slots
	// reserved for sockets that have not paired yet, so the slot is given back
	// exactly once — on pairing or on disconnect, whichever comes first.
	// Transports with no accept step of their own never take one.
	pendingHeld bool
	// onRemove is called once when the hub lets this client go, so a transport
	// keeping its own table of clients can drop its entry at the same moment.
	// Nil for WebSocket clients, which keep no such table.
	onRemove func()
	// removed guards onRemove against firing twice: removeClient runs from the
	// connection's own goroutine and from the heartbeat sweep, and both may
	// reach the same client.
	removed bool
	// limiter bounds how fast this client's messages are handled. Created on
	// the first message rather than with the client, so a connection that never
	// says anything costs nothing.
	limiter *tokenBucket
	// authLimiter bounds pairing attempts, separately from limiter. The two
	// cannot share one allowance: a flood from a paired phone would otherwise
	// eat into what a stranger's guesses are counted against.
	authLimiter *tokenBucket
	// sealed is the session key this connection agreed on, or nil for a
	// connection that did not ask for one. Set once, in handleAuth, after the
	// acceptance has gone out — the acceptance itself is what tells the app
	// the answer, so it is the last message on either side that is readable
	// from the wire.
	//
	// It is the one field here that is not confined to the connection's own
	// goroutine, so it is the one that needs a lock: an alarm reaches every
	// client from whichever goroutine noticed it, and that goroutine reads
	// this while the read loop is still writing it. The session behind the
	// pointer has a lock of its own; this one is over the pointer.
	sealedMu sync.RWMutex
	sealed   *session

	// writeMu makes "is this connection sealed yet" a question with one answer
	// for the whole of a write, rather than one answered and then acted on.
	//
	// Reading the pointer under sealedMu is not enough on its own. The
	// acceptance is the last frame either end writes in the clear, and the
	// session is installed immediately after it — so between those two lines
	// there was a window in which any other goroutine's alarm, status or
	// alarm_cleared went out unencrypted on a connection both ends had just
	// agreed to seal. Narrow, and the exact downgrade the handshake exists to
	// make impossible. See acceptAndSeal.
	writeMu sync.Mutex
}

// sealedSession returns the session this connection agreed on, or nil.
func (c *Client) sealedSession() *session {
	c.sealedMu.RLock()
	defer c.sealedMu.RUnlock()
	return c.sealed
}

// sealFrom seals everything this connection sends from here on.
//
// Called only by acceptAndSeal, which holds the write lock across this and the
// acceptance that precedes it. Called on its own it would reopen the window it
// exists to close.
func (c *Client) sealFrom(s *session) {
	c.sealedMu.Lock()
	defer c.sealedMu.Unlock()
	c.sealed = s
}

// unseal returns the plaintext of one frame from this client, or the frame
// itself when the connection is not sealed.
//
// A connection that agreed to seal and then sends something that does not open
// is not a connection with one bad frame on it: nobody else can produce a
// frame that opens, so what arrived was written by somebody else, or is the
// same frame played twice. The caller closes the socket.
func (c *Client) unseal(data []byte) ([]byte, error) {
	sealed := c.sealedSession()
	if sealed == nil {
		return data, nil
	}
	return sealed.open(data)
}

// allowMessage reports whether this client may have another message handled,
// spending one token if so.
//
// Read and written only on the connection's own goroutine, like authenticated
// and pendingHeld, so the bucket needs no lock of its own.
func (c *Client) allowMessage() bool {
	if c.limiter == nil {
		c.limiter = newTokenBucket()
	}
	return c.limiter.allow()
}

// allowAuth reports whether this client may have another pairing attempt
// handled, spending one token if so. Same goroutine discipline as allowMessage.
func (c *Client) allowAuth() bool {
	if c.authLimiter == nil {
		c.authLimiter = newAuthBucket(c.hub.authManager.MaxAttempts())
	}
	return c.authLimiter.allow()
}

// close tears down the underlying connection, whichever transport it uses.
// Errors are ignored: the caller is already dropping this client.
func (c *Client) close() {
	if c.transport != nil {
		_ = c.transport.Close()
		return
	}
	if c.conn != nil {
		_ = c.conn.Close(websocket.StatusNormalClosure, "session expired")
	}
}

// send marshals and writes a message to the client.
func (c *Client) send(msg ServerMessage) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.sendLocked(msg)
}

// acceptAndSeal writes the acceptance in the clear and seals everything after
// it, with nothing able to slip between the two.
//
// One lock over both halves, because the order alone cannot be made safe. Seal
// first and a broadcast racing this reaches a phone as a sealed frame before
// the acceptance that tells it a seal was agreed, which reads as a handshake
// message that will not parse. Send first and the same broadcast goes out in
// the clear. Holding the write lock across both leaves no instant in which
// either is possible: every other sender waits, and what it waits for is the
// connection becoming sealed.
func (c *Client) acceptAndSeal(msg ServerMessage, s *session) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	// Still in the clear: the session is not installed until the line below,
	// and the phone cannot open anything before it has read this.
	c.sendLocked(msg)
	c.sealFrom(s)
}

func (c *Client) sendLocked(msg ServerMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Errorf("marshal message: %v", err)
		return
	}

	if session := c.sealedSession(); session != nil {
		sealed, err := session.seal(data)
		if err != nil {
			// Nothing is sent in the clear as a fallback. A phone that cannot
			// be told something is a phone that shows stale state; a phone
			// told it over a channel this connection agreed to seal is one
			// whose owner believes it is protected and is not.
			log.Errorf("seal message: %v", err)
			return
		}
		data = sealed
	}

	if c.transport != nil {
		if err := c.transport.Send(data); err != nil {
			log.Warnf("write to client: %v", err)
		}
		return
	}

	if c.conn != nil {
		ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
		defer cancel()
		if err := c.conn.Write(ctx, websocket.MessageText, data); err != nil {
			log.Warnf("write to client: %v", err)
		}
	}
}
