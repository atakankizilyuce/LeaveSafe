//go:build e2e

package e2e_test

import (
	"testing"
	"time"

	"github.com/leavesafe/leavesafe/internal/ws"
	"github.com/leavesafe/leavesafe/test/harness"
)

// armedPhone pairs, enables the network sensor and arms the system. Sensors
// have to be enabled before arming, because the hub refuses configure changes
// while armed.
func armedPhone(t *testing.T, opts harness.Options) (*harness.App, *harness.Phone) {
	t.Helper()
	app := harness.Start(t, opts)
	phone := harness.Dial(t, app.Port())

	if reply := phone.Authenticate(app.Key()); reply.Type != ws.MsgTypeAuthOK {
		t.Fatalf("authenticate: %s", reply.Reason)
	}

	phone.Send(ws.ClientMessage{
		Type:    ws.MsgTypeConfigure,
		Sensors: map[string]bool{"network": true},
	})
	phone.Send(ws.ClientMessage{Type: ws.MsgTypeArm})

	// configure broadcasts its own status before arm does, so wait for the
	// armed one rather than whichever status arrives first.
	waitForArmedState(t, phone, true)
	return app, phone
}

// waitForArmedState reads status broadcasts until the armed flag matches want.
func waitForArmedState(t *testing.T, phone *harness.Phone, want bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		status := phone.Expect(ws.MsgTypeStatus, 10*time.Second)
		if status.Armed != nil && *status.Armed == want {
			return
		}
	}
	t.Fatalf("system never reported armed=%v", want)
}

// TestAlarm_TriggerRaisesAlarmWhenArmed proves an armed system escalates a
// sensor event into an active alarm on the phone.
func TestAlarm_TriggerRaisesAlarmWhenArmed(t *testing.T) {
	_, phone := armedPhone(t, harness.Options{})

	phone.Send(ws.ClientMessage{Type: ws.MsgTypeTriggerSensor, Sensor: "network"})

	alert := phone.Expect(ws.MsgTypeAlert, 10*time.Second)
	if alert.Alert == nil || alert.Alert.Sensor != "network" {
		t.Fatalf("alert did not name the network sensor: %+v", alert.Alert)
	}

	active := phone.Expect(ws.MsgTypeAlarmActive, 10*time.Second)
	if active.Alert == nil || active.Alert.Message == "" {
		t.Error("alarm_active carried no message for the user to act on")
	}
}

// TestAlarm_NoAlarmWhenDisarmed proves a disarmed system stays quiet — a false
// alarm destroys trust as surely as a missed one.
func TestAlarm_NoAlarmWhenDisarmed(t *testing.T) {
	app := harness.Start(t, harness.Options{})
	phone := harness.Dial(t, app.Port())
	if reply := phone.Authenticate(app.Key()); reply.Type != ws.MsgTypeAuthOK {
		t.Fatalf("authenticate: %s", reply.Reason)
	}

	phone.Send(ws.ClientMessage{Type: ws.MsgTypeTriggerSensor, Sensor: "network"})
	phone.ExpectNot(ws.MsgTypeAlarmActive, 5*time.Second)
}

// TestAlarm_DisarmClearsArmedState proves disarming without PIN protection
// returns the system to rest.
func TestAlarm_DisarmClearsArmedState(t *testing.T) {
	_, phone := armedPhone(t, harness.Options{})

	phone.Send(ws.ClientMessage{Type: ws.MsgTypeDisarm})
	waitForArmedState(t, phone, false)
}

// TestAlarm_PinProtectedDisarm proves a thief holding the phone cannot silence
// the alarm without the PIN.
func TestAlarm_PinProtectedDisarm(t *testing.T) {
	_, phone := armedPhone(t, harness.Options{Pin: "4271"})

	// A bare disarm must be refused with a PIN challenge.
	phone.Send(ws.ClientMessage{Type: ws.MsgTypeDisarm})
	phone.Expect(ws.MsgTypePinRequired, 10*time.Second)

	// A wrong PIN must be rejected.
	phone.Send(ws.ClientMessage{Type: ws.MsgTypeDisarmPin, Pin: "0000"})
	fail := phone.Expect(ws.MsgTypeAuthFail, 10*time.Second)
	if fail.Reason != "invalid PIN" {
		t.Errorf("reason = %q, want %q", fail.Reason, "invalid PIN")
	}

	// The correct PIN must work.
	phone.Send(ws.ClientMessage{Type: ws.MsgTypeDisarmPin, Pin: "4271"})
	waitForArmedState(t, phone, false)
}
