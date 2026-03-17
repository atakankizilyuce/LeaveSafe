package auth

import "testing"

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
	token, _, err := mgr.Authenticate(mgr.RawPairingKey())
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
	token, _, err := mgr.Authenticate(mgr.PairingKey())
	if err != nil {
		t.Fatalf("auth with dashes failed: %v", err)
	}
	if token == "" {
		t.Error("expected non-empty token")
	}
}

func TestAuthenticate_InvalidKey(t *testing.T) {
	mgr, _ := NewManager()
	_, remaining, err := mgr.Authenticate("0000000000000000")
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
		mgr.Authenticate("0000000000000000")
	}
	_, _, err := mgr.Authenticate(mgr.RawPairingKey())
	if err == nil {
		t.Error("expected lockout error")
	}
}

func TestRemoveSession(t *testing.T) {
	mgr, _ := NewManager()
	token, _, _ := mgr.Authenticate(mgr.RawPairingKey())
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
	mgr.Authenticate(mgr.RawPairingKey())
	if mgr.SessionCount() != 1 {
		t.Error("session count should be 1 after auth")
	}
}

func TestMaxConnections(t *testing.T) {
	mgr, _ := NewManager()
	for i := 0; i < 3; i++ {
		_, _, err := mgr.Authenticate(mgr.RawPairingKey())
		if err != nil {
			t.Fatalf("auth %d failed: %v", i, err)
		}
	}
	_, _, err := mgr.Authenticate(mgr.RawPairingKey())
	if err == nil {
		t.Error("expected max connections error")
	}
}

func TestStripDashes(t *testing.T) {
	if got := stripDashes("1234-5678-9012-3456"); got != "1234567890123456" {
		t.Errorf("stripDashes = %q, want 1234567890123456", got)
	}
}
