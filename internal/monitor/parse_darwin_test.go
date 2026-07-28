//go:build darwin

package monitor

import (
	"os"
	"path/filepath"
	"testing"
)

// fixture reads captured OS output. See testdata/PROVENANCE.md for where each
// file came from and which ones no runner could produce.
func fixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "darwin", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(data)
}

func TestParseOnACPower(t *testing.T) {
	if !parseOnACPower(fixture(t, "pmset_ac.txt")) {
		t.Error("a machine on AC power was read as running on battery")
	}
	if parseOnACPower(fixture(t, "pmset_battery.txt")) {
		t.Error("a machine on battery was read as on AC; the alarm would never fire")
	}
}

func TestParseClamshellOpen(t *testing.T) {
	if !parseClamshellOpen(fixture(t, "ioreg_clamshell_open.txt")) {
		t.Error("an open lid was read as closed")
	}
	if parseClamshellOpen(fixture(t, "ioreg_clamshell_closed.txt")) {
		t.Error("a closed lid was read as open; the alarm would never fire")
	}
	// A machine with no clamshell at all reports nothing. Treating that as
	// "closed" would fire the alarm on every desktop Mac.
	if parseClamshellOpen(fixture(t, "ioreg_clamshell_absent.txt")) {
		t.Error("a machine with no clamshell was reported as having an open lid")
	}
}

func TestParseDisplayOn(t *testing.T) {
	if !parseDisplayOn(fixture(t, "ioreg_display.txt")) {
		t.Error("an active display was read as off")
	}
	if parseDisplayOn(fixture(t, "ioreg_display_asleep.txt")) {
		t.Error("a sleeping display was read as on")
	}
}

func TestParseHIDIdleSeconds(t *testing.T) {
	got := parseHIDIdleSeconds(fixture(t, "ioreg_hid_idle.txt"))
	if got < 0 {
		t.Fatalf("HIDIdleTime was not found in real ioreg output (got %v)", got)
	}

	if got := parseHIDIdleSeconds(`"HIDIdleTime" = 2000000000`); got != 2 {
		t.Errorf("parseHIDIdleSeconds() = %v, want 2 (nanoseconds converted to seconds)", got)
	}
	if got := parseHIDIdleSeconds("no idle time here"); got != -1 {
		t.Errorf("missing HIDIdleTime = %v, want -1", got)
	}
}

func TestParseUSBDeviceNames(t *testing.T) {
	// The macOS runner has no USB devices, so the real capture is empty.
	if names := parseUSBDeviceNames(fixture(t, "system_profiler_usb_empty.txt")); len(names) != 0 {
		t.Errorf("empty profiler output yielded %v, want no devices", names)
	}

	const sample = `USB:

    USB 3.1 Bus:

      Host Controller Driver: AppleT8103USBXHCI

        SanDisk Ultra:

          Product ID: 0x5581
`
	names := parseUSBDeviceNames(sample)
	if len(names) != 1 || names[0] != "SanDisk Ultra" {
		t.Errorf("parseUSBDeviceNames() = %v, want [SanDisk Ultra]", names)
	}
}
