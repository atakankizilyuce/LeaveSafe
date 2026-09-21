package ws

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"sync"

	"golang.org/x/crypto/argon2"
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
	//
	// v2 changed it twice over. The proofs are computed under a stretched key
	// rather than the digits (see stretchedKey), and they now cover the
	// construction the two ends agreed to seal with (see handshakeProof).
	proofDomain = "leavesafe/v2"

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

// handshakeProof returns the proof for one role: an HMAC-SHA256 under the
// stretched key of
//
//	leavesafe/v2|<role>|<server nonce>|<client nonce>|<encrypt>
//
// hex-encoded.
//
// Both nonces go into both proofs, and that is the reason the client sends one
// at all. A proof bound only to the server's nonce could be replayed by
// whatever recorded it onto any connection that happened to be challenged with
// the same number; a proof bound only to the client's could be replayed back at
// the app by an impostor that had watched a genuine pairing. With both in the
// input, neither half means anything outside the one connection whose two
// nonces produced it.
//
// # Why the construction is in here
//
// encrypt is the name the two ends agreed to seal the rest of the conversation
// with — the client asks for it and the acceptance names it back. It used to
// sit outside every signature, which made it the one field on the exchange a
// machine on the path could edit and be believed about. Strip it from the auth
// message and the laptop reads an app that cannot seal; strip it from the
// acceptance and the phone reads a laptop that cannot. Both proofs still
// checked out, both ends still believed each other, and the conversation
// carried on in the clear for whoever had done the stripping to read, inject a
// `disarm` into, and quietly drop an alarm frame from.
//
// With it inside both signatures there is nothing to strip: each end signs what
// it asked for and what it granted, and an edited field is a proof that does not
// hold. Only one construction exists today and sealing is required, so a
// stripped field is already refused a line earlier — this is what keeps that
// true on the day there are two.
//
// key is the stretched key, not the digits. See stretchedKey for why.
func handshakeProof(key []byte, role, serverNonce, clientNonce, encrypt string) string {
	mac := hmac.New(sha256.New, key)
	// Written in parts rather than assembled with Sprintf so the exact bytes
	// being signed are visible here, since they are the contract with the app.
	mac.Write([]byte(proofDomain))
	mac.Write([]byte("|"))
	mac.Write([]byte(role))
	mac.Write([]byte("|"))
	mac.Write([]byte(serverNonce))
	mac.Write([]byte("|"))
	mac.Write([]byte(clientNonce))
	mac.Write([]byte("|"))
	mac.Write([]byte(encrypt))
	return hex.EncodeToString(mac.Sum(nil))
}

// proofHolds reports whether offered is the proof for this role over these
// nonces and this construction.
//
// hmac.Equal rather than ==, because a string comparison stops at the first
// byte that differs and how long it took is measurable over a local socket.
// That turns guessing a proof from an exhaustive search into a byte-at-a-time
// one, which is a difference of many orders of magnitude.
func proofHolds(key []byte, role, serverNonce, clientNonce, encrypt, offered string) bool {
	want := handshakeProof(key, role, serverNonce, clientNonce, encrypt)
	return hmac.Equal([]byte(want), []byte(offered))
}

// How the sixteen digits become the key the proofs and the session are really
// computed under.
const (
	// saltDomain is what this machine's own salt is prefixed with, so that the
	// same random bytes could never mean the same thing to some other
	// derivation this program might one day grow.
	//
	// The salt itself is per key: minted with it, replaced with it, and sent in
	// the greeting — see auth.Manager.PairingSalt. It is not a secret and does
	// not need to be. What it buys is that the stretch is specific to one
	// machine, so a table built against one installation is worth nothing
	// against the next, and a rotation throws away whatever was built against
	// the key before it.
	//
	// It arrives before anything is computed, which is why it is in the
	// greeting rather than the acceptance: both ends have to stretch under the
	// same salt to produce proofs the other can check.
	saltDomain = "leavesafe/v2 lan pairing key|"

	// Argon2id at thirty-two mebibytes and three passes. Roughly a fifth of a
	// second on a laptop and under a second on a phone, paid once per key
	// rather than once per connection.
	//
	// The number it has to move is this: a pairing key is sixteen digits with a
	// Luhn check on the end, so there are 10^15 of them — fifty bits. Anything
	// that can watch one pairing on a café network records both nonces and both
	// proofs, and can then work through those 10^15 offline, at home, for as
	// long as it likes. Under a bare HMAC that is one hash per guess: a few
	// graphics cards do ten billion a second, and fifty bits falls in about a
	// day and a half. The key that opens somebody's laptop, for a day and a
	// half of somebody else's electricity.
	//
	// Argon2id is what makes that guess expensive rather than the key long. It
	// is memory-hard, so the thirty-two mebibytes each guess needs is what
	// bounds how many can run at once on the hardware that does the grinding —
	// a card with twenty-four gigabytes holds a few hundred, not a few hundred
	// thousand. At even ten thousand guesses a second, which is generous to the
	// attacker, 10^15 is three thousand years.
	//
	// The alternative was a longer key, and it is a worse one: these digits are
	// read off a screen and typed into a phone, and the product is worth
	// nothing if that is unpleasant. Making each guess cost something costs the
	// owner a fifth of a second, once.
	argonTime    = 3
	argonMemory  = 32 * 1024 // KiB
	argonThreads = 1
	argonKeyLen  = 32
)

// rememberedKeys is how many stretched keys are kept at once.
//
// A daemon holds one pairing key, so one would do for production and the rest
// of the room is for a rotation — the key before it is worth keeping for the
// moment it takes every phone to notice. It also means a process that builds
// several hubs, which is every run of this package's tests, derives once per
// key rather than once per hub.
const rememberedKeys = 8

// stretched is what has already been derived. Package-wide rather than a field
// on the Hub, because the answer depends on nothing but the key: two hubs
// holding the same key hold the same stretched key, and deriving it twice is
// half a second spent proving that.
//
// The keys in it are the pairing keys this process already holds in memory, so
// keeping them costs no secrecy that was not already spent.
var stretched = struct {
	mu   sync.Mutex
	seen map[string][]byte
	// order is the entries in the order they arrived, so the oldest goes when
	// the table is full.
	order []string
}{seen: make(map[string][]byte)}

// stretchedKey returns the key everything else is computed under: the proofs
// above, and the session keys in session.go.
//
// Cached, because Argon2 is deliberately slow by design and the answer only
// changes when the pairing key does. A phone reconnects every time its screen
// unlocks, and none of those should pay for it.
//
// The empty key is the one refusedKey uses to mean "this client is not entitled
// to be judged against anything", and it is stretched like any other rather
// than special-cased: it produces a key nothing can match, which is precisely
// what refusing means here.
func stretchedKey(key, salt string) []byte {
	// The pair, because a key stretched under two salts is two keys and the
	// table has to tell them apart. The separator cannot appear in either: a
	// pairing key is digits and a salt is hex.
	at := key + "|" + salt

	stretched.mu.Lock()
	defer stretched.mu.Unlock()

	if got, ok := stretched.seen[at]; ok {
		return got
	}

	got := argon2.IDKey([]byte(key), []byte(saltDomain+salt),
		argonTime, argonMemory, argonThreads, argonKeyLen)

	if len(stretched.order) >= rememberedKeys {
		delete(stretched.seen, stretched.order[0])
		stretched.order = stretched.order[1:]
	}
	stretched.seen[at] = got
	stretched.order = append(stretched.order, at)
	return got
}
