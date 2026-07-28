package auth

import (
	"fmt"
	"testing"
	"time"
)

// testAddr is a stand-in for a single remote peer in tests that do not care
// about which address the attempt came from.
const testAddr = "192.0.2.1:51000"

func TestNewManager(t *testing.T) {
	mgr, err := NewManager()
	if err != nil {
		t.Fatal(err)
	}
	key := mgr.PairingKey()
	if len(key) != 19 { // XXXX-XXXX-XXXX-XXXX = 19 chars
		t.Errorf("formatted key length = %d, want 19", len(key))
	}
	raw := mgr.RawPairingKey()
	if len(raw) != 16 {
		t.Errorf("raw key length = %d, want 16", len(raw))
	}
}

func TestAuthenticate_Success(t *testing.T) {
	mgr, _ := NewManager()
	token, _, err := mgr.Authenticate(testAddr, mgr.RawPairingKey())
	if err != nil {
		t.Fatalf("auth failed: %v", err)
	}
	if token == "" {
		t.Error("expected non-empty token")
	}
	if !mgr.ValidateSession(token) {
		t.Error("session should be valid")
	}
}

func TestAuthenticate_WithDashes(t *testing.T) {
	mgr, _ := NewManager()
	token, _, err := mgr.Authenticate(testAddr, mgr.PairingKey())
	if err != nil {
		t.Fatalf("auth with dashes failed: %v", err)
	}
	if token == "" {
		t.Error("expected non-empty token")
	}
}

func TestAuthenticate_InvalidKey(t *testing.T) {
	mgr, _ := NewManager()
	_, remaining, err := mgr.Authenticate(testAddr, "0000000000000000")
	if err == nil {
		t.Error("expected error for invalid key")
	}
	if remaining != 4 {
		t.Errorf("remaining = %d, want 4", remaining)
	}
}

func TestAuthenticate_Lockout(t *testing.T) {
	mgr, _ := NewManager()
	for i := 0; i < 5; i++ {
		mgr.Authenticate(testAddr, "0000000000000000")
	}
	_, _, err := mgr.Authenticate(testAddr, mgr.RawPairingKey())
	if err == nil {
		t.Error("expected lockout error")
	}
}

// TestLockout_IsPerAddress is the regression test for the denial of service
// that a global attempt counter creates. Once the port is reachable from the
// internet, a stranger burning through the allowance must not be able to stop
// the owner from pairing.
func TestLockout_IsPerAddress(t *testing.T) {
	mgr, _ := NewManager()

	const attacker = "198.51.100.7:40000"
	const owner = "192.168.1.50:60000"

	for i := 0; i < 5; i++ {
		mgr.Authenticate(attacker, "0000000000000000")
	}

	if _, _, err := mgr.Authenticate(attacker, mgr.RawPairingKey()); err == nil {
		t.Error("attacker should be locked out after exhausting its attempts")
	}

	token, _, err := mgr.Authenticate(owner, mgr.RawPairingKey())
	if err != nil {
		t.Fatalf("owner locked out by another address's failures: %v", err)
	}
	if token == "" {
		t.Error("owner should have received a session token")
	}
}

// TestLockout_IgnoresSourcePort checks that reconnecting from a new ephemeral
// port does not hand the same peer a fresh allowance.
func TestLockout_IgnoresSourcePort(t *testing.T) {
	mgr, _ := NewManager()

	for i := 0; i < 4; i++ {
		if _, _, err := mgr.Authenticate(fmt.Sprintf("203.0.113.9:%d", 40000+i), "0000000000000000"); err == nil {
			t.Fatal("expected invalid key error")
		}
	}

	_, remaining, _ := mgr.Authenticate("203.0.113.9:49999", "0000000000000000")
	if remaining != 0 {
		t.Errorf("remaining = %d, want 0: a new source port must not reset the counter", remaining)
	}
	if _, _, err := mgr.Authenticate("203.0.113.9:50000", mgr.RawPairingKey()); err == nil {
		t.Error("expected the peer to be locked out regardless of source port")
	}
}

func TestLockout_Expires(t *testing.T) {
	mgr, _ := NewManagerWithOptions(Options{LockoutPeriod: 30 * time.Millisecond})

	for i := 0; i < 5; i++ {
		mgr.Authenticate(testAddr, "0000000000000000")
	}
	if _, _, err := mgr.Authenticate(testAddr, mgr.RawPairingKey()); err == nil {
		t.Fatal("expected lockout immediately after exhausting attempts")
	}

	time.Sleep(50 * time.Millisecond)

	if _, _, err := mgr.Authenticate(testAddr, mgr.RawPairingKey()); err != nil {
		t.Errorf("lockout should have expired: %v", err)
	}
}

func TestAuthenticate_SuccessClearsFailures(t *testing.T) {
	mgr, _ := NewManager()

	for i := 0; i < 3; i++ {
		mgr.Authenticate(testAddr, "0000000000000000")
	}
	token, _, err := mgr.Authenticate(testAddr, mgr.RawPairingKey())
	if err != nil {
		t.Fatalf("auth failed: %v", err)
	}
	mgr.RemoveSession(token)

	if _, remaining, _ := mgr.Authenticate(testAddr, "0000000000000000"); remaining != 4 {
		t.Errorf("remaining = %d, want 4: a successful pairing should clear failure history", remaining)
	}
}

// TestTrackedAddrs_Bounded checks that failures from many distinct addresses
// cannot grow the failure table without limit.
func TestTrackedAddrs_Bounded(t *testing.T) {
	const bound = 8
	mgr, _ := NewManagerWithOptions(Options{MaxTrackedAddrs: bound})

	for i := 0; i < bound*4; i++ {
		mgr.Authenticate(fmt.Sprintf("10.0.0.%d:1234", i), "0000000000000000")
	}

	if got := mgr.TrackedAddrs(); got > bound {
		t.Errorf("tracked addresses = %d, want at most %d", got, bound)
	}
}

func TestOptions_MaxAttemptsIsHonoured(t *testing.T) {
	mgr, _ := NewManagerWithOptions(Options{MaxAttempts: 2})

	if _, remaining, _ := mgr.Authenticate(testAddr, "0000000000000000"); remaining != 1 {
		t.Errorf("remaining after first failure = %d, want 1", remaining)
	}
	mgr.Authenticate(testAddr, "0000000000000000")

	if _, _, err := mgr.Authenticate(testAddr, mgr.RawPairingKey()); err == nil {
		t.Error("expected lockout after 2 failures when max_auth_attempts is 2")
	}
}

func TestOptions_MaxSessionsIsHonoured(t *testing.T) {
	mgr, _ := NewManagerWithOptions(Options{MaxSessions: 1})

	if _, _, err := mgr.Authenticate(testAddr, mgr.RawPairingKey()); err != nil {
		t.Fatalf("first session rejected: %v", err)
	}
	if _, _, err := mgr.Authenticate("192.0.2.2:1234", mgr.RawPairingKey()); err == nil {
		t.Error("expected the second session to be refused when max_sessions is 1")
	}
}

func TestRemoveSession(t *testing.T) {
	mgr, _ := NewManager()
	token, _, _ := mgr.Authenticate(testAddr, mgr.RawPairingKey())
	mgr.RemoveSession(token)
	if mgr.ValidateSession(token) {
		t.Error("session should be invalid after removal")
	}
}

func TestSessionCount(t *testing.T) {
	mgr, _ := NewManager()
	if mgr.SessionCount() != 0 {
		t.Error("initial session count should be 0")
	}
	mgr.Authenticate(testAddr, mgr.RawPairingKey())
	if mgr.SessionCount() != 1 {
		t.Error("session count should be 1 after auth")
	}
}

func TestMaxConnections(t *testing.T) {
	mgr, _ := NewManager()
	for i := 0; i < 3; i++ {
		_, _, err := mgr.Authenticate(testAddr, mgr.RawPairingKey())
		if err != nil {
			t.Fatalf("auth %d failed: %v", i, err)
		}
	}
	_, _, err := mgr.Authenticate(testAddr, mgr.RawPairingKey())
	if err == nil {
		t.Error("expected max connections error")
	}
}

func TestRegenerate_ClearsLockout(t *testing.T) {
	mgr, _ := NewManager()
	for i := 0; i < 5; i++ {
		mgr.Authenticate(testAddr, "0000000000000000")
	}
	if _, err := mgr.Regenerate(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := mgr.Authenticate(testAddr, mgr.RawPairingKey()); err != nil {
		t.Errorf("rotating the key should clear lockouts: %v", err)
	}
}

func TestNormalizeAddr(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"192.0.2.1:51000", "192.0.2.1"},
		{"[2001:db8::1]:443", "2001:db8::1"},
		{"192.0.2.1", "192.0.2.1"},
		{"", UnknownAddr},
		{UnknownAddr, UnknownAddr},
	}
	for _, tt := range tests {
		if got := normalizeAddr(tt.in); got != tt.want {
			t.Errorf("normalizeAddr(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestStripDashes(t *testing.T) {
	if got := stripDashes("1234-5678-9012-3456"); got != "1234567890123456" {
		t.Errorf("stripDashes = %q, want 1234567890123456", got)
	}
}
