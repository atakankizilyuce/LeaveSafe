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
	msg, err := p.await(msgType, within)
	if err != nil {
		p.t.Fatal(err)
	}
	return msg
}

// Await waits for a message of this type and reports whether it arrived.
//
// It is Expect without the verdict. Expect ends the test the moment its own
// window closes, so a caller looping around it never got a second attempt: a
// loop with a longer deadline outside an Expect with a shorter one is really
// just the shorter one, and every wait built that way was quietly a single
// attempt. A caller that has something else to try needs the answer instead.
func (p *Phone) Await(msgType string, within time.Duration) (ws.ServerMessage, bool) {
	p.t.Helper()
	msg, err := p.await(msgType, within)
	return msg, err == nil
}

func (p *Phone) await(msgType string, within time.Duration) (ws.ServerMessage, error) {
	deadline := time.After(within)
	var seen []string
	for {
		select {
		case msg, ok := <-p.inbox:
			if !ok {
				return ws.ServerMessage{}, fmt.Errorf(
					"connection closed while waiting for %q (saw %v)", msgType, seen)
			}
			if msg.Type == msgType {
				return msg, nil
			}
			seen = append(seen, msg.Type)
		case <-deadline:
			return ws.ServerMessage{}, fmt.Errorf(
				"timed out after %s waiting for %q (saw %v)", within, msgType, seen)
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

// armTimeout is how long the whole wait for an armed status may take. Generous
// on purpose: a hosted runner starting the real binary on a cold machine is
// slow in a way that says nothing about the code.
const armTimeout = 30 * time.Second

// WaitUntilArmed reads status broadcasts until the armed flag matches want, and
// fails the test if it never does.
//
// Configure broadcasts its own status before arm does, so the first status to
// arrive is not the one worth reading — which is why this is a loop rather than
// a single Expect. It is also why the loop has to be able to outlive one
// attempt: written around Expect, the outer deadline never applied, and a
// runner slow enough to take longer than the inner window produced a failed
// build rather than a slower one.
func (p *Phone) WaitUntilArmed(want bool) {
	p.t.Helper()
	deadline := time.Now().Add(armTimeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		status, ok := p.Await(ws.MsgTypeStatus, remaining)
		if !ok {
			break
		}
		if status.Armed != nil && *status.Armed == want {
			return
		}
	}
	p.t.Fatalf("the system never reported armed=%v within %s", want, armTimeout)
}
