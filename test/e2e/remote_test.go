//go:build e2e

package e2e_test

import (
	"strings"
	"testing"
	"time"

	"github.com/leavesafe/leavesafe/internal/ws"
	"github.com/leavesafe/leavesafe/test/harness"
)

// TestRemote_RefusesCleartextWhenTLSFails is the regression test for the most
// serious defect in the remote access path: when certificate setup failed, the
// app used to log a warning and carry on serving plain HTTP — while still
// opening a UPnP mapping and handing out a public URL. The pairing key, the
// only thing guarding the alarm, went over the internet in cleartext.
//
// The required behaviour is that remote access does not come up at all. The
// process stays alive and usable on the local network, because refusing to
// start would turn a confidentiality problem into an availability one.
func TestRemote_RefusesCleartextWhenTLSFails(t *testing.T) {
	app := harness.Start(t, harness.Options{
		RemoteAccess:  true,
		BreakTLSSetup: true,
	})

	out := app.Output()

	if !strings.Contains(out, "Remote access DISABLED") {
		t.Errorf("the app did not report that it disabled remote access.\n--- output ---\n%s", out)
	}

	// The giveaway for the old behaviour: it went on to publish the port. If
	// remote access is properly refused, the UPnP step is never reached, so no
	// UPnP log line — success or failure — should appear.
	//
	// The lines are named rather than searching for "UPnP" anywhere in the
	// output. The connection-mode question is asked on every start and its own
	// text mentions UPnP, so the bare word now appears in a run where nothing
	// was published at all.
	for _, line := range []string{"UPnP port mapping", "UPnP failed", "UPnP discovery", "UPnP lease"} {
		if strings.Contains(out, line) {
			t.Errorf("the port was published despite having no TLS (%q).\n--- output ---\n%s", line, out)
		}
	}

	// Availability is preserved: the local network path still pairs.
	phone := harness.Dial(t, app.Port())
	if reply := phone.Authenticate(app.Key()); reply.Type != ws.MsgTypeAuthOK {
		t.Fatalf("local pairing broke when remote access was refused: %q (%s)", reply.Type, reply.Reason)
	}
}

// TestRemote_TogglingKeepsTheLocalPhoneConnected is the claim the whole
// two-listener design exists to make good on.
//
// Remote access used to move the only listener to a new port and switch it to
// TLS, so turning it on meant every connected phone was dropped and had to
// re-pair from the QR code — including the phone that asked for the change.
// Now it is a second listener, and the local one is never touched.
//
// The socket is proved alive afterwards by using it, not by looking at it: a
// closed TCP connection can take a while to admit it, and a ping that comes
// back is the only answer that cannot be wrong.
func TestRemote_TogglingKeepsTheLocalPhoneConnected(t *testing.T) {
	_, phone := pairedPhone(t)

	phone.Send(ws.ClientMessage{Type: ws.MsgTypeGetConfig})
	current := phone.Expect(ws.MsgTypeConfigData, 10*time.Second)
	if current.Config == nil {
		t.Fatal("config_data carried no config")
	}

	for _, want := range []bool{true, false, true, false} {
		updated := *current.Config
		updated.RemoteAccess = want
		phone.Send(ws.ClientMessage{Type: ws.MsgTypeUpdateConfig, Config: &updated})

		// The same socket has to still answer. Turning remote access on asks a
		// router for a port mapping and the internet for an address, neither of
		// which is likely to succeed on a CI runner — and neither of which is
		// allowed to disturb this connection either way.
		phone.Send(ws.ClientMessage{Type: ws.MsgTypePing})
		phone.Expect(ws.MsgTypePong, 20*time.Second)

		phone.Send(ws.ClientMessage{Type: ws.MsgTypeGetConfig})
		live := phone.Expect(ws.MsgTypeConfigData, 10*time.Second)
		if live.Config.RemoteAccess != want {
			t.Fatalf("remote_access in the live config = %v, want %v", live.Config.RemoteAccess, want)
		}
		current = live
	}
}

// The phone has to be able to see whether remote access actually came up, not
// only that it asked for it. Without this the switch is one a user can flip
// with no way to learn it achieved nothing.
func TestRemote_TheConfigReportsWhatActuallyHappened(t *testing.T) {
	_, phone := pairedPhone(t)

	phone.Send(ws.ClientMessage{Type: ws.MsgTypeGetConfig})
	reply := phone.Expect(ws.MsgTypeConfigData, 10*time.Second)

	if reply.Config.RemoteState == nil {
		t.Fatal("config_data carried no remote_state, so the phone cannot tell working from broken")
	}
	if reply.Config.RemoteState.Enabled {
		t.Errorf("remote_state says enabled on a run started without remote access")
	}
}

// TestRemote_ConfiguredAuthAttemptsAreApplied proves the settings screen is
// telling the truth. max_auth_attempts was editable, persisted, and then
// ignored in favour of a hardcoded 5.
func TestRemote_ConfiguredAuthAttemptsAreApplied(t *testing.T) {
	app := harness.Start(t, harness.Options{MaxAuthAttempts: 2})
	phone := harness.Dial(t, app.Port())

	if reply := phone.Authenticate("0000-0000-0000-0000"); reply.RemainingAttempts != 1 {
		t.Errorf("remaining_attempts after one failure = %d, want 1 with max_auth_attempts=2",
			reply.RemainingAttempts)
	}

	// The second failure must engage the lockout, three short of the old default.
	phone.Authenticate("0000-0000-0000-0000")

	if reply := phone.Authenticate(app.Key()); reply.Type != ws.MsgTypeAuthFail {
		t.Errorf("the correct key was accepted after 2 failures (type %q); max_auth_attempts was ignored",
			reply.Type)
	}
}
