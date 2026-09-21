package harness

import (
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/base64"
	binaryenc "encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"

	"golang.org/x/crypto/chacha20poly1305"
)

// What a paired connection is sealed with, written out here rather than
// imported from internal/ws for the reason given on proofFor: a harness that
// called the daemon's own code would agree with it by construction, including
// about a change nobody meant to make.
const (
	// encryption is the construction this phone asks for by name. A daemon
	// that is not asked refuses the pairing.
	encryption = "chacha20-poly1305"

	// sealedType is the only message type that crosses a sealed connection.
	sealedType = "sealed"

	infoToApp    = "leavesafe/v2 session server-to-app"
	infoToDaemon = "leavesafe/v2 session app-to-server"
)

// phoneSession is the application's half of one connection's keys: one per
// direction, and a counter that must strictly increase.
type phoneSession struct {
	mu sync.Mutex

	send    []byte
	receive []byte

	sent     uint64
	received uint64
	seen     bool
}

// newPhoneSession derives the two keys from the stretched pairing key and the
// two nonces the handshake already exchanged. Nothing new is sent to establish
// it: both ends have all three by the time the acceptance is written.
func newPhoneSession(t *testing.T, key []byte, serverNonce, clientNonce string) *phoneSession {
	t.Helper()

	salt := []byte(serverNonce + clientNonce)

	toApp, err := hkdf.Key(sha256.New, key, salt, infoToApp, chacha20poly1305.KeySize)
	if err != nil {
		t.Fatalf("deriving the session key: %v", err)
	}
	toDaemon, err := hkdf.Key(sha256.New, key, salt, infoToDaemon, chacha20poly1305.KeySize)
	if err != nil {
		t.Fatalf("deriving the session key: %v", err)
	}

	// This end is the phone, so it sends under the key the daemon receives on.
	return &phoneSession{send: toDaemon, receive: toApp}
}

// seal wraps one encoded message.
func (s *phoneSession) seal(plaintext []byte) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	aead, err := chacha20poly1305.New(s.send)
	if err != nil {
		return nil, fmt.Errorf("sealing: %w", err)
	}

	seq := s.sent
	s.sent++

	return json.Marshal(map[string]any{
		"type": sealedType,
		"seq":  seq,
		// The counter is the nonce and is authenticated as additional data, so
		// the number on the outside of a frame is covered by the tag inside it.
		"body": base64.StdEncoding.EncodeToString(
			aead.Seal(nil, nonceFor(seq), plaintext, counterBytes(seq)),
		),
	})
}

// open unwraps one sealed frame, or says why it will not.
func (s *phoneSession) open(data []byte) ([]byte, error) {
	var frame struct {
		Type string `json:"type"`
		Seq  uint64 `json:"seq"`
		Body string `json:"body"`
	}
	if err := json.Unmarshal(data, &frame); err != nil || frame.Type != sealedType {
		return nil, errors.New("not a sealed frame")
	}

	body, err := base64.StdEncoding.DecodeString(frame.Body)
	if err != nil {
		return nil, errors.New("a sealed frame carried a body that is not base64")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Strictly increasing, which is what makes a recorded frame worth nothing
	// the second time it is played.
	if s.seen && frame.Seq <= s.received {
		return nil, fmt.Errorf("a sealed frame arrived out of order (%d after %d)",
			frame.Seq, s.received)
	}

	aead, err := chacha20poly1305.New(s.receive)
	if err != nil {
		return nil, fmt.Errorf("opening: %w", err)
	}

	plaintext, err := aead.Open(nil, nonceFor(frame.Seq), body, counterBytes(frame.Seq))
	if err != nil {
		return nil, errors.New("a sealed frame did not open")
	}

	s.received = frame.Seq
	s.seen = true
	return plaintext, nil
}

// nonceFor turns a counter into the twelve bytes ChaCha20-Poly1305 wants: four
// zeroes and the counter, big-endian.
func nonceFor(seq uint64) []byte {
	nonce := make([]byte, chacha20poly1305.NonceSize)
	binaryenc.BigEndian.PutUint64(nonce[4:], seq)
	return nonce
}

// counterBytes is the counter as additional data.
func counterBytes(seq uint64) []byte {
	buf := make([]byte, 8)
	binaryenc.BigEndian.PutUint64(buf, seq)
	return buf
}
