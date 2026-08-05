//go:build realtrigger

// Package realtrigger fires the hardware changes a hosted runner genuinely
// permits, against the real running binary. Anything the platform cannot
// produce is skipped with its reason recorded, never faked.
package realtrigger

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/leavesafe/leavesafe/internal/ws"
	"github.com/leavesafe/leavesafe/test/harness"
)

var matrix = harness.NewMatrix()

func TestMain(m *testing.M) {
	code := m.Run()
	if err := matrix.WriteSummary(); err != nil {
		panic(err)
	}
	os.Exit(code)
}

// allSensors is every sensor the binary registers, so a test can say which one
// it is measuring and switch the rest off rather than leaving them to chance.
var allSensors = []string{"power", "lid", "usb", "screen", "network", "input"}

// onlySensor is the sensor map that leaves one sensor watching.
//
// Naming all of them matters: they are all on by default, and the first alarm
// on an armed machine suppresses every alert behind it. A runner whose own lid
// or charger state moved mid-test would swallow the very alert the test then
// waits for, and the failure would read as "the sensor did not fire".
func onlySensor(name string) map[string]bool {
	want := make(map[string]bool, len(allSensors))
	for _, s := range allSensors {
		want[s] = s == name
	}
	return want
}

// armWith pairs, leaves one sensor watching and arms the system. Sensors must
// be set before arming, because the hub refuses configure changes while armed.
func armWith(t *testing.T, sensor string) *harness.Phone {
	t.Helper()
	app := harness.Start(t, harness.Options{})
	phone := harness.Dial(t, app.Port())
	if reply := phone.Authenticate(app.Key()); reply.Type != ws.MsgTypeAuthOK {
		t.Fatalf("authenticate: %s", reply.Reason)
	}

	phone.Send(ws.ClientMessage{
		Type:    ws.MsgTypeConfigure,
		Sensors: onlySensor(sensor),
	})
	phone.Send(ws.ClientMessage{Type: ws.MsgTypeArm})

	// configure broadcasts its own status before arm does, so wait for the
	// armed one rather than whichever status arrives first.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		status := phone.Expect(ws.MsgTypeStatus, 10*time.Second)
		if status.Armed != nil && *status.Armed {
			return phone
		}
	}
	t.Fatal("system never reported itself armed")
	return nil
}

// expectSensorAlert waits for an alert naming the given sensor.
func expectSensorAlert(t *testing.T, phone *harness.Phone, sensor string, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		alert := phone.Expect(ws.MsgTypeAlert, within)
		if alert.Alert != nil && alert.Alert.Sensor == sensor {
			return
		}
	}
	t.Fatalf("no alert from the %s sensor within %s", sensor, within)
}

// TestRealTrigger_Network changes this machine's IP addresses for real and
// requires the armed system to notice.
func TestRealTrigger_Network(t *testing.T) {
	phone := armWith(t, "network")

	if err := triggerNetworkChange(t); err != nil {
		if errors.Is(err, errUnsupported) {
			matrix.Skipped("network", err.Error())
			t.Skipf("no real network trigger here: %v", err)
		}
		t.Fatalf("trigger network change: %v", err)
	}

	expectSensorAlert(t, phone, "network", 25*time.Second)
	matrix.Triggered("network")
}

// TestRealTrigger_Screen puts the display to sleep for real.
func TestRealTrigger_Screen(t *testing.T) {
	phone := armWith(t, "screen")

	if err := triggerScreenOff(t); err != nil {
		if errors.Is(err, errUnsupported) {
			matrix.Skipped("screen", err.Error())
			t.Skipf("no real screen trigger here: %v", err)
		}
		t.Fatalf("trigger screen off: %v", err)
	}

	expectSensorAlert(t, phone, "screen", 25*time.Second)
	matrix.Triggered("screen")
}

// TestRealTrigger_Input generates real keyboard or pointer activity.
func TestRealTrigger_Input(t *testing.T) {
	phone := armWith(t, "input")

	// The input sensor ignores activity for five seconds after arming, so a
	// trigger sent immediately would be swallowed by the grace period.
	time.Sleep(7 * time.Second)

	if err := triggerInputActivity(t); err != nil {
		if errors.Is(err, errUnsupported) {
			matrix.Skipped("input", err.Error())
			t.Skipf("no real input trigger here: %v", err)
		}
		t.Fatalf("trigger input activity: %v", err)
	}

	expectSensorAlert(t, phone, "input", 30*time.Second)
	matrix.Triggered("input")
}

// TestRealTrigger_UnavailableSensors records the sensors no hosted runner can
// produce, so the coverage matrix tells the whole truth rather than half of it.
func TestRealTrigger_UnavailableSensors(t *testing.T) {
	for sensor, reason := range unproducibleSensors() {
		matrix.Skipped(sensor, reason)
	}
	t.Skip("recorded in the coverage matrix; see the job summary")
}
