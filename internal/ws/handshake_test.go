package ws

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/leavesafe/leavesafe/internal/auth"
	"github.com/leavesafe/leavesafe/internal/eventlog"
	"github.com/leavesafe/leavesafe/internal/monitor"
)

// The constants below are one worked example of the handshake, shared with the
// app so either side can be tested against the same numbers without running the
// other. fixedKey is a real 16-digit pairing key, check digit and all, so it can
// also be handed to an auth.Manager.
const (
	fixedKey         = "4839201746583123"
	fixedServerNonce = "a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
	fixedClientNonce = "0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c4b5a69788796a5b4c3d2e1f0"
	fixedClientProof = "f785b21993fb53c846fb29a00730f0a0d6c66b2d6a4bc96786191da158633221"
	fixedServerProof = "5c874905866a1293e83eef4fd52a74c78b0bf5fc57d8e279d63cc0817a23622e"
)

// greeted returns a stand-in phone that has been greeted, so it is holding the
// challenge a real socket would have been sent, together with the recorder the
// hub wrote it to and the nonce the greeting carried.
func greeted(t *testing.T, hub *Hub) (*Client, *recorder, string) {
	t.Helper()
	rec := &recorder{}
	client := hub.RegisterExternalClient(rec, nil)
	hub.greet(client)
	hello, ok := rec.saw(MsgTypeHello)
	if !ok {
		t.Fatal("the greeting never reached the client")
	}
	if !validNonce(hello.Nonce) {
		t.Fatalf("the greeting carried %q, which is not a hex-encoded 32-byte nonce", hello.Nonce)
	}
	return client, rec, hello.Nonce
}

// authWithProof builds what a current app sends: a nonce of its own and a proof
// over both nonces, and no key at all.
func authWithProof(key, serverNonce, clientNonce string) ClientMessage {
	return ClientMessage{
		Type:  MsgTypeAuth,
		Nonce: clientNonce,
		Proof: handshakeProof(key, proofRoleClient, serverNonce, clientNonce),
	}
}

// hubWithKey returns a hub whose pairing key is fixed, so a test can assert
// against proofs computed by hand.
func hubWithKey(t *testing.T, key string) *Hub {
	t.Helper()
	authMgr, err := auth.NewManagerWithOptions(auth.Options{PairingKey: key})
	if err != nil {
		t.Fatalf("auth manager: %v", err)
	}
	return NewHub(authMgr, monitor.NewManager(), "test")
}

// wrongKeyReason is what a plain wrong key is refused with, read from a hub of
// its own so no test here hard-codes wording the auth manager owns.
func wrongKeyReason(t *testing.T) string {
	t.Helper()
	hub := testHub(t)
	rec := &recorder{}
	client := challenged(hub.RegisterExternalClient(rec, nil))
	hub.handleMessage(client, provingAuth(client, "0000000000000000"))
	fail, ok := rec.saw(MsgTypeAuthFail)
	if !ok {
		t.Fatal("a wrong key was not refused at all")
	}
	return fail.Reason
}

// provingAuth is authWithProof for a test that built its client directly
// rather than through greeted(): it plants on the connection the challenge
// HandleConnection would have sent, and answers it.
//
// It exists because there is no longer any other way to authenticate. Every
// test that used to hand over provingAuth(client, …) says
// the same thing through this instead — including the ones that mean to fail,
// which now pass a key that is wrong rather than a field that is gone.
func provingAuth(client *Client, key string) ClientMessage {
	return authWithProof(key, client.serverNonce, fixedClientNonce)
}

// challenged plants on a client the nonce HandleConnection would have sent,
// and hands it back so a test can wrap the line that made it.
//
// Separate from provingAuth, and not a write hidden inside it, because one of
// these tests hands the same client to several goroutines at once: a helper
// that filled the nonce in on first use would be two goroutines writing the
// same field, which is a data race whatever the value they agree on. Planting
// it once, where the client is made, is a fact about the connection rather
// than about the message.
func challenged(client *Client) *Client {
	if client.serverNonce == "" {
		client.serverNonce = fixedServerNonce
	}
	return client
}

// The whole point of the exchange: a client that can compute the proof holds
// the pairing key, and is paired without ever putting it on the wire.
func TestAClientThatProvesItHoldsTheKeyIsPaired(t *testing.T) {
	hub := testHub(t)
	client, rec, serverNonce := greeted(t, hub)

	hub.handleMessage(client, authWithProof(hub.authManager.RawPairingKey(), serverNonce, fixedClientNonce))

	if !client.authenticated {
		t.Fatal("a correct proof did not pair the client")
	}
	if _, ok := rec.saw(MsgTypeAuthOK); !ok {
		t.Error("a correct proof was not answered with auth_ok")
	}
}

// A proof computed with anything other than the pairing key is a wrong key, and
// has to be refused in exactly the words a wrong key is refused in. Anything
// else hands an attacker a way to tell "your proof was malformed" apart from
// "your key was wrong", which is the difference between guessing and knowing.
func TestAProofFromTheWrongKeyIsRefused(t *testing.T) {
	hub := testHub(t)
	client, rec, serverNonce := greeted(t, hub)

	hub.handleMessage(client, authWithProof("0000000000000000", serverNonce, fixedClientNonce))

	if client.authenticated {
		t.Fatal("a proof computed with the wrong key paired the client")
	}
	fail, ok := rec.saw(MsgTypeAuthFail)
	if !ok {
		t.Fatal("a wrong proof was not answered with auth_fail")
	}
	if want := wrongKeyReason(t); fail.Reason != want {
		t.Errorf("a wrong proof was refused with %q, want the wrong-key wording %q", fail.Reason, want)
	}
}

// This is why the proof is bound to the server's nonce. Without that binding,
// anything that could read one exchange could replay the client's half onto a
// connection of its own and be paired.
func TestAProofOverAnotherConnectionsChallengeIsRefused(t *testing.T) {
	hub := testHub(t)
	key := hub.authManager.RawPairingKey()

	_, _, firstNonce := greeted(t, hub)
	second, rec, secondNonce := greeted(t, hub)
	if firstNonce == secondNonce {
		t.Fatal("two connections were challenged with the same nonce")
	}

	// A perfectly good proof — for the other connection.
	hub.handleMessage(second, authWithProof(key, firstNonce, fixedClientNonce))

	if second.authenticated {
		t.Fatal("a proof over another connection's challenge was accepted")
	}
	if _, ok := rec.saw(MsgTypeAuthFail); !ok {
		t.Error("a replayed proof was not refused")
	}
}

// A proof with no client nonce behind it is refused even when it is otherwise
// perfectly computed. An app whose own randomness failed would send exactly
// this, and pairing it would leave the laptop's half of the exchange bound to
// nothing the app chose — replayable at that app forever after.
func TestAnAuthWithAProofButNoNonceIsRefused(t *testing.T) {
	hub := testHub(t)
	client, rec, serverNonce := greeted(t, hub)

	hub.handleMessage(client, authWithProof(hub.authManager.RawPairingKey(), serverNonce, ""))

	if client.authenticated {
		t.Fatal("an auth message with no client nonce was accepted")
	}
	if _, ok := rec.saw(MsgTypeAuthFail); !ok {
		t.Error("an auth message with no client nonce was not refused")
	}
}

// A nonce on its own proves nothing, and with no key either there is nothing
// here to authenticate.
func TestAnAuthWithANonceButNoProofIsRefused(t *testing.T) {
	hub := testHub(t)
	client, rec, _ := greeted(t, hub)

	hub.handleMessage(client, ClientMessage{Type: MsgTypeAuth, Nonce: fixedClientNonce})

	if client.authenticated {
		t.Fatal("an auth message carrying only a nonce was accepted")
	}
	if _, ok := rec.saw(MsgTypeAuthFail); !ok {
		t.Error("an auth message carrying only a nonce was not refused")
	}
}

// The nonce is a fixed-width lowercase hex string. Anything else is refused
// before it is used, so nothing downstream has to reason about its shape.
func TestAMalformedNonceIsRefused(t *testing.T) {
	hub := testHub(t)
	key := hub.authManager.RawPairingKey()

	for name, nonce := range map[string]string{
		"too short":   "abc123",
		"not hex":     strings.Repeat("z", 64),
		"upper case":  strings.ToUpper(fixedClientNonce),
		"too long":    fixedClientNonce + "00",
		"with spaces": " " + fixedClientNonce[1:],
	} {
		t.Run(name, func(t *testing.T) {
			client, rec, serverNonce := greeted(t, hub)
			hub.handleMessage(client, authWithProof(key, serverNonce, nonce))
			if client.authenticated {
				t.Fatalf("a client nonce that was %s was accepted", name)
			}
			if _, ok := rec.saw(MsgTypeAuthFail); !ok {
				t.Errorf("a client nonce that was %s was not refused", name)
			}
		})
	}
}

// A transport that never issued a challenge has no server nonce, so no proof
// over one can be verified here. Refusing is the only honest answer.
//
// The second case is the one that matters. A connection with no challenge on it
// holds the empty string, and without the guard the proof would simply be
// computed over that — leaving a fixed, publicly known challenge that anything
// in radio range of the BLE transport could answer once and reuse forever.
func TestAProofOnAConnectionThatWasNeverChallengedIsRefused(t *testing.T) {
	hub := testHub(t)
	key := hub.authManager.RawPairingKey()

	for name, serverNonce := range map[string]string{
		"over a challenge from elsewhere": fixedServerNonce,
		"over the absent challenge":       "",
	} {
		t.Run(name, func(t *testing.T) {
			rec := &recorder{}
			client := hub.RegisterExternalClient(rec, nil)

			hub.handleMessage(client, authWithProof(key, serverNonce, fixedClientNonce))

			if client.authenticated {
				t.Fatalf("a proof %s was accepted on a connection that was never challenged", name)
			}
			if _, ok := rec.saw(MsgTypeAuthFail); !ok {
				t.Errorf("a proof %s on an unchallenged connection was not refused", name)
			}
		})
	}
}

// An app that answers the greeting with no proof is one from before the
// handshake, and it is turned away.
//
// This used to pair. While it did, the pairing key still crossed the wire on
// every such pairing — which is the one thing the handshake was built to stop,
// and the reason a listener on a café network had anything to collect.
//
// The daemon no longer has a field to read the key out of, so what arrives from
// such an app is exactly this: an auth message with nothing in it but its type.
func TestAnAuthWithNoProofIsRefused(t *testing.T) {
	hub := testHub(t)
	client, rec, _ := greeted(t, hub)

	hub.handleMessage(client, ClientMessage{Type: MsgTypeAuth})

	if client.authenticated {
		t.Fatal("an auth message with no proof paired the client")
	}
	fail, ok := rec.saw(MsgTypeAuthFail)
	if !ok {
		t.Fatal("an auth message with no proof was not refused")
	}
	// Told which end is out of date, rather than that the key was wrong. The
	// app says the mirror of this to somebody whose daemon is too old to prove
	// itself, and a person holding one of the two needs to know which.
	if !strings.Contains(fail.Reason, "too old") {
		t.Errorf("refused with %q, want a reason that says the app is too old", fail.Reason)
	}
}

// And it costs nothing against the lockout. Nothing was guessed: the message
// names no key and learns nothing from the answer, so counting it would let an
// app that is merely out of date lock its owner out of pairing the moment they
// updated it.
//
// Read off the refusals rather than by flooding, because a connection has an
// allowance of its own for auth messages and exhausting that would prove
// something else.
func TestAnAuthWithNoProofSpendsNoAttempt(t *testing.T) {
	hub := testHub(t)
	client, rec, serverNonce := greeted(t, hub)

	hub.handleMessage(client, ClientMessage{Type: MsgTypeAuth})
	unproven, ok := rec.saw(MsgTypeAuthFail)
	if !ok {
		t.Fatal("an auth message with no proof was not refused")
	}
	if unproven.RemainingAttempts != hub.authManager.MaxAttempts() {
		t.Errorf("after an unproven auth, %d attempts remained, want all %d",
			unproven.RemainingAttempts, hub.authManager.MaxAttempts())
	}

	// A real guess, for contrast: this one is counted.
	rec.reset()
	hub.handleMessage(client, authWithProof("0000000000000000", serverNonce, fixedClientNonce))
	guessed, ok := rec.saw(MsgTypeAuthFail)
	if !ok {
		t.Fatal("a proof computed with the wrong key was not refused")
	}
	if guessed.RemainingAttempts != hub.authManager.MaxAttempts()-1 {
		t.Errorf("after a wrong proof, %d attempts remained, want %d",
			guessed.RemainingAttempts, hub.authManager.MaxAttempts()-1)
	}
}

// One nonce per connection, never reused: sockets opened back to back must be
// challenged with different numbers, or the second one's exchange could be
// answered with a recording of the first.
func TestEachConnectionIsChallengedWithItsOwnNonce(t *testing.T) {
	srv := hubServer(t, testHub(t))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	seen := make(map[string]bool)
	for range 8 {
		conn, _, err := websocket.Dial(ctx, wsURL(srv), nil) //nolint:bodyclose // response body is not used
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		hello := readHello(t, ctx, conn)
		conn.Close(websocket.StatusNormalClosure, "")

		if !validNonce(hello.Nonce) {
			t.Fatalf("the greeting carried %q, which is not a hex-encoded 32-byte nonce", hello.Nonce)
		}
		if seen[hello.Nonce] {
			t.Fatalf("nonce %s was issued to two connections", hello.Nonce)
		}
		seen[hello.Nonce] = true
	}
}

// The proof input string is the contract with the app, and it is invisible from
// the outside: change a separator or a role name and every proof still verifies
// against itself while no app can pair. These are the exact bytes both sides
// agreed on, pinned here so that cannot happen quietly.
func TestTheProofsAreTheOnesTheSpecDescribes(t *testing.T) {
	if got := handshakeProof(fixedKey, proofRoleClient, fixedServerNonce, fixedClientNonce); got != fixedClientProof {
		t.Errorf("client proof is %s, want %s", got, fixedClientProof)
	}
	if got := handshakeProof(fixedKey, proofRoleServer, fixedServerNonce, fixedClientNonce); got != fixedServerProof {
		t.Errorf("server proof is %s, want %s", got, fixedServerProof)
	}
	if fixedClientProof == fixedServerProof {
		t.Error("the two roles produce the same proof, so either half would answer for the other")
	}
}

// The half this change exists for. The daemon answers the challenge, so the app
// can tell the machine it actually paired with from anything that rewrote the
// endpoint file and then said "armed, all sensors fine".
func TestAuthOKCarriesTheServersProof(t *testing.T) {
	hub := hubWithKey(t, fixedKey)
	rec := &recorder{}
	client := challenged(hub.RegisterExternalClient(rec, nil))
	client.serverNonce = fixedServerNonce

	hub.handleMessage(client, ClientMessage{
		Type:  MsgTypeAuth,
		Nonce: fixedClientNonce,
		Proof: fixedClientProof,
	})

	authOK, ok := rec.saw(MsgTypeAuthOK)
	if !ok {
		t.Fatal("the worked example did not pair")
	}
	if authOK.Proof != fixedServerProof {
		t.Errorf("auth_ok proved itself with %s, want %s", authOK.Proof, fixedServerProof)
	}
}

// A refused proof is a refused key all the way down: one line in the event log
// and one attempt spent, so a proof-based flood is bounded exactly as a keyed
// one is and cannot push the history out of a size-rotated file.
func TestARefusedProofIsCountedLikeARefusedKey(t *testing.T) {
	hub, path := hubWithEventLog(t)
	client, _, serverNonce := greeted(t, hub)

	hub.handleMessage(client, authWithProof("0000000000000000", serverNonce, fixedClientNonce))

	if n := countEvents(t, path, eventlog.EventAuthFail); n != 1 {
		t.Errorf("a refused proof wrote %d auth failures to the event log, want 1", n)
	}
	if client.authLimiter == nil {
		t.Fatal("a refused proof did not touch the pairing-attempt limiter")
	}
}

// The greeting is sent before anything has proved anything, so the challenge
// must be all it gained.
func TestTheChallengeGivesNothingElseAway(t *testing.T) {
	hub := testHub(t)
	rec := &recorder{}
	client := challenged(hub.RegisterExternalClient(rec, nil))
	hub.greet(client)

	hello, ok := rec.saw(MsgTypeHello)
	if !ok {
		t.Fatal("no greeting was sent")
	}
	body, err := json.Marshal(hello)
	if err != nil {
		t.Fatalf("marshal hello: %v", err)
	}
	if strings.Contains(string(body), hub.authManager.RawPairingKey()) {
		t.Error("the challenge leaks the pairing key")
	}
	if hello.Proof != "" {
		t.Error("the greeting carries a proof, before it has a client nonce to bind one to")
	}
	if hello.Token != "" || hello.Sensors != nil || hello.Armed != nil {
		t.Error("the greeting describes the machine before the client has paired")
	}
}

// sealedAuth is authWithProof plus the request to seal what follows.
func sealedAuth(key, serverNonce string) ClientMessage {
	msg := authWithProof(key, serverNonce, fixedClientNonce)
	msg.Encrypt = encChaCha
	return msg
}

// An app that asks for a sealed session is told it has one, and everything
// after the acceptance goes out sealed.
//
// The acceptance itself does not: it is what tells the app the answer, so it
// is the last message on either side that is readable from the wire.
func TestAnAppThatAsksForASessionGetsOne(t *testing.T) {
	hub := testHub(t)
	client, rec, serverNonce := greeted(t, hub)

	hub.handleMessage(client, sealedAuth(hub.authManager.RawPairingKey(), serverNonce))

	authOK, ok := rec.saw(MsgTypeAuthOK)
	if !ok {
		t.Fatal("a proving auth that asked to seal was not accepted")
	}
	if authOK.Encrypt != encChaCha {
		t.Fatalf("auth_ok named %q, want %q", authOK.Encrypt, encChaCha)
	}
	if client.sealed == nil {
		t.Fatal("the connection was answered with a construction and then not sealed")
	}

	rec.reset()
	client.send(ServerMessage{Type: MsgTypeAlarmActive, Reason: "the cable came out"})

	sealed, ok := rec.saw(sealedType)
	if !ok {
		t.Fatal("a message after the acceptance went out in the clear")
	}
	if sealed.Reason != "" {
		t.Errorf("the sealed frame still carries its contents: reason %q", sealed.Reason)
	}
}

// And what it sealed, the app opens — with the key the app derives for itself
// from what it already had.
func TestWhatTheDaemonSealsTheAppCanOpen(t *testing.T) {
	hub := testHub(t)
	key := hub.authManager.RawPairingKey()
	client, rec, serverNonce := greeted(t, hub)

	hub.handleMessage(client, sealedAuth(key, serverNonce))
	rec.reset()

	client.send(ServerMessage{Type: MsgTypeAlarmActive, Reason: "the cable came out"})

	app, err := newSession(key, serverNonce, fixedClientNonce, false)
	if err != nil {
		t.Fatalf("newSession: %v", err)
	}
	frame, ok := rec.raw(sealedType)
	if !ok {
		t.Fatal("nothing sealed was written")
	}
	plaintext, err := app.open(frame)
	if err != nil {
		t.Fatalf("the app could not open what the daemon sealed: %v", err)
	}

	var got ServerMessage
	if err := json.Unmarshal(plaintext, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Type != MsgTypeAlarmActive || got.Reason != "the cable came out" {
		t.Errorf("opened %+v, want the alarm that was sent", got)
	}
}

// An app that asks for something this daemon does not know is not refused. It
// is answered without a construction named, and carries on in the clear —
// which is what keeps a daemon and an app of different ages working together.
func TestAnUnknownConstructionLeavesTheConnectionInTheClear(t *testing.T) {
	hub := testHub(t)
	client, rec, serverNonce := greeted(t, hub)

	msg := authWithProof(hub.authManager.RawPairingKey(), serverNonce, fixedClientNonce)
	msg.Encrypt = "something-nobody-has-implemented"
	hub.handleMessage(client, msg)

	authOK, ok := rec.saw(MsgTypeAuthOK)
	if !ok {
		t.Fatal("an app asking for an unknown construction was refused outright")
	}
	if authOK.Encrypt != "" {
		t.Errorf("auth_ok named %q, want nothing", authOK.Encrypt)
	}
	if client.sealed != nil {
		t.Error("the connection was sealed under a construction nobody agreed on")
	}
}

// An app that does not ask gets what it has always got.
func TestAnAppThatDoesNotAskIsNotSealed(t *testing.T) {
	hub := testHub(t)
	client, rec, serverNonce := greeted(t, hub)

	hub.handleMessage(client, authWithProof(hub.authManager.RawPairingKey(), serverNonce, fixedClientNonce))

	authOK, ok := rec.saw(MsgTypeAuthOK)
	if !ok {
		t.Fatal("a proving auth was not accepted")
	}
	if authOK.Encrypt != "" || client.sealed != nil {
		t.Error("a connection that asked for nothing was sealed anyway")
	}
}
