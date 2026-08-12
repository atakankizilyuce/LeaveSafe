package push

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

// keyFileMode keeps the signing key readable by its owner and nobody else,
// which is the same rule the pairing key is kept under and for the same reason:
// anything that can read it can send this phone an alarm.
const keyFileMode os.FileMode = 0o600

// tokenLifetime is how long a signed token stays good for. The specification
// allows twenty-four hours; half of that is long enough that clock skew between
// this laptop and a push service is never the reason an alarm did not arrive,
// and short enough that a token seen on the wire is not useful for long.
const tokenLifetime = 12 * time.Hour

// Identity is who this laptop says it is when it hands a message to a push
// service.
//
// It is one key pair, kept for the life of the installation. The public half
// travels to the phone at subscribe time and is written into the subscription
// by the browser, which then refuses any message not signed by the matching
// private half. That is what stops somebody who has learned a phone's endpoint
// — a URL, not a secret — from sending it alarms of their own.
type Identity struct {
	key *ecdsa.PrivateKey
}

// LoadOrCreateIdentity reads the signing key from path, or makes one and writes
// it there.
//
// Losing it is not fatal but is not free either: every phone subscribed under
// the old key stops accepting pushes and has to subscribe again, which it does
// the next time it is on the same network as the laptop.
func LoadOrCreateIdentity(path string) (*Identity, error) {
	stored, err := os.ReadFile(path) //nolint:gosec // a path this program chose
	if err == nil {
		key, decodeErr := decodeIdentity(strings.TrimSpace(string(stored)))
		if decodeErr == nil {
			return &Identity{key: key}, nil
		}
		// A key file that cannot be read is replaced rather than refused. The
		// alternative is a laptop that will not send alarms until somebody
		// deletes a file, and nobody is at the laptop when that matters.
		//
		// Said out loud, though. Replacing it silently is how a key that never
		// round-tripped looked exactly like a key that was working: every start
		// quietly made a new one, and every phone subscribed under the old one
		// stopped listening without anybody being told.
		log.Warnf("The push signing key could not be read (%v) — generating a new one. "+
			"Phones already subscribed will subscribe again the next time they are on this network.",
			decodeErr)
		return newIdentity(path)
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read the push signing key: %w", err)
	}
	return newIdentity(path)
}

func newIdentity(path string) (*Identity, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate a push signing key: %w", err)
	}

	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("encode the push signing key: %w", err)
	}

	encoded := base64.RawURLEncoding.EncodeToString(der)
	if err := os.WriteFile(path, []byte(encoded+"\n"), keyFileMode); err != nil {
		return nil, fmt.Errorf("write the push signing key: %w", err)
	}
	// WriteFile applies the mode only when it creates the file, so a key
	// restored from a backup keeps whatever permissions it arrived with.
	if err := os.Chmod(path, keyFileMode); err != nil {
		return nil, fmt.Errorf("restrict the push signing key: %w", err)
	}
	return &Identity{key: key}, nil
}

// decodeIdentity reads back what newIdentity wrote: the key in the standard
// PKCS#8 encoding, so nothing here has to reconstruct a point by hand.
func decodeIdentity(encoded string) (*ecdsa.PrivateKey, error) {
	der, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	parsed, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, err
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("the stored key is a %T, want an ECDSA key", parsed)
	}
	if key.Curve != elliptic.P256() {
		return nil, fmt.Errorf("the stored key is not on P-256")
	}
	return key, nil
}

// PublicKey is what the phone is told to subscribe with: base64url of the
// uncompressed point, which is the only form the browsers accept for an
// application server key. It is not a secret and is meant to be handed out.
func (id *Identity) PublicKey() string {
	return base64.RawURLEncoding.EncodeToString(id.publicBytes())
}

// publicBytes is the uncompressed sixty-five octet point.
//
// Taken through crypto/ecdh rather than elliptic.Marshal, which is deprecated —
// the two produce the same octets, and this one will still be here in a release
// or two.
func (id *Identity) publicBytes() []byte {
	converted, err := id.key.ECDH()
	if err != nil {
		// Only possible for a key on a curve crypto/ecdh does not know, and
		// this one is always P-256.
		return nil
	}
	return converted.PublicKey().Bytes()
}

// authorization is the header that proves this message came from the holder of
// the key the phone subscribed under.
func (id *Identity) authorization(endpoint string, now time.Time) (string, error) {
	token, err := id.token(endpoint, now)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("vapid t=%s, k=%s", token, id.PublicKey()), nil
}

// token is the signed JWT of RFC 8292.
//
// The audience is the push service's origin and nothing more of the endpoint:
// the path identifies the phone, and putting it in a token that the service
// reads would tell it which subscription this is before it has read the
// request. Scheme and host are all it needs to know the token is for it.
func (id *Identity) token(endpoint string, now time.Time) (string, error) {
	audience, err := origin(endpoint)
	if err != nil {
		return "", err
	}

	header := b64json(map[string]string{"typ": "JWT", "alg": "ES256"})
	claims := b64json(map[string]any{
		"aud": audience,
		"exp": now.Add(tokenLifetime).Unix(),
		// A subject is required, and the specification asks for something a
		// push service operator could contact. This program has no address to
		// give, so it gives where it comes from.
		"sub": "https://github.com/atakankizilyuce/LeaveSafe",
	})
	signed := header + "." + claims

	digest := sha256.Sum256([]byte(signed))
	r, s, err := ecdsa.Sign(rand.Reader, id.key, digest[:])
	if err != nil {
		return "", fmt.Errorf("sign the push token: %w", err)
	}

	// Raw r and s, each padded to the curve's size. This is not ASN.1: a JWS
	// ES256 signature is the two integers concatenated, and a DER one is
	// rejected by every push service there is.
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])

	return signed + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func origin(endpoint string) (string, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrBadSubscription, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("%w: %q has no origin to address a token to", ErrBadSubscription, endpoint)
	}
	return parsed.Scheme + "://" + parsed.Host, nil
}

func b64json(v any) string {
	// Marshaling a map of strings and integers cannot fail, and there is
	// nothing useful to do at this point if it somehow did.
	encoded, _ := json.Marshal(v)
	return base64.RawURLEncoding.EncodeToString(encoded)
}
