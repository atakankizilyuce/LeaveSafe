package ws

import (
	"testing"
	"time"

	"github.com/leavesafe/leavesafe/internal/config"
	"github.com/leavesafe/leavesafe/internal/remote"
)

// Turning remote access on from the phone has to actually turn it on, not
// record a wish and tell the user to restart.
func TestChangingRemoteAccessCallsTheToggle(t *testing.T) {
	h := testHub(t)
	var got []bool
	h.SetRemoteToggle(func(enable bool) { got = append(got, enable) })

	off := false
	h.SetConfig(&config.Config{RemoteAccess: &off, RemotePort: 9443})

	h.applyRemoteAccessChange(true)

	if len(got) != 1 || !got[0] {
		t.Errorf("toggle calls = %v, want [true]", got)
	}
}

// Saving the settings screen without touching the connection mode must not
// bounce the listener — every save sends the whole config back.
func TestAnUnchangedRemoteAccessDoesNotTouchTheListener(t *testing.T) {
	h := testHub(t)
	var calls int
	h.SetRemoteToggle(func(bool) { calls++ })

	on := true
	h.SetConfig(&config.Config{RemoteAccess: &on, RemotePort: 9443})

	h.applyRemoteAccessChange(true)

	if calls != 0 {
		t.Errorf("toggle called %d times for an unchanged setting, want 0", calls)
	}
}

// An embedding with no listener — a test, or a run where remote access was
// never wired up — must not panic when the setting changes.
func TestChangingRemoteAccessWithNoToggleInstalledIsSafe(t *testing.T) {
	h := testHub(t)
	off := false
	h.SetConfig(&config.Config{RemoteAccess: &off, RemotePort: 9443})

	h.applyRemoteAccessChange(true)
}

// The phone has to be able to see whether remote access is actually working,
// which is the whole reason State exists separately from the config value.
func TestTheStateReachesTheConfigPayload(t *testing.T) {
	h := testHub(t)
	h.SetRemoteState(remote.State{
		Enabled:    true,
		UPnP:       remote.UPnPFailed,
		ManualPort: 9443,
		Reason:     "Your router did not accept an automatic port mapping.",
	})

	on := true
	payload := h.configPayloadWithRemoteState(&config.Config{RemoteAccess: &on, RemotePort: 9443})

	if payload.RemoteState == nil {
		t.Fatal("RemoteState is nil, so the phone cannot tell working from broken")
	}
	if payload.RemoteState.ManualPort != 9443 {
		t.Errorf("ManualPort = %d, want 9443", payload.RemoteState.ManualPort)
	}
	if payload.RemoteState.UPnP != remote.UPnPFailed {
		t.Errorf("UPnP = %q, want %q", payload.RemoteState.UPnP, remote.UPnPFailed)
	}
	// The config value says what was asked for; the state says what happened.
	// Both have to survive the trip or the phone cannot show the difference.
	if !payload.RemoteAccess {
		t.Error("RemoteAccess is false, but the config asked for it")
	}
}

// waitForToggle gives the callback time to land: it runs on its own goroutine
// so that bringing a listener up — which asks a router and then the internet —
// does not hold up the reply to the phone that asked for it.
func waitForToggle(t *testing.T, calls chan bool) bool {
	t.Helper()
	select {
	case got := <-calls:
		return got
	case <-time.After(2 * time.Second):
		t.Fatal("the toggle was never called")
		return false
	}
}

// The whole point of the change, exercised through the message the phone
// actually sends rather than through the helper underneath it.
func TestTheSettingsScreenTurnsRemoteAccessOnForReal(t *testing.T) {
	hub := testHub(t)
	calls := make(chan bool, 4)
	hub.SetRemoteToggle(func(enable bool) { calls <- enable })

	off := false
	cfg := config.Default()
	cfg.RemoteAccess = &off
	hub.SetConfig(cfg)

	payload := configToPayload(cfg)
	payload.RemoteAccess = true
	hub.handleUpdateConfig(ClientMessage{Type: MsgTypeUpdateConfig, Config: &payload}, &Client{hub: hub})

	if got := waitForToggle(t, calls); !got {
		t.Error("the toggle was asked to turn remote access off, want on")
	}
}

// Saving an unrelated setting sends the whole config back. Bouncing the
// listener on every save would drop whoever is connected through it.
func TestSavingAnUnrelatedSettingLeavesTheListenerAlone(t *testing.T) {
	hub := testHub(t)
	calls := make(chan bool, 4)
	hub.SetRemoteToggle(func(enable bool) { calls <- enable })

	on := true
	cfg := config.Default()
	cfg.RemoteAccess = &on
	hub.SetConfig(cfg)

	payload := configToPayload(cfg)
	payload.RemoteAccess = true
	payload.HeartbeatSeconds = 25
	hub.handleUpdateConfig(ClientMessage{Type: MsgTypeUpdateConfig, Config: &payload}, &Client{hub: hub})

	select {
	case got := <-calls:
		t.Errorf("the listener was bounced (called with %v) for an unrelated change", got)
	case <-time.After(300 * time.Millisecond):
	}
}

// Resetting every setting puts remote access back to its default, which is off.
// Without applying that, "reset everything" would leave a port open to the
// internet while the config it just wrote asks for nothing of the sort.
func TestResettingEverythingClosesRemoteAccess(t *testing.T) {
	hub := testHub(t)
	calls := make(chan bool, 4)
	hub.SetRemoteToggle(func(enable bool) { calls <- enable })

	on := true
	cfg := config.Default()
	cfg.RemoteAccess = &on
	hub.SetConfig(cfg)

	hub.handleResetConfig(ClientMessage{Type: MsgTypeResetConfig}, &Client{hub: hub})

	if got := waitForToggle(t, calls); got {
		t.Error("the toggle was asked to turn remote access on during a reset")
	}
}
