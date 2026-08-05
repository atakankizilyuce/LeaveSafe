//go:build sandbox && linux

package linuxvm

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/leavesafe/leavesafe/internal/monitor"
	"github.com/leavesafe/leavesafe/internal/ws"
	"github.com/leavesafe/leavesafe/test/harness"
)

var matrix = harness.NewLabeledMatrix("the Linux VM sandbox")

func TestMain(m *testing.M) {
	code := m.Run()
	if err := matrix.WriteSummary(); err != nil {
		panic(err)
	}
	os.Exit(code)
}

// skipUnavailable records a missing-hardware error in the coverage matrix and
// skips. Any other error is a real failure and must not be swallowed.
func skipUnavailable(t *testing.T, sensor string, err error) {
	t.Helper()
	if errors.Is(err, errNoHardware) {
		matrix.Skipped(sensor, err.Error())
		t.Skipf("no real %s hardware here: %v", sensor, err)
	}
	t.Fatalf("preparing the %s scenario failed: %v", sensor, err)
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
		Sensors: harness.OnlySensor(sensor),
	})
	phone.Send(ws.ClientMessage{Type: ws.MsgTypeArm})

	phone.WaitUntilArmed(true)
	return phone
}

// expectSensorAlert waits for an alert naming the given sensor and returns it.
func expectSensorAlert(t *testing.T, phone *harness.Phone, sensor string, within time.Duration) ws.ServerMessage {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		alert := phone.Expect(ws.MsgTypeAlert, within)
		if alert.Alert != nil && alert.Alert.Sensor == sensor {
			return alert
		}
	}
	t.Fatalf("no alert from the %s sensor within %s", sensor, within)
	return ws.ServerMessage{}
}

// TestSandbox_ChargerUnplugged is the product's headline scenario: the charger
// is genuinely removed at the kernel level and the phone must be told.
func TestSandbox_ChargerUnplugged(t *testing.T) {
	if err := loadModule("test_power"); err != nil {
		skipUnavailable(t, "power", err)
	}
	if err := setTestPowerAC(true); err != nil {
		skipUnavailable(t, "power", err)
	}

	phone := armWith(t, "power")

	// The sensor samples every two seconds; let it establish its baseline
	// before the state changes underneath it.
	time.Sleep(3 * time.Second)

	if err := setTestPowerAC(false); err != nil {
		t.Fatalf("unplug the charger: %v", err)
	}

	alert := expectSensorAlert(t, phone, "power", 25*time.Second)
	if alert.Alert.Level != string(monitor.AlertCritical) {
		t.Errorf("charger removal reported as %q, want critical — the alarm only "+
			"sounds for critical alerts", alert.Alert.Level)
	}
	matrix.Triggered("power")
}

// TestSandbox_KeyboardActivity proves real HID events wake the input sensor.
func TestSandbox_KeyboardActivity(t *testing.T) {
	if err := loadModule("uinput"); err != nil {
		skipUnavailable(t, "input", err)
	}
	keyboard, err := createVirtualKeyboard()
	if err != nil {
		skipUnavailable(t, "input", err)
	}

	phone := armWith(t, "input")

	// The input sensor ignores activity for five seconds after arming.
	time.Sleep(7 * time.Second)
	keyboard.Type(10)

	expectSensorAlert(t, phone, "input", 30*time.Second)
	matrix.Triggered("input")
}

// TestSandbox_USBAttached proves a real USB device appearing is noticed.
func TestSandbox_USBAttached(t *testing.T) {
	if err := loadModule("dummy_hcd"); err != nil {
		skipUnavailable(t, "usb", err)
	}

	phone := armWith(t, "usb")

	// The USB sensor snapshots on start and compares every three seconds.
	time.Sleep(4 * time.Second)

	if err := attachDummyUSB(); err != nil {
		skipUnavailable(t, "usb", err)
	}

	expectSensorAlert(t, phone, "usb", 30*time.Second)
	matrix.Triggered("usb")
}

// TestSandbox_ScreenBlanked proves a real DPMS transition is noticed.
func TestSandbox_ScreenBlanked(t *testing.T) {
	server, err := startXvfb()
	if err != nil {
		skipUnavailable(t, "screen", err)
	}
	defer server.Stop()

	phone := armWith(t, "screen")
	time.Sleep(3 * time.Second)

	if err := forceDPMSOff(); err != nil {
		t.Fatalf("blank the screen: %v", err)
	}

	expectSensorAlert(t, phone, "screen", 25*time.Second)
	matrix.Triggered("screen")
}

// TestSandbox_NetworkChanged proves a real IP change is noticed.
func TestSandbox_NetworkChanged(t *testing.T) {
	if err := requireSandboxVM(); err != nil {
		skipUnavailable(t, "network", err)
	}

	phone := armWith(t, "network")
	time.Sleep(4 * time.Second)

	if err := addLoopbackAddress(); err != nil {
		t.Fatalf("add a loopback address: %v", err)
	}

	expectSensorAlert(t, phone, "network", 25*time.Second)
	matrix.Triggered("network")
}

// TestSandbox_LidUnavailable records the one sensor even a VM cannot produce.
func TestSandbox_LidUnavailable(t *testing.T) {
	if _, err := os.Stat("/proc/acpi/button/lid"); err == nil {
		t.Fatal("this VM has an ACPI lid after all; write a real scenario for it " +
			"instead of leaving the sensor unproven")
	}
	matrix.Skipped("lid", "QEMU x86 emulates no ACPI lid button, so /proc/acpi/button/lid never exists")
	t.Skip("recorded in the coverage matrix; see docs/manual-verification.md")
}
