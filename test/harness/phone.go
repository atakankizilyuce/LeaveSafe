package harness

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"golang.org/x/crypto/argon2"

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

	// greeted closes once the daemon's greeting has arrived, and serverNonce
	// holds the challenge it carried. The read loop fills them in and still
	// passes the greeting on, so a test that wants to look at it can.
	greeted     chan struct{}
	greetedOnce sync.Once
	serverNonce string

	// sealed is what this phone writes under and reads under once it has
	// paired. Everything after the acceptance is sealed, in both directions, so
	// a harness that did not have one would be a harness that could pair and
	// then say nothing.
	sealedMu sync.Mutex
	sealed   *phoneSession
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
		t:       t,
		conn:    conn,
		ctx:     ctx,
		cancel:  cancel,
		inbox:   make(chan ws.ServerMessage, 64),
		greeted: make(chan struct{}),
	}

	go p.readLoop()
	t.Cleanup(p.Close)
	return p
}

func (p *Phone) readLoop() {
	for {
		var raw json.RawMessage
		if err := wsjson.Read(p.ctx, p.conn, &raw); err != nil {
			close(p.inbox)
			return
		}

		msg, err := p.decode(raw)
		if err != nil {
			// Not a frame this phone could have been sent. Nobody else can
			// produce one, so the connection is over rather than the frame
			// being skipped — which is what the application does too.
			close(p.inbox)
			return
		}

		if msg.Type == ws.MsgTypeHello {
			p.greetedOnce.Do(func() {
				p.serverNonce = msg.Nonce
				close(p.greeted)
			})
		}
		select {
		case p.inbox <- msg:
		case <-p.ctx.Done():
			return
		}
	}
}

// Send writes one client message, sealed once this phone has paired.
func (p *Phone) Send(msg ws.ClientMessage) {
	p.t.Helper()
	ctx, cancel := context.WithTimeout(p.ctx, 10*time.Second)
	defer cancel()

	plain, err := json.Marshal(msg)
	if err != nil {
		p.t.Fatalf("encode %s: %v", msg.Type, err)
	}

	if sealed := p.session(); sealed != nil {
		framed, err := sealed.seal(plain)
		if err != nil {
			p.t.Fatalf("seal %s: %v", msg.Type, err)
		}
		plain = framed
	}

	if err := p.conn.Write(ctx, websocket.MessageText, plain); err != nil {
		p.t.Fatalf("send %s: %v", msg.Type, err)
	}
}

// session returns what this phone is sealing under, or nil before it pairs.
func (p *Phone) session() *phoneSession {
	p.sealedMu.Lock()
	defer p.sealedMu.Unlock()
	return p.sealed
}

// sealFrom is called with the session the acceptance settled on. Only after
// the acceptance: that is the last message either end sends in the clear.
func (p *Phone) sealFrom(s *phoneSession) {
	p.sealedMu.Lock()
	defer p.sealedMu.Unlock()
	p.sealed = s
}

// decode turns one frame off the wire into a message, opening it first when
// this phone has a session.
func (p *Phone) decode(raw json.RawMessage) (ws.ServerMessage, error) {
	var msg ws.ServerMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return msg, err
	}
	if msg.Type != sealedType {
		return msg, nil
	}

	sealed := p.session()
	if sealed == nil {
		return msg, errors.New("a sealed frame arrived before the session did")
	}
	plain, err := sealed.open(raw)
	if err != nil {
		return msg, err
	}

	var inside ws.ServerMessage
	if err := json.Unmarshal(plain, &inside); err != nil {
		return inside, err
	}
	return inside, nil
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

	// The greeting first, because its challenge is half of what the proof is
	// over. The daemon sends it as soon as the socket opens, so this is a wait
	// on something already in flight rather than a round trip.
	select {
	case <-p.greeted:
	case <-time.After(10 * time.Second):
		p.t.Fatal("timed out waiting for the daemon's greeting")
	}

	// Undashed, because that is what the proof is over. The daemon prints the
	// key grouped for a person to read and compares it stripped, so a proof
	// computed over the printed form would be an answer to a different
	// question — and the refusal would read as "invalid key", which is exactly
	// how long that would take to work out.
	key = strings.ReplaceAll(key, "-", "")

	// Stretched once, here, because the proofs and the session keys are both
	// computed under it rather than under the digits.
	strong := stretchKey(key)

	clientNonce := freshNonce(p.t)
	p.Send(ws.ClientMessage{
		Type:  ws.MsgTypeAuth,
		Nonce: clientNonce,
		// Asked for by name, and inside the proof. A daemon that is not told
		// which construction this phone wants refuses it now, and a machine on
		// the path that deleted the request would be deleting part of what was
		// signed.
		Encrypt: encryption,
		Proof: proofFor(strong, "client", p.serverNonce, clientNonce,
			encryption),
	})

	deadline := time.After(10 * time.Second)
	for {
		select {
		case msg, ok := <-p.inbox:
			if !ok {
				p.t.Fatal("connection closed while authenticating")
			}
			switch msg.Type {
			case ws.MsgTypeAuthOK:
				// The other half of the exchange. A test that accepted an
				// auth_ok without checking this would pass against a daemon
				// that had stopped proving anything, which is the failure this
				// whole handshake exists to catch.
				//
				// The construction the daemon granted is inside its proof, so
				// checking the proof is also checking that the field naming it
				// was not edited on the way here.
				want := proofFor(strong, "server", p.serverNonce, clientNonce,
					msg.Encrypt)
				if msg.Proof != want {
					p.t.Fatalf("the daemon's proof was %q, want %q", msg.Proof, want)
				}
				if msg.Encrypt != encryption {
					p.t.Fatalf("the daemon named %q, want %q — it will not seal",
						msg.Encrypt, encryption)
				}
				// Only now. Everything after the acceptance is sealed, in both
				// directions, from this line.
				p.sealFrom(newPhoneSession(p.t, strong, p.serverNonce, clientNonce))
				return msg
			case ws.MsgTypeAuthFail:
				return msg
			}
		case <-deadline:
			p.t.Fatal("timed out waiting for an auth reply")
		}
	}
}

// AuthenticateWithoutProof sends an auth message the way a pre-handshake
// release did: the pairing key itself, in the clear, and no answer to the
// challenge.
//
// Written as raw JSON because ws.ClientMessage no longer has a field for it —
// which is the point. This is the wire form an old app still produces, and the
// test that uses this asserts the daemon refuses it. Nothing else should.
func (p *Phone) AuthenticateWithoutProof(key string) ws.ServerMessage {
	p.t.Helper()

	ctx, cancel := context.WithTimeout(p.ctx, 10*time.Second)
	if err := wsjson.Write(ctx, p.conn, map[string]string{
		"type": ws.MsgTypeAuth,
		"key":  key,
	}); err != nil {
		cancel()
		p.t.Fatalf("write legacy auth: %v", err)
	}
	cancel()

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

// The proof, computed here rather than imported from internal/ws.
//
// A test that called the daemon's own function would agree with it by
// construction, including about a change nobody meant to make. These few lines
// are the contract as the application implements it — the same string, built
// from literals — so a change to either side has to be made twice on purpose.
// The same reasoning covers the stretch and the session below it.
func proofFor(key []byte, role, serverNonce, clientNonce, encrypt string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("leavesafe/v2|" + role + "|" + serverNonce + "|" +
		clientNonce + "|" + encrypt))
	return hex.EncodeToString(mac.Sum(nil))
}

// stretchKey turns the sixteen digits into the key the proofs and the session
// are really computed under.
//
// Argon2id, because a proof is guessable offline: anything that watched one
// pairing has both nonces and both proofs, and sixteen digits under a bare
// HMAC is about a day and a half of a few graphics cards. Memory-hard, the
// same search runs to millennia.
func stretchKey(key string) []byte {
	return argon2.IDKey([]byte(key), []byte("leavesafe/v2 lan pairing key"),
		3, 32*1024, 1, 32)
}

// freshNonce is the client's half of the challenge: thirty-two random bytes,
// hex-encoded, which is the width the daemon requires.
func freshNonce(t *testing.T) string {
	t.Helper()

	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("reading random bytes: %v", err)
	}
	return hex.EncodeToString(buf)
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
