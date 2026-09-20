package ws

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"
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
	// The stretched key the proofs are really computed under, hex-encoded.
	fixedStretched   = "a6586de3786c30d63085deeb32dc6373f48edad705023dc45d85ad9480a8a5d9"
	fixedClientProof = "9a809db5d1addb37a4fefca7fe21b1ba6113d89321482a05068340c7379043c5"
	fixedServerProof = "6734e4b93d162d4f8f34e8a9a87e571e26d95a1bc6189eefc8e50fd4f5af6dbf"
)

// greeted returns a stand-in phone that has been greeted, so it is holding the
// challenge a real socket would have been sent, together with the recorder the
// hub wrote it to and the nonce the greeting carried.
func greeted(t *testing.T, hub *Hub) (*Client, *recorder, string) {
	t.Helper()
	rec := &recorder{}
	client := hub.RegisterExternalClient(rec, nil)
	rec.watching(client)
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

// authWithProof builds what a current app sends: a nonce of its own, the
// construction it wants the rest sealed with, and a proof over all of it. No
// key at all.
func authWithProof(key, serverNonce, clientNonce string) ClientMessage {
	return ClientMessage{
		Type:    MsgTypeAuth,
		Nonce:   clientNonce,
		Encrypt: encChaCha,
		Proof: handshakeProof(stretchedForTest(key), proofRoleClient,
			serverNonce, clientNonce, encChaCha),
	}
}

// stretchedForTest is stretchedKey with a cache that remembers every key rather
// than the last one.
//
// Argon2 is a fifth of a second by design and these tests run through a dozen
// keys many times over, which would be a minute of the suite spent proving
// nothing. The production cache holds one entry because a daemon holds one
// pairing key; a test holds as many as it has scenarios.
var forTest = struct {
	mu   sync.Mutex
	seen map[string][]byte
}{seen: make(map[string][]byte)}

func stretchedForTest(key string) []byte {
	forTest.mu.Lock()
	defer forTest.mu.Unlock()

	if got, ok := forTest.seen[key]; ok {
		return got
	}
	var one keyStretcher
	got := one.of(key)
	forTest.seen[key] = got
	return got
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
	rec.watching(client)
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
			rec.watching(client)

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
	// The stretched key first: it is the input to both proofs and to the
	// session, so a change to the Argon2 parameters or to the salt would move
	// every one of them at once and this is where that shows up by name.
	if got := hex.EncodeToString(stretchedForTest(fixedKey)); got != fixedStretched {
		t.Errorf("stretched key is %s, want %s", got, fixedStretched)
	}
	if got := handshakeProof(stretchedForTest(fixedKey), proofRoleClient,
		fixedServerNonce, fixedClientNonce, encChaCha); got != fixedClientProof {
		t.Errorf("client proof is %s, want %s", got, fixedClientProof)
	}
	if got := handshakeProof(stretchedForTest(fixedKey), proofRoleServer,
		fixedServerNonce, fixedClientNonce, encChaCha); got != fixedServerProof {
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
	rec.watching(client)
	client.serverNonce = fixedServerNonce

	hub.handleMessage(client, ClientMessage{
		Type:    MsgTypeAuth,
		Nonce:   fixedClientNonce,
		Encrypt: encChaCha,
		Proof:   fixedClientProof,
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
	rec.watching(client)
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

	// The bytes rather than what the recorder made of them: the recorder opens
	// what it is given, and the question here is what went down the wire.
	onTheWire, ok := rec.raw(sealedType)
	if !ok {
		t.Fatal("a message after the acceptance went out in the clear")
	}
	if strings.Contains(string(onTheWire), "the cable came out") {
		t.Errorf("the sealed frame still carries its contents: %s", onTheWire)
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

	app, err := newSession(stretchedForTest(key), serverNonce, fixedClientNonce, false)
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

// An app that asks for something this daemon does not know is refused, and so
// is one that asks for nothing.
//
// Both used to pair and carry on in the clear, which was a kindness to old apps
// and was also the whole of the downgrade: a machine on the path edited this
// one field — it sat outside both proofs — and the laptop obligingly held a
// plaintext conversation with a phone that had asked for an encrypted one.
// There is no longer a value of it that means "do not seal".
func TestAnAppThatWillNotSealIsRefused(t *testing.T) {
	for name, asked := range map[string]string{
		"a construction nobody has implemented": "something-nobody-has-implemented",
		"nothing at all":                        "",
	} {
		t.Run(name, func(t *testing.T) {
			hub := testHub(t)
			client, rec, serverNonce := greeted(t, hub)

			msg := authWithProof(hub.authManager.RawPairingKey(), serverNonce, fixedClientNonce)
			msg.Encrypt = asked
			hub.handleMessage(client, msg)

			if _, ok := rec.saw(MsgTypeAuthOK); ok {
				t.Fatal("an app that would not seal was paired anyway")
			}
			fail, ok := rec.saw(MsgTypeAuthFail)
			if !ok {
				t.Fatal("an app that would not seal was told nothing")
			}
			if !strings.Contains(fail.Reason, "too old") {
				t.Errorf("refused with %q, want a reason naming which end is out of date", fail.Reason)
			}
			if client.sealed != nil || client.authenticated {
				t.Error("the connection was paired under a construction nobody agreed on")
			}
		})
	}
}

// And the field is inside both proofs, so it cannot be edited on the way here
// either. An app that asks to seal and has its request stripped on the path
// does not pair: the proof it sent covers what it asked for, and the laptop
// checks it against what arrived.
func TestStrippingTheConstructionBreaksTheProof(t *testing.T) {
	hub := testHub(t)
	client, rec, serverNonce := greeted(t, hub)

	// What an honest app sends, with the one field a machine on the path would
	// have deleted deleted — and nothing else touched.
	msg := authWithProof(hub.authManager.RawPairingKey(), serverNonce, fixedClientNonce)
	msg.Encrypt = encChaCha + " but not really"
	hub.handleMessage(client, msg)

	if _, ok := rec.saw(MsgTypeAuthOK); ok {
		t.Fatal("an edited auth message was believed")
	}
}

// The proofs are computed under the stretched key, not the digits, and a proof
// over the digits does not pair.
//
// This is the whole of what the Argon2 buys. Anything that can watch one
// pairing on a café network records both nonces and both proofs, and can then
// work through the 10^15 possible keys offline for as long as it likes. Under a
// bare HMAC that is one hash per guess — a few graphics cards do ten billion a
// second, and fifty bits falls in about a day and a half. Under a memory-hard
// derivation each guess costs thirty-two mebibytes and a fifth of a second, and
// the same search is three thousand years.
func TestAProofOverTheBareDigitsDoesNotPair(t *testing.T) {
	hub := testHub(t)
	client, rec, serverNonce := greeted(t, hub)
	key := hub.authManager.RawPairingKey()

	hub.handleMessage(client, ClientMessage{
		Type:    MsgTypeAuth,
		Nonce:   fixedClientNonce,
		Encrypt: encChaCha,
		// What the old protocol signed: an HMAC keyed on the digits themselves.
		Proof: handshakeProof([]byte(key), proofRoleClient,
			serverNonce, fixedClientNonce, encChaCha),
	})

	if _, ok := rec.saw(MsgTypeAuthOK); ok {
		t.Fatal("a proof over the bare digits was accepted")
	}
}

// The stretch is cached, because Argon2 is a fifth of a second by design and a
// phone reconnects every time its screen unlocks. One entry is all a daemon
// needs — it holds one pairing key — and a rotation replaces it.
func TestTheStretchIsCachedUntilTheKeyChanges(t *testing.T) {
	var one keyStretcher

	first := one.of(fixedKey)
	if again := one.of(fixedKey); &again[0] != &first[0] {
		t.Error("the same key was stretched twice")
	}

	other := one.of("8791234567890129")
	if string(other) == string(first) {
		t.Fatal("two different keys stretched to the same thing")
	}
	// And the cache moved with it rather than keeping both.
	if back := one.of(fixedKey); &back[0] == &first[0] {
		t.Error("the cache kept an entry it had been asked to replace")
	}
}
