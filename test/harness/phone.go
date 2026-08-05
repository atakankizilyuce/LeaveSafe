package harness

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/leavesafe/leavesafe/internal/ws"
)

// Phone is a WebSocket client that speaks the protocol the mobile UI speaks.
// Incoming messages are drained by a background reader so status broadcasts
// never block an Expect waiting for a different type.
type Phone struct {
	t      *testing.T
	conn   *websocket.Conn
	ctx    context.Context
	cancel context.CancelFunc
	inbox  chan ws.ServerMessage
}

// Dial connects to a running app and starts the reader.
func Dial(t *testing.T, port int) *Phone {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	url := fmt.Sprintf("ws://127.0.0.1:%d/ws", port)

	dialCtx, dialCancel := context.WithTimeout(ctx, 10*time.Second)
	defer dialCancel()

	conn, _, err := websocket.Dial(dialCtx, url, nil) //nolint:bodyclose // the response body is not used
	if err != nil {
		cancel()
		t.Fatalf("dial %s: %v", url, err)
	}
	conn.SetReadLimit(1 << 20)

	p := &Phone{
		t:      t,
		conn:   conn,
		ctx:    ctx,
		cancel: cancel,
		inbox:  make(chan ws.ServerMessage, 64),
	}

	go p.readLoop()
	t.Cleanup(p.Close)
	return p
}

func (p *Phone) readLoop() {
	for {
		var msg ws.ServerMessage
		if err := wsjson.Read(p.ctx, p.conn, &msg); err != nil {
			close(p.inbox)
			return
		}
		select {
		case p.inbox <- msg:
		case <-p.ctx.Done():
			return
		}
	}
}

// Send writes one client message.
func (p *Phone) Send(msg ws.ClientMessage) {
	p.t.Helper()
	ctx, cancel := context.WithTimeout(p.ctx, 10*time.Second)
	defer cancel()
	if err := wsjson.Write(ctx, p.conn, msg); err != nil {
		p.t.Fatalf("send %s: %v", msg.Type, err)
	}
}

// Expect waits for the next message of the given type, discarding others.
func (p *Phone) Expect(msgType string, within time.Duration) ws.ServerMessage {
	p.t.Helper()
	deadline := time.After(within)
	var seen []string
	for {
		select {
		case msg, ok := <-p.inbox:
			if !ok {
				p.t.Fatalf("connection closed while waiting for %q (saw %v)", msgType, seen)
			}
			if msg.Type == msgType {
				return msg
			}
			seen = append(seen, msg.Type)
		case <-deadline:
			p.t.Fatalf("timed out after %s waiting for %q (saw %v)", within, msgType, seen)
		}
	}
}

// ExpectNot fails the test if the given type arrives within the window.
func (p *Phone) ExpectNot(msgType string, within time.Duration) {
	p.t.Helper()
	deadline := time.After(within)
	for {
		select {
		case msg, ok := <-p.inbox:
			if !ok {
				return
			}
			if msg.Type == msgType {
				p.t.Fatalf("received %q, which must not happen here", msgType)
			}
		case <-deadline:
			return
		}
	}
}

// Authenticate sends a pairing key and returns the auth_ok or auth_fail reply.
func (p *Phone) Authenticate(key string) ws.ServerMessage {
	p.t.Helper()
	p.Send(ws.ClientMessage{Type: ws.MsgTypeAuth, Key: key})

	deadline := time.After(10 * time.Second)
	for {
		select {
		case msg, ok := <-p.inbox:
			if !ok {
				p.t.Fatal("connection closed while authenticating")
			}
			if msg.Type == ws.MsgTypeAuthOK || msg.Type == ws.MsgTypeAuthFail {
				return msg
			}
		case <-deadline:
			p.t.Fatal("timed out waiting for an auth reply")
		}
	}
}

// Close tears down the connection. Safe to call more than once.
func (p *Phone) Close() {
	p.cancel()
	_ = p.conn.Close(websocket.StatusNormalClosure, "")
}
