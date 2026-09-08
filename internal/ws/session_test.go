package ws

import (
	"encoding/json"
	"strings"
	"testing"
)

const (
	sessionKey    = "1234567890123456"
	sessionServer = "a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90"
	sessionClient = "0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c4b5a69788796a5b4c3d2e1f0"
)

// pairOfSessions is the two ends of one connection: what the daemon derived,
// and what an app holding the same key and the same two nonces derives.
func pairOfSessions(t *testing.T) (server, app *session) {
	t.Helper()

	server, err := newSession(sessionKey, sessionServer, sessionClient, true)
	if err != nil {
		t.Fatalf("newSession(server) = %v", err)
	}
	app, err = newSession(sessionKey, sessionServer, sessionClient, false)
	if err != nil {
		t.Fatalf("newSession(app) = %v", err)
	}
	return server, app
}

// The point of the thing: what one end sealed, the other opens, and nothing of
// what was inside is visible on the way.
func TestASealedMessageOpensAtTheOtherEnd(t *testing.T) {
	server, app := pairOfSessions(t)

	frame, err := server.seal([]byte(`{"type":"alarm","sensor":"input"}`))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if strings.Contains(string(frame), "alarm") || strings.Contains(string(frame), "input") {
		t.Errorf("the sealed frame still reads as its contents: %s", frame)
	}

	got, err := app.open(frame)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if string(got) != `{"type":"alarm","sensor":"input"}` {
		t.Errorf("opened %q, want the message that went in", got)
	}
}

// Both directions, in order, on one pair of sessions — because a counter that
// was shared between them would repeat a nonce under one key, which is the one
// mistake this construction does not survive.
func TestBothDirectionsRunTheirOwnCounters(t *testing.T) {
	server, app := pairOfSessions(t)

	for i := range 4 {
		up, err := app.seal([]byte(`{"type":"ping"}`))
		if err != nil {
			t.Fatalf("app seal %d: %v", i, err)
		}
		if _, err := server.open(up); err != nil {
			t.Fatalf("server open %d: %v", i, err)
		}

		down, err := server.seal([]byte(`{"type":"pong"}`))
		if err != nil {
			t.Fatalf("server seal %d: %v", i, err)
		}
		if _, err := app.open(down); err != nil {
			t.Fatalf("app open %d: %v", i, err)
		}
	}
}

// A frame recorded off the wire and played again is refused. Without this the
// seal would stop somebody writing a disarm of their own and not stop them
// replaying the one they watched somebody else send.
func TestAReplayedFrameIsRefused(t *testing.T) {
	server, app := pairOfSessions(t)

	frame, err := app.seal([]byte(`{"type":"disarm"}`))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	if _, err := server.open(frame); err != nil {
		t.Fatalf("the first delivery: %v", err)
	}
	if _, err := server.open(frame); err == nil {
		t.Fatal("the same frame was accepted twice")
	}
}

// And a frame from earlier in the conversation cannot be slipped in later.
func TestAFrameOutOfOrderIsRefused(t *testing.T) {
	server, app := pairOfSessions(t)

	first, err := app.seal([]byte(`{"type":"ping"}`))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	second, err := app.seal([]byte(`{"type":"arm"}`))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	if _, err := server.open(second); err != nil {
		t.Fatalf("the later frame: %v", err)
	}
	if _, err := server.open(first); err == nil {
		t.Fatal("a frame from earlier in the conversation was accepted after a later one")
	}
}

// The whole point of an AEAD rather than a cipher: an edited frame is refused
// rather than delivered as something else.
func TestATamperedFrameDoesNotOpen(t *testing.T) {
	server, app := pairOfSessions(t)

	frame, err := app.seal([]byte(`{"type":"ping"}`))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	var edited sealedFrame
	if err := json.Unmarshal(frame, &edited); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// One character of the body, which is what an attacker on the path has to
	// work with.
	edited.Body = "A" + edited.Body[1:]
	tampered, err := json.Marshal(edited)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	if _, err := server.open(tampered); err == nil {
		t.Fatal("an edited frame opened")
	}
}

// The counter is on the outside of the frame and covered by the tag inside it,
// so moving it is the same kind of failure as editing the body.
func TestAnEditedCounterDoesNotOpen(t *testing.T) {
	server, app := pairOfSessions(t)

	frame, err := app.seal([]byte(`{"type":"ping"}`))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	var edited sealedFrame
	if err := json.Unmarshal(frame, &edited); err != nil {
		t.Fatalf("decode: %v", err)
	}
	edited.Seq = 41
	moved, err := json.Marshal(edited)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	if _, err := server.open(moved); err == nil {
		t.Fatal("a frame whose counter was moved opened")
	}
}

// A relay holds the two ends of a conversation and forwards both proofs
// unchanged — which is the attack the handshake alone does not stop. What it
// cannot do is compute the key those proofs were made with, so nothing it
// writes afterwards opens.
func TestAFrameSealedUnderAnotherKeyDoesNotOpen(t *testing.T) {
	server, _ := pairOfSessions(t)

	impostor, err := newSession("6543210987654321", sessionServer, sessionClient, false)
	if err != nil {
		t.Fatalf("newSession: %v", err)
	}

	frame, err := impostor.seal([]byte(`{"type":"disarm"}`))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if _, err := server.open(frame); err == nil {
		t.Fatal("a frame sealed under a different key opened")
	}
}

// Two connections between the same phone and the same laptop must not share a
// session: the nonces differ, so the keys do, and a frame recorded from one is
// worth nothing on the other.
func TestEachConnectionsSessionIsItsOwn(t *testing.T) {
	server, app := pairOfSessions(t)

	other, err := newSession(sessionKey, sessionClient, sessionServer, true)
	if err != nil {
		t.Fatalf("newSession: %v", err)
	}

	frame, err := app.seal([]byte(`{"type":"disarm"}`))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if _, err := other.open(frame); err == nil {
		t.Fatal("a frame from one connection opened on another")
	}
	if _, err := server.open(frame); err != nil {
		t.Fatalf("the connection it belonged to refused it: %v", err)
	}
}

// A frame that is not one of ours at all — a plain message on a sealed
// connection — is refused as such rather than as a broken key.
func TestAPlainMessageOnASealedConnectionIsRefused(t *testing.T) {
	server, _ := pairOfSessions(t)

	if _, err := server.open([]byte(`{"type":"disarm"}`)); err == nil {
		t.Fatal("a plain message was accepted on a sealed connection")
	}
	if _, err := server.open([]byte(`{"type":"sealed","seq":0,"body":"not base64!"}`)); err == nil {
		t.Fatal("a sealed frame with an unreadable body was accepted")
	}
}

// Missing halves are a programming error, and they fail where they happen
// rather than producing a key derived from an empty string.
func TestASessionNeedsEveryPartOfItsInput(t *testing.T) {
	for name, in := range map[string][3]string{
		"no key":          {"", sessionServer, sessionClient},
		"no server nonce": {sessionKey, "", sessionClient},
		"no client nonce": {sessionKey, sessionServer, ""},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := newSession(in[0], in[1], in[2], true); err == nil {
				t.Error("a session was derived from an incomplete handshake")
			}
		})
	}
}
