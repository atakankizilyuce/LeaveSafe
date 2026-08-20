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
	client := hub.RegisterExternalClient(rec, nil)
	hub.handleMessage(client, ClientMessage{Type: MsgTypeAuth, Key: "0000000000000000"})
	fail, ok := rec.saw(MsgTypeAuthFail)
	if !ok {
		t.Fatal("a wrong key was not refused at all")
	}
	return fail.Reason
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

// Transitional: released apps still send the key itself, and this change is not
// the one that breaks them.
func TestTheOldKeyMessageStillPairs(t *testing.T) {
	hub := testHub(t)
	client, rec, _ := greeted(t, hub)

	hub.handleMessage(client, ClientMessage{Type: MsgTypeAuth, Key: hub.authManager.RawPairingKey()})

	if !client.authenticated {
		t.Fatal("the old key message no longer pairs")
	}
	authOK, ok := rec.saw(MsgTypeAuthOK)
	if !ok {
		t.Fatal("the old key message was not answered with auth_ok")
	}
	// There is no client nonce in the old message, so there is nothing for the
	// laptop to prove itself against. Inventing a proof would be worse than
	// omitting one: it would look like an answer to a challenge nobody made.
	if authOK.Proof != "" {
		t.Errorf("auth_ok answered a keyed pairing with a proof %q", authOK.Proof)
	}
}

// A client sending both is judged by the proof. The key field is the weaker of
// the two and must not be able to override the stronger one.
func TestAKeyAlongsideAProofIsJudgedByTheProof(t *testing.T) {
	hub := testHub(t)
	key := hub.authManager.RawPairingKey()
	client, rec, serverNonce := greeted(t, hub)

	msg := authWithProof("0000000000000000", serverNonce, fixedClientNonce)
	msg.Key = key // the right key, behind a proof that is wrong
	hub.handleMessage(client, msg)

	if client.authenticated {
		t.Fatal("a correct key rescued a wrong proof")
	}
	if _, ok := rec.saw(MsgTypeAuthFail); !ok {
		t.Error("a wrong proof beside a right key was not refused")
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
	client := hub.RegisterExternalClient(rec, nil)
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
	client := hub.RegisterExternalClient(rec, nil)
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
