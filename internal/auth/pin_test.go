package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

func TestHashPinRoundTrip(t *testing.T) {
	hash, err := HashPin("4271")
	if err != nil {
		t.Fatalf("HashPin: %v", err)
	}
	if strings.Contains(hash, "4271") {
		t.Error("hash contains the PIN in cleartext")
	}
	if !VerifyPinHash("4271", hash) {
		t.Error("correct PIN did not verify")
	}
	if VerifyPinHash("0000", hash) {
		t.Error("wrong PIN verified")
	}
}

func TestHashPinSalted(t *testing.T) {
	a, err := HashPin("4271")
	if err != nil {
		t.Fatalf("HashPin: %v", err)
	}
	b, err := HashPin("4271")
	if err != nil {
		t.Fatalf("HashPin: %v", err)
	}
	if a == b {
		t.Error("two hashes of the same PIN are identical — salt is not doing its job")
	}
}

func TestVerifyPinHashMalformed(t *testing.T) {
	for _, encoded := range []string{
		"",
		"4271",
		"md5$aa$bb",
		"sha256$zz$zz",
		"sha256$only-two",
		"scrypt$16384$8$1$aa", // too few fields
		"scrypt$notanumber$8$1$aa$bb",
		"scrypt$16384$8$1$zz$bb", // salt is not hex
		"scrypt$16384$8$1$aa$zz", // digest is not hex
		"scrypt$16384$8$1$aa$",   // empty digest
	} {
		if VerifyPinHash("4271", encoded) {
			t.Errorf("malformed encoding %q verified", encoded)
		}
	}
}

func TestHashPinUsesScrypt(t *testing.T) {
	hash, err := HashPin("4271")
	if err != nil {
		t.Fatalf("HashPin: %v", err)
	}
	if !strings.HasPrefix(hash, "scrypt$") {
		t.Errorf("hash %q does not use scrypt", hash)
	}
	if NeedsRehash(hash) {
		t.Error("a freshly written hash was reported as needing a rehash")
	}
}

// Refusing the old encoding on upgrade would lock every existing user out of
// their own disarm PIN, with no way to set a new one without first disarming.
func TestVerifyAcceptsLegacySHA256Hashes(t *testing.T) {
	// Produced by the pre-scrypt implementation: sha256(salt || pin).
	const legacy = "sha256$" +
		"000102030405060708090a0b0c0d0e0f" + "$" +
		"1f2a1d4bd4b1d1bd8c1cbe6b71ad2ee27b47ee1e04b3b2b47e0dd0b1a54a2ff5"

	// The digest above is a placeholder; compute the real one the same way the
	// old code did so the test asserts on behaviour rather than a copied string.
	real := legacySHA256Hash(t, "000102030405060708090a0b0c0d0e0f", "4271")

	if VerifyPinHash("4271", legacy) {
		t.Error("a legacy hash with the wrong digest verified")
	}
	if !VerifyPinHash("4271", real) {
		t.Error("a correct legacy hash did not verify")
	}
	if VerifyPinHash("0000", real) {
		t.Error("a wrong PIN verified against a legacy hash")
	}
}

func TestNeedsRehash(t *testing.T) {
	real := legacySHA256Hash(t, "000102030405060708090a0b0c0d0e0f", "4271")
	if !NeedsRehash(real) {
		t.Error("a legacy SHA-256 hash was not reported as needing a rehash")
	}
	if !NeedsRehash("") {
		t.Error("an empty hash was not reported as needing a rehash")
	}
	if !NeedsRehash("garbage") {
		t.Error("an unparseable hash was not reported as needing a rehash")
	}
	// Weaker parameters than the current preset must be upgraded too.
	if !NeedsRehash("scrypt$1024$8$1$aabb$ccdd") {
		t.Error("a hash with a lower cost was not reported as needing a rehash")
	}
}

// A hand-edited file must not be able to talk the verifier into allocating
// gigabytes or spinning forever.
func TestVerifyRejectsAbsurdScryptParameters(t *testing.T) {
	for _, encoded := range []string{
		"scrypt$1073741824$8$1$aabb$ccdd", // N far past anything sane
		"scrypt$16384$1000$1$aabb$ccdd",   // r past the bound
		"scrypt$16384$8$999$aabb$ccdd",    // p past the bound
		"scrypt$12345$8$1$aabb$ccdd",      // N is not a power of two
		"scrypt$1$8$1$aabb$ccdd",          // N below the minimum
	} {
		if VerifyPinHash("4271", encoded) {
			t.Errorf("hash with absurd parameters %q verified", encoded)
		}
	}
}

// legacySHA256Hash builds a hash in the pre-scrypt encoding.
func legacySHA256Hash(t *testing.T, saltHex, pin string) string {
	t.Helper()
	salt, err := hex.DecodeString(saltHex)
	if err != nil {
		t.Fatalf("decode salt: %v", err)
	}
	sum := sha256.Sum256(append(salt, []byte(pin)...))
	return "sha256$" + saltHex + "$" + hex.EncodeToString(sum[:])
}

func TestCheckPinLockout(t *testing.T) {
	m, err := NewManagerWithOptions(Options{MaxAttempts: 3, LockoutPeriod: time.Minute})
	if err != nil {
		t.Fatalf("NewManagerWithOptions: %v", err)
	}
	hash, err := HashPin("4271")
	if err != nil {
		t.Fatalf("HashPin: %v", err)
	}

	addr := "192.168.1.50:1234"
	for i := 0; i < 3; i++ {
		if err := m.CheckPin(addr, "0000", hash); err == nil {
			t.Fatalf("attempt %d: wrong PIN accepted", i+1)
		}
	}

	// The address is now locked out: even the right PIN must be refused.
	if err := m.CheckPin(addr, "4271", hash); err == nil {
		t.Error("correct PIN accepted while locked out")
	}

	// A different address is unaffected.
	if err := m.CheckPin("10.0.0.9:5555", "4271", hash); err != nil {
		t.Errorf("correct PIN from a clean address refused: %v", err)
	}
}

func TestCheckPinSuccessClearsFailures(t *testing.T) {
	m, err := NewManagerWithOptions(Options{MaxAttempts: 3, LockoutPeriod: time.Minute})
	if err != nil {
		t.Fatalf("NewManagerWithOptions: %v", err)
	}
	hash, err := HashPin("4271")
	if err != nil {
		t.Fatalf("HashPin: %v", err)
	}

	addr := "192.168.1.50:1234"
	if err := m.CheckPin(addr, "0000", hash); err == nil {
		t.Fatal("wrong PIN accepted")
	}
	if err := m.CheckPin(addr, "4271", hash); err != nil {
		t.Fatalf("correct PIN refused: %v", err)
	}
	if got := m.TrackedAddrs(); got != 0 {
		t.Errorf("TrackedAddrs after success = %d, want 0", got)
	}
}
