//go:build e2e

package e2e_test

import (
	"testing"
	"time"

	"github.com/leavesafe/leavesafe/internal/ws"
	"github.com/leavesafe/leavesafe/test/harness"
)

// TestPairing_CorrectKeyIssuesToken proves the documented pairing flow works
// against the real process.
func TestPairing_CorrectKeyIssuesToken(t *testing.T) {
	app := harness.Start(t, harness.Options{})
	phone := harness.Dial(t, app.Port())

	reply := phone.Authenticate(app.Key())
	if reply.Type != ws.MsgTypeAuthOK {
		t.Fatalf("auth reply type = %q, want %q (reason: %q)",
			reply.Type, ws.MsgTypeAuthOK, reply.Reason)
	}
	if reply.Token == "" {
		t.Error("auth_ok carried no session token")
	}
	if len(reply.Sensors) == 0 {
		t.Error("auth_ok carried no sensor list; the phone cannot render its UI")
	}
}

// TestPairing_WrongKeyIsRejected proves a bad key never yields a session.
func TestPairing_WrongKeyIsRejected(t *testing.T) {
	app := harness.Start(t, harness.Options{})
	phone := harness.Dial(t, app.Port())

	reply := phone.Authenticate("0000-0000-0000-0000")
	if reply.Type != ws.MsgTypeAuthFail {
		t.Fatalf("auth reply type = %q, want %q", reply.Type, ws.MsgTypeAuthFail)
	}
	if reply.Token != "" {
		t.Error("auth_fail carried a session token")
	}
}

// TestPairing_UnauthenticatedCommandsRejected proves an unpaired client cannot
// arm the system — the single most important access-control rule here.
func TestPairing_UnauthenticatedCommandsRejected(t *testing.T) {
	app := harness.Start(t, harness.Options{})
	phone := harness.Dial(t, app.Port())

	phone.Send(ws.ClientMessage{Type: ws.MsgTypeArm})
	reply := phone.Expect(ws.MsgTypeAuthFail, 5*time.Second)
	if reply.Reason != "not authenticated" {
		t.Errorf("reason = %q, want %q", reply.Reason, "not authenticated")
	}
}

// TestPairing_LockoutAfterFiveFailures proves brute force is stopped. This test
// gets its own process because the lockout lasts 60 seconds.
func TestPairing_LockoutAfterFiveFailures(t *testing.T) {
	app := harness.Start(t, harness.Options{})
	phone := harness.Dial(t, app.Port())

	for i := 1; i <= 4; i++ {
		reply := phone.Authenticate("0000-0000-0000-0000")
		if reply.Type != ws.MsgTypeAuthFail {
			t.Fatalf("attempt %d: type = %q, want auth_fail", i, reply.Type)
		}
		if want := 5 - i; reply.RemainingAttempts != want {
			t.Errorf("attempt %d: remaining_attempts = %d, want %d",
				i, reply.RemainingAttempts, want)
		}
	}

	// The fifth failure engages the lockout.
	phone.Authenticate("0000-0000-0000-0000")

	// The correct key must now be refused too.
	reply := phone.Authenticate(app.Key())
	if reply.Type != ws.MsgTypeAuthFail {
		t.Fatalf("after lockout the correct key was accepted (type %q)", reply.Type)
	}
}

// TestPairing_SessionCapEnforced proves the fourth concurrent phone is refused.
func TestPairing_SessionCapEnforced(t *testing.T) {
	app := harness.Start(t, harness.Options{})

	for i := 1; i <= 3; i++ {
		p := harness.Dial(t, app.Port())
		if reply := p.Authenticate(app.Key()); reply.Type != ws.MsgTypeAuthOK {
			t.Fatalf("client %d could not authenticate: %q", i, reply.Reason)
		}
	}

	fourth := harness.Dial(t, app.Port())
	reply := fourth.Authenticate(app.Key())
	if reply.Type != ws.MsgTypeAuthFail {
		t.Fatal("fourth client was accepted; the session cap is not enforced")
	}
}
