package push

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	cryptorand "crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A token has to be addressed to the service it is for, so an endpoint with no
// origin is refused before anything is signed.
func TestATokenCannotBeAddressedToNothing(t *testing.T) {
	id, err := LoadOrCreateIdentity(filepath.Join(t.TempDir(), "push-key"))
	if err != nil {
		t.Fatalf("identity: %v", err)
	}

	for _, endpoint := range []string{"", "/just/a/path", "://nope"} {
		if _, err := id.authorization(endpoint, time.Now()); !errors.Is(err, ErrBadSubscription) {
			t.Errorf("authorization(%q) = %v, want it refused", endpoint, err)
		}
	}
}

// ---- what a key file can turn out to be ------------------------------------

func writeKeyFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "push-key")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

// Every one of these is a file that parses far enough to be tempting and is not
// a key this program can sign with. Each is replaced rather than refused, and
// what matters is that none of them is used.
func TestAKeyFileThatIsNotOurKindIsReplaced(t *testing.T) {
	cases := map[string]string{
		"not base64":               "!!!! not base64 !!!!",
		"not a key at all":         base64.RawURLEncoding.EncodeToString([]byte("hello")),
		"a key of the wrong kind":  ed25519KeyFile(t),
		"a key on the wrong curve": p384KeyFile(t),
	}
	for name, contents := range cases {
		t.Run(name, func(t *testing.T) {
			path := writeKeyFile(t, contents)

			id, err := LoadOrCreateIdentity(path)

			if err != nil {
				t.Fatalf("LoadOrCreateIdentity: %v", err)
			}
			// Replaced: what is on disk now is a key this program can use.
			stored, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatalf("read back: %v", readErr)
			}
			if _, decodeErr := decodeIdentity(strings.TrimSpace(string(stored))); decodeErr != nil {
				t.Errorf("what was written back is still unusable: %v", decodeErr)
			}
			if id.PublicKey() == "" {
				t.Error("no usable key came back")
			}
		})
	}
}

// A perfectly valid PKCS#8 key that is not the kind this signs with.
func ed25519KeyFile(t *testing.T) string {
	t.Helper()
	_, key, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatalf("generate an Ed25519 key: %v", err)
	}
	return pkcs8(t, key)
}

// And one of the right kind on a curve VAPID does not use.
func p384KeyFile(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P384(), cryptorand.Reader)
	if err != nil {
		t.Fatalf("generate a P-384 key: %v", err)
	}
	return pkcs8(t, key)
}

func pkcs8(t *testing.T, key any) string {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(der)
}
