package ws

import (
	"testing"

	"github.com/leavesafe/leavesafe/internal/config"
	"github.com/leavesafe/leavesafe/internal/monitor"
)

// armedHub returns a hub with a registered sensor, a config, and the machine
// armed with that sensor watching — the state every test in this file is about.
func armedHub(t *testing.T) *Hub {
	t.Helper()
	hub := testHub(t)
	hub.sensorMgr.Register(monitor.NewPowerSensor())
	hub.sensorMgr.Enable("power")
	hub.SetConfig(config.Default())
	hub.Arm()
	t.Cleanup(hub.Disarm)
	return hub
}

// pairedClient returns a client holding a real session.
//
// Setting `authenticated` by hand is not enough and quietly ruins a test:
// handleMessage touches the session before it dispatches anything, so a client
// with no token is refused at the door and every assertion afterwards passes
// because the message was never handled at all.
func pairedClient(t *testing.T, hub *Hub) *Client {
	t.Helper()
	client := &Client{hub: hub, remoteAddr: "192.0.2.10:5000"}
	hub.handleAuth(client, ClientMessage{Type: MsgTypeAuth, Key: hub.authManager.RawPairingKey()})
	if !client.authenticated {
		t.Fatal("the test client did not pair")
	}
	return client
}

// The PIN guards disarming. Switching every sensor off reaches the same silence
// by another door, so the settings screen must refuse it while armed — exactly
// as the dedicated `configure` message already does.
func TestUpdateConfigWillNotDisableSensorsWhileArmed(t *testing.T) {
	hub := armedHub(t)

	payload := configToPayload(config.Default())
	payload.EnabledSensors = map[string]bool{"power": false}
	hub.handleUpdateConfig(ClientMessage{Type: MsgTypeUpdateConfig, Config: &payload}, &Client{hub: hub})

	if !hub.sensorMgr.IsEnabled("power") {
		t.Error("update_config switched a sensor off while the machine was armed")
	}
}

// The same message must still work when the machine is not armed, or the guard
// has broken the settings screen instead of protecting it.
func TestUpdateConfigStillChangesSensorsWhenDisarmed(t *testing.T) {
	hub := armedHub(t)
	hub.Disarm()

	payload := configToPayload(config.Default())
	payload.EnabledSensors = map[string]bool{"power": false}
	hub.handleUpdateConfig(ClientMessage{Type: MsgTypeUpdateConfig, Config: &payload}, &Client{hub: hub})

	if hub.sensorMgr.IsEnabled("power") {
		t.Error("update_config did not switch a sensor off while disarmed")
	}
}

// Saving an unrelated setting resends the sensor map unchanged. Refusing that
// would make the settings screen unusable while armed and would raise a warning
// about sensors nobody touched.
func TestUpdateConfigAcceptsAnUnchangedSensorMapWhileArmed(t *testing.T) {
	hub := armedHub(t)

	payload := configToPayload(config.Default())
	payload.EnabledSensors = map[string]bool{"power": true}
	payload.HeartbeatSeconds = 20
	hub.handleUpdateConfig(ClientMessage{Type: MsgTypeUpdateConfig, Config: &payload}, &Client{hub: hub})

	if !hub.sensorMgr.IsEnabled("power") {
		t.Error("resending the current sensor map switched a sensor off")
	}
}

// Pausing a sensor answers an alarm. Accepting it at any other time let a
// paired phone switch the sensors off one at a time while the machine was armed
// and nothing was sounding — disarming, without the PIN that disarming asks for.
func TestPauseIsIgnoredWhenNoAlarmIsSounding(t *testing.T) {
	hub := armedHub(t)
	client := pairedClient(t, hub)

	hub.handleMessage(client, ClientMessage{
		Type:     MsgTypeDismissAlarmPause,
		Sensor:   "power",
		Duration: 1_000_000,
	})

	if !hub.sensorMgr.IsEnabled("power") {
		t.Error("a pause with no alarm active switched a sensor off")
	}
}

func TestDisableIsIgnoredWhenNoAlarmIsSounding(t *testing.T) {
	hub := armedHub(t)
	client := pairedClient(t, hub)

	hub.handleMessage(client, ClientMessage{Type: MsgTypeDismissAlarmDisable, Sensor: "power"})

	if !hub.sensorMgr.IsEnabled("power") {
		t.Error("a disable with no alarm active switched a sensor off")
	}
}

// With an alarm sounding, the sensor that fired is the one that gets paused —
// and it is the hub's record of which one that was, not the name the message
// carried, that decides.
func TestPauseActsOnTheSensorThatActuallyFired(t *testing.T) {
	hub := armedHub(t)
	hub.sensorMgr.Register(monitor.NewLidSensor())
	hub.sensorMgr.Enable("lid")
	client := pairedClient(t, hub)

	hub.mu.Lock()
	hub.alarmActive = true
	hub.alarmSensor = "power"
	hub.mu.Unlock()

	// The message names a different sensor than the one that fired.
	hub.handleMessage(client, ClientMessage{Type: MsgTypeDismissAlarmDisable, Sensor: "lid"})

	if hub.sensorMgr.IsEnabled("power") {
		t.Error("the sensor that fired was not the one disabled")
	}
	if !hub.sensorMgr.IsEnabled("lid") {
		t.Error("the sensor named in the message was disabled instead")
	}
}

// A pause arrives over the wire, so its length is not the phone's to choose
// without limit. An unbounded one would leave a sensor off for the rest of the
// armed session while the panel still showed the machine as watched.
func TestPauseLengthIsClamped(t *testing.T) {
	cases := []struct {
		name string
		in   int
		want int
	}{
		{"a year", 31_536_000, maxPauseSeconds},
		{"zero means the default", 0, defaultPauseSeconds},
		{"negative means the default", -5, defaultPauseSeconds},
		{"an ordinary value is kept", 10, 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := clampPauseSeconds(tc.in); got != tc.want {
				t.Errorf("clampPauseSeconds(%d) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}
