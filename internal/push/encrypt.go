// Package push delivers an alarm to a phone that cannot be reached.
//
// Every other way this program talks to a phone needs the phone to open a
// connection to it, and behind carrier-grade NAT or a hotspot there is no
// address for it to open one to. The alert then travels over a WebSocket that
// does not exist: the laptop shrieks in the room it is in, and the owner — who
// may be the only person who would do anything about it — is told nothing at
// all.
//
// Web push reverses the direction. The phone subscribes while it is on the same
// network as the laptop, which is exactly when it is paired, and hands over a
// URL at its browser's push service. The laptop then reaches that service with
// an ordinary outbound HTTPS request, which works from behind any NAT that lets
// anything out at all.
//
// The push service is not trusted with the message. The payload is encrypted to
// a key only the subscribing phone holds, by RFC 8291, so what is relayed is
// ciphertext the relay cannot read — which is what makes this an acceptable
// answer for a program whose whole subject is somebody being where they should
// not be.
package push

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

const (
	// The lengths RFC 8291 fixes. An uncompressed P-256 point is 65 octets, the
	// salt is 16, and the authentication secret the phone generates is 16.
	publicKeyLen = 65
	saltLen      = 16
	authLen      = 16

	// recordSize is how large a record may be. One message is one record here —
	// an alarm is a sentence, not a stream — so this only ever says "everything
	// fits".
	recordSize = 4096

	// The two info strings, each terminated by a zero octet, exactly as the
	// specification writes them. They are not decoration: they are what stops a
	// key derived for one purpose being usable for another.
	keyInfoPrefix = "WebPush: info\x00"
	cekInfo       = "Content-Encoding: aes128gcm\x00"
	nonceInfo     = "Content-Encoding: nonce\x00"
)

// encrypt seals plaintext so that only the holder of the subscription's private
// key can read it.
//
// The steps are RFC 8291's, in its order, and the order is the specification's
// to change rather than this program's. What is worth knowing while reading:
// the shared secret comes from a key pair generated for this one message, so
// two pushes to the same phone share no key material; the phone's own
// authentication secret is mixed in as the HKDF salt, so a push service that
// somehow obtained the ECDH secret still could not derive the content key; and
// the sending public key travels in the header, because the phone needs it to
// arrive at the same secret from the other side.
func encrypt(plaintext []byte, sub Subscription, salt []byte, sender *ecdh.PrivateKey) ([]byte, error) {
	receiver, err := ecdh.P256().NewPublicKey(sub.Key)
	if err != nil {
		return nil, fmt.Errorf("the subscription's public key is not a P-256 point: %w", err)
	}

	shared, err := sender.ECDH(receiver)
	if err != nil {
		return nil, fmt.Errorf("agree a shared secret: %w", err)
	}

	senderPublic := sender.PublicKey().Bytes()

	keyInfo := make([]byte, 0, len(keyInfoPrefix)+2*publicKeyLen)
	keyInfo = append(keyInfo, keyInfoPrefix...)
	keyInfo = append(keyInfo, sub.Key...)
	keyInfo = append(keyInfo, senderPublic...)

	ikm, err := hkdf.Key(sha256.New, shared, sub.Auth, string(keyInfo), sha256.Size)
	if err != nil {
		return nil, fmt.Errorf("derive the input keying material: %w", err)
	}

	key, err := hkdf.Key(sha256.New, ikm, salt, cekInfo, aes.BlockSize)
	if err != nil {
		return nil, fmt.Errorf("derive the content key: %w", err)
	}
	nonce, err := hkdf.Key(sha256.New, ikm, salt, nonceInfo, gcmNonceLen)
	if err != nil {
		return nil, fmt.Errorf("derive the nonce: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("prepare the cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("prepare the cipher: %w", err)
	}

	// The padding delimiter. 0x02 says this is the last record, which for a
	// single-record message is the only thing it can say — and leaving it out
	// is a message the phone discards without a word.
	record := append(append([]byte{}, plaintext...), 0x02)

	return append(header(salt, senderPublic), aead.Seal(nil, nonce, record, nil)...), nil
}

// gcmNonceLen is the twelve octets AES-GCM takes.
const gcmNonceLen = 12

// header is the aes128gcm content coding's preamble, which travels in the clear
// because the phone needs all of it before it can decrypt anything.
func header(salt, senderPublic []byte) []byte {
	out := make([]byte, 0, saltLen+4+1+publicKeyLen)
	out = append(out, salt...)
	out = binary.BigEndian.AppendUint32(out, recordSize)
	// Always sixty-five, because that is what an uncompressed P-256 point is
	// and nothing else is ever put here.
	out = append(out, byte(len(senderPublic))) //nolint:gosec // a fixed 65
	return append(out, senderPublic...)
}

// newSalt is the per-message salt, drawn fresh every time.
//
// No error to return: since Go 1.24 crypto/rand.Read always fills what it is
// given, and a machine whose randomness has failed takes the process down
// rather than handing back something weak. Carrying an error here would be a
// branch that cannot be reached and cannot be tested, on the one code path
// where a reader most wants to know what happens if it fails.
func newSalt() []byte {
	salt := make([]byte, saltLen)
	rand.Read(salt) //nolint:errcheck // documented never to fail; it fatals instead
	return salt
}
