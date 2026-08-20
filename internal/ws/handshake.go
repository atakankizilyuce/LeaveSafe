package ws

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
)

// The pairing handshake is a mutual challenge-response over the pairing key,
// and it exists because the app cannot otherwise tell this daemon apart from
// anything else listening on the machine.
//
// The daemon publishes its port in endpoint.json, which is writable by anything
// running as the same user. Under the old exchange the app sent the key in
// plaintext and the laptop answered auth_ok, so whatever rewrote that file
// harvested the key on the first connection and — far worse for an alarm panel
// — could answer auth_ok itself and report "armed, all sensors fine" while the
// real machine sat unwatched. Neither half of that is possible against a peer
// that has to prove it holds the key before it is believed.
const (
	// proofDomain keeps these proofs from being mistaken for, or reused as, any
	// other HMAC this program might one day compute over the same key. The
	// version in it is the protocol's, not the release's: it changes when the
	// shape of the exchange changes, and never otherwise.
	proofDomain = "leavesafe/v1"

	// proofRoleServer and proofRoleClient are what keeps one side's answer from
	// serving as the other's. Without the role in the input string the two
	// proofs over a pair of nonces would be identical, and an impostor could
	// pair by echoing back the very proof the app had just sent it.
	proofRoleServer = "server"
	proofRoleClient = "client"

	// nonceBytes is how much randomness each side contributes. Thirty-two bytes
	// is the width of the hash underneath, so the nonce is never the weak half.
	nonceBytes = 32
	// nonceHexLen is the encoded width every nonce on the wire must have.
	nonceHexLen = nonceBytes * 2
)

// newNonce returns a fresh challenge, hex-encoded.
//
// crypto/rand.Read is documented never to return an error and always to fill
// the buffer it is given: a failure of the operating system's random source
// stops the program rather than hand back bytes that only look random. That is
// exactly the behavior wanted here, because a predictable nonce is a handshake
// that proves nothing while appearing to work, so there is no error to handle.
func newNonce() string {
	buf := make([]byte, nonceBytes)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

// validNonce reports whether s is a nonce this protocol could have produced.
//
// The shape is checked before the nonce is used so that everything downstream
// works on a known quantity, and so a peer cannot steer the proof input by
// sending a nonce with a separator or a newline in it.
func validNonce(s string) bool {
	if len(s) != nonceHexLen {
		return false
	}
	// hex.DecodeString would accept upper case as well. The encoding on the
	// wire is lowercase, and the same bytes written two ways are two different
	// proof inputs, so accepting both would mean a client that pairs or not
	// depending on how its hex encoder happened to be configured.
	for i := 0; i < len(s); i++ {
		c := s[i]
		isDigit := c >= '0' && c <= '9'
		isLowerHex := c >= 'a' && c <= 'f'
		if !isDigit && !isLowerHex {
			return false
		}
	}
	return true
}

// handshakeProof returns the proof for one role over a pair of nonces: an
// HMAC-SHA256 under the pairing key of
//
//	leavesafe/v1|<role>|<server nonce>|<client nonce>
//
// hex-encoded. Both nonces go into both proofs, and that is the reason the
// client sends one at all. A proof bound only to the server's nonce could be
// replayed by whatever recorded it onto any connection that happened to be
// challenged with the same number; a proof bound only to the client's could be
// replayed back at the app by an impostor that had watched a genuine pairing.
// With both in the input, neither half means anything outside the one
// connection whose two nonces produced it.
//
// key is the pairing key as the app knows it — the sixteen digits pairing.key
// holds, without the dashes the dashboard groups them with.
func handshakeProof(key, role, serverNonce, clientNonce string) string {
	mac := hmac.New(sha256.New, []byte(key))
	// Written in parts rather than assembled with Sprintf so the exact bytes
	// being signed are visible here, since they are the contract with the app.
	mac.Write([]byte(proofDomain))
	mac.Write([]byte("|"))
	mac.Write([]byte(role))
	mac.Write([]byte("|"))
	mac.Write([]byte(serverNonce))
	mac.Write([]byte("|"))
	mac.Write([]byte(clientNonce))
	return hex.EncodeToString(mac.Sum(nil))
}

// proofHolds reports whether offered is the proof for this role over these
// nonces.
//
// hmac.Equal rather than ==, because a string comparison stops at the first
// byte that differs and how long it took is measurable over a local socket.
// That turns guessing a proof from an exhaustive search into a byte-at-a-time
// one, which is a difference of many orders of magnitude.
func proofHolds(key, role, serverNonce, clientNonce, offered string) bool {
	want := handshakeProof(key, role, serverNonce, clientNonce)
	return hmac.Equal([]byte(want), []byte(offered))
}
