//go:build e2e

package e2e_test

import (
	"testing"
	"time"

	"github.com/leavesafe/leavesafe/internal/ws"
	"github.com/leavesafe/leavesafe/test/harness"
)

func pairedPhone(t *testing.T) (*harness.App, *harness.Phone) {
	t.Helper()
	app := harness.Start(t, harness.Options{})
	phone := harness.Dial(t, app.Port())
	if reply := phone.Authenticate(app.Key()); reply.Type != ws.MsgTypeAuthOK {
		t.Fatalf("authenticate: %s", reply.Reason)
	}
	return app, phone
}

// TestConfig_GetReturnsSettings proves the phone can read the live config.
func TestConfig_GetReturnsSettings(t *testing.T) {
	_, phone := pairedPhone(t)

	phone.Send(ws.ClientMessage{Type: ws.MsgTypeGetConfig})
	reply := phone.Expect(ws.MsgTypeConfigData, 10*time.Second)

	if reply.Config == nil {
		t.Fatal("config_data carried no config")
	}
	if reply.Config.MaxSessions != 3 {
		t.Errorf("max_sessions = %d, want 3", reply.Config.MaxSessions)
	}
}

// TestConfig_UpdatePersists proves a setting changed from the phone survives a
// restart — the feature is worthless if it forgets.
func TestConfig_UpdatePersists(t *testing.T) {
	app, phone := pairedPhone(t)

	phone.Send(ws.ClientMessage{Type: ws.MsgTypeGetConfig})
	current := phone.Expect(ws.MsgTypeConfigData, 10*time.Second)

	updated := *current.Config
	updated.InputThreshold = 7
	phone.Send(ws.ClientMessage{Type: ws.MsgTypeUpdateConfig, Config: &updated})

	// update_config broadcasts a status rather than replying, so read the value
	// back to confirm it landed. Messages on one connection are handled in
	// order, which makes this deterministic.
	phone.Send(ws.ClientMessage{Type: ws.MsgTypeGetConfig})
	live := phone.Expect(ws.MsgTypeConfigData, 10*time.Second)
	if live.Config.InputThreshold != 7 {
		t.Fatalf("input_threshold in the live config = %d, want 7", live.Config.InputThreshold)
	}

	// Restart against the same home directory and confirm the value stuck.
	if err := app.Stop(); err != nil {
		t.Fatalf("stop app: %v", err)
	}
	restarted := harness.StartIn(t, app.HomeDir(), harness.Options{})
	phone2 := harness.Dial(t, restarted.Port())
	if reply := phone2.Authenticate(restarted.Key()); reply.Type != ws.MsgTypeAuthOK {
		t.Fatalf("authenticate after restart: %s", reply.Reason)
	}

	phone2.Send(ws.ClientMessage{Type: ws.MsgTypeGetConfig})
	after := phone2.Expect(ws.MsgTypeConfigData, 10*time.Second)
	if after.Config.InputThreshold != 7 {
		t.Errorf("input_threshold after restart = %d, want 7", after.Config.InputThreshold)
	}
}

// TestConfig_ResetRestoresDefaults proves the reset escape hatch works.
func TestConfig_ResetRestoresDefaults(t *testing.T) {
	_, phone := pairedPhone(t)

	phone.Send(ws.ClientMessage{Type: ws.MsgTypeGetConfig})
	current := phone.Expect(ws.MsgTypeConfigData, 10*time.Second)

	updated := *current.Config
	updated.InputThreshold = 9
	phone.Send(ws.ClientMessage{Type: ws.MsgTypeUpdateConfig, Config: &updated})

	phone.Send(ws.ClientMessage{Type: ws.MsgTypeGetConfig})
	live := phone.Expect(ws.MsgTypeConfigData, 10*time.Second)
	if live.Config.InputThreshold != 9 {
		t.Fatalf("input_threshold in the live config = %d, want 9", live.Config.InputThreshold)
	}

	phone.Send(ws.ClientMessage{Type: ws.MsgTypeResetConfig})
	reset := phone.Expect(ws.MsgTypeConfigData, 10*time.Second)
	if reset.Config.InputThreshold == 9 {
		t.Error("reset_config left the modified input_threshold in place")
	}
}
