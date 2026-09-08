package ws

import (
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"golang.org/x/crypto/chacha20poly1305"
)

// The session layer: what the handshake is worth once the pairing is over.
//
// The proofs establish that both ends hold the pairing key. They do not
// establish a channel. Nothing binds the messages that follow to the two
// parties that just proved themselves, so a machine that can get on the path —
// ARP spoofing on a café network, a rogue access point with the same name —
// can relay the whole exchange between a real phone and a real laptop, forward
// both proofs unchanged, and own the plaintext socket that comes after it.
// What that is worth to it is the whole product: a `disarm` it can inject, an
// alarm frame it can drop, and a PIN it can read as it is typed.
//
// So the handshake now produces a key as well as a verdict. Everything after
// auth_ok is sealed under it, and a frame that was not written by the holder
// of the pairing key does not open.
//
// This is not TLS and does not pretend to be: there is no certificate, no
// authority, and no identity beyond "knows the sixteen digits on the laptop's
// screen". That is exactly the identity this product has, which is why a
// certificate was never the answer for an address like 192.168.1.24.

const (
	// sealedType is the only message type that crosses a sealed connection.
	// Everything else is inside one.
	sealedType = "sealed"

	// encChaCha names the one construction this daemon speaks, and it travels
	// on the wire: the client asks for it by name in its auth message and the
	// acceptance names it back. A second one, some day, is a second constant
	// and a client that asks for whichever it prefers — not a version number
	// this end has to guess at.
	encChaCha = "chacha20-poly1305"

	// The labels the two directional keys are derived under. Two keys rather
	// than one, so that a counter on one side can never name the same
	// (key, nonce) pair as a counter on the other — which is the one mistake
	// this construction does not survive.
	infoToApp    = "leavesafe/v1 session server-to-app"
	infoToDaemon = "leavesafe/v1 session app-to-server"
)

// errNotSealed says a frame is not one of ours. It is worth its own error
// because the read path treats it as "this connection is not sealed after all"
// rather than as an attack.
var errNotSealed = errors.New("ws: not a sealed frame")

// sealedFrame is what a sealed message looks like from outside: a type, a
// counter, and an opaque body. Nothing about what is inside — not the message
// type, not its length beyond the ciphertext's own — is readable without the
// key.
type sealedFrame struct {
	Type string `json:"type"`
	Seq  uint64 `json:"seq"`
	Body string `json:"body"`
}

// session holds the two directional keys and the counters that go with them.
//
// A counter rather than a random nonce: ChaCha20-Poly1305 needs a nonce that
// is never repeated under one key, and a counter is the only way to be certain
// of that rather than probably right. It also gives the ordering check below
// for free.
type session struct {
	mu sync.Mutex

	send    []byte
	receive []byte

	// sent is the next counter this end will use; received is the last one it
	// accepted. A frame at or below received is a replay or a reordering, and
	// neither is something an honest connection produces.
	sent     uint64
	received uint64
	// seen is whether anything has been accepted yet, so that the very first
	// frame — counter zero — is not mistaken for a replay of itself.
	seen bool
}

// newSession derives the session keys from the pairing key and the two nonces
// the handshake already exchanged.
//
// Nothing new is sent to establish it. Both ends have all three inputs by the
// time the acceptance is written, which is what makes this a change to what
// the handshake produces rather than another round trip somebody has to wait
// through.
//
// key is the pairing key as the proofs use it: sixteen digits, no dashes.
func newSession(key, serverNonce, clientNonce string, forServer bool) (*session, error) {
	if key == "" || serverNonce == "" || clientNonce == "" {
		return nil, errors.New("ws: a session needs the key and both nonces")
	}

	salt := []byte(serverNonce + clientNonce)

	toApp, err := hkdf.Key(sha256.New, []byte(key), salt, infoToApp, chacha20poly1305.KeySize)
	if err != nil {
		return nil, fmt.Errorf("ws: deriving the session key: %w", err)
	}
	toDaemon, err := hkdf.Key(sha256.New, []byte(key), salt, infoToDaemon, chacha20poly1305.KeySize)
	if err != nil {
		return nil, fmt.Errorf("ws: deriving the session key: %w", err)
	}

	s := &session{send: toApp, receive: toDaemon}
	if !forServer {
		// The mirror image, for a test standing in for the app.
		s.send, s.receive = toDaemon, toApp
	}
	return s, nil
}

// seal wraps one encoded message.
func (s *session) seal(plaintext []byte) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	aead, err := chacha20poly1305.New(s.send)
	if err != nil {
		return nil, fmt.Errorf("ws: sealing: %w", err)
	}

	seq := s.sent
	s.sent++

	// The counter is the nonce, and it is also authenticated as additional
	// data. Without that, a frame's counter could be edited on the wire: the
	// ciphertext would fail to open, which is a refusal rather than a
	// corruption, but the refusal would look like a broken key rather than
	// like tampering.
	frame := sealedFrame{
		Type: sealedType,
		Seq:  seq,
		Body: base64.StdEncoding.EncodeToString(
			aead.Seal(nil, nonceFor(seq), plaintext, counterBytes(seq)),
		),
	}
	return json.Marshal(frame)
}

// open unwraps one sealed frame, or says why it will not.
func (s *session) open(data []byte) ([]byte, error) {
	var frame sealedFrame
	if err := json.Unmarshal(data, &frame); err != nil || frame.Type != sealedType {
		return nil, errNotSealed
	}

	body, err := base64.StdEncoding.DecodeString(frame.Body)
	if err != nil {
		return nil, errors.New("ws: a sealed frame carried a body that is not base64")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Strictly increasing, which is what makes a recorded frame worth nothing
	// the second time it is played. A gap is allowed — nothing here drops
	// frames, but a transport that did should not be turned into a broken
	// pairing — while a repeat or a step backwards is refused.
	if s.seen && frame.Seq <= s.received {
		return nil, fmt.Errorf("ws: a sealed frame arrived out of order (%d after %d)",
			frame.Seq, s.received)
	}

	aead, err := chacha20poly1305.New(s.receive)
	if err != nil {
		return nil, fmt.Errorf("ws: opening: %w", err)
	}

	plaintext, err := aead.Open(nil, nonceFor(frame.Seq), body, counterBytes(frame.Seq))
	if err != nil {
		// Deliberately without detail. Whoever wrote this frame either holds
		// the key or does not, and the only thing an error message could add
		// is which of their guesses got closer.
		return nil, errors.New("ws: a sealed frame did not open")
	}

	s.received = frame.Seq
	s.seen = true
	return plaintext, nil
}

// nonceFor turns a counter into the twelve bytes ChaCha20-Poly1305 wants: four
// zeroes and the counter, big-endian. The two directions use different keys, so
// the same counter on each is not a reuse.
func nonceFor(seq uint64) []byte {
	nonce := make([]byte, chacha20poly1305.NonceSize)
	binary.BigEndian.PutUint64(nonce[4:], seq)
	return nonce
}

// counterBytes is the counter as additional data, so the number on the outside
// of a frame is covered by the tag inside it.
func counterBytes(seq uint64) []byte {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, seq)
	return buf
}
