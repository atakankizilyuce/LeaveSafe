package ws

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// The whole thing over a real socket: greet, prove, agree to seal, and then
// hold a conversation neither end sends in the clear.
//
// The tests beside this one drive the hub directly, which is the right shape
// for what the rules are — but the read path is where a sealed frame becomes a
// message again, and a hub whose read loop never learned to open one would
// pass every one of them.
func TestASealedConnectionCarriesRealMessages(t *testing.T) {
	hub := testHub(t)
	key := hub.authManager.RawPairingKey()
	srv := hubServer(t, hub)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL(srv), nil) //nolint:bodyclose // the response body is not used
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	hello := readHello(t, ctx, conn)

	auth := sealedAuth(key, hello.Nonce)
	writeJSON(t, ctx, conn, auth)

	authOK := readJSON(t, ctx, conn)
	if authOK.Type != MsgTypeAuthOK {
		t.Fatalf("auth reply was %q (%s), want %q", authOK.Type, authOK.Reason, MsgTypeAuthOK)
	}
	if authOK.Encrypt != encChaCha {
		t.Fatalf("auth_ok named %q, want %q", authOK.Encrypt, encChaCha)
	}

	app, err := newSession(key, hello.Nonce, auth.Nonce, false)
	if err != nil {
		t.Fatalf("newSession: %v", err)
	}

	// Armed by a sealed message, which is the direction that matters: this is
	// the one an attacker on the path would want to write.
	armed, err := app.seal([]byte(`{"type":"arm"}`))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if err := conn.Write(ctx, websocket.MessageText, armed); err != nil {
		t.Fatalf("write: %v", err)
	}

	// The status the hub broadcasts on arming comes back sealed, and opens.
	deadline := time.Now().Add(3 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatal("nothing sealed came back after arming")
		}

		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read: %v", err)
		}

		var envelope ServerMessage
		if err := json.Unmarshal(data, &envelope); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if envelope.Type != sealedType {
			t.Fatalf("a message came back in the clear as %q", envelope.Type)
		}

		plaintext, err := app.open(data)
		if err != nil {
			t.Fatalf("the app could not open what the daemon sent: %v", err)
		}
		var msg ServerMessage
		if err := json.Unmarshal(plaintext, &msg); err != nil {
			t.Fatalf("decode the opened message: %v", err)
		}
		if msg.Type == MsgTypeStatus && msg.Armed != nil && *msg.Armed {
			break
		}
	}

	if !hub.IsArmed() {
		t.Error("a sealed arm did not arm the machine")
	}
}

// And a connection that agreed to seal is closed on the first frame that does
// not open. Nobody but the two ends can produce one, so what arrived was
// written by somebody else — or is a frame that has already been delivered.
func TestASealedConnectionIsClosedOnAFrameThatDoesNotOpen(t *testing.T) {
	hub := testHub(t)
	key := hub.authManager.RawPairingKey()
	srv := hubServer(t, hub)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL(srv), nil) //nolint:bodyclose // the response body is not used
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	hello := readHello(t, ctx, conn)
	writeJSON(t, ctx, conn, sealedAuth(key, hello.Nonce))

	if authOK := readJSON(t, ctx, conn); authOK.Encrypt != encChaCha {
		t.Fatalf("the connection was not sealed: auth_ok named %q", authOK.Encrypt)
	}

	// A plain message, which is what an attacker who cannot seal has to send.
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"disarm"}`)); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, _, err := conn.Read(ctx); err == nil {
		t.Fatal("a plain message on a sealed connection was answered rather than closing it")
	}
	if hub.IsArmed() {
		t.Error("the machine acted on a message it could not open")
	}
}

// writeJSON and readJSON are the two halves of talking to a hub over a real
// socket, for the tests above.
func writeJSON(t *testing.T, ctx context.Context, conn *websocket.Conn, msg ClientMessage) {
	t.Helper()

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func readJSON(t *testing.T, ctx context.Context, conn *websocket.Conn) ServerMessage {
	t.Helper()

	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var msg ServerMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return msg
}
