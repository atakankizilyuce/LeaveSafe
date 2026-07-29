package auth

import (
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
	for _, encoded := range []string{"", "4271", "md5$aa$bb", "sha256$zz$zz", "sha256$only-two"} {
		if VerifyPinHash("4271", encoded) {
			t.Errorf("malformed encoding %q verified", encoded)
		}
	}
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
