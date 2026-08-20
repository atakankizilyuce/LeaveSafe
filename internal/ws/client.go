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
	data, err := json.Marshal(msg)
	if err != nil {
		log.Errorf("marshal message: %v", err)
		return
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
