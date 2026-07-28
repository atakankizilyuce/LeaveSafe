//go:build linux

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
	data, err := os.ReadFile(filepath.Join("testdata", "linux", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(data)
}

func TestParseACOnline(t *testing.T) {
	tests := []struct {
		name string
		file string
		want bool
	}{
		{"charger plugged in", "ac_online_1.txt", true},
		{"charger unplugged", "ac_online_0.txt", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := parseACOnline(fixture(t, tt.file)); got != tt.want {
				t.Errorf("parseACOnline() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseBatteryCharging(t *testing.T) {
	if !parseBatteryCharging(fixture(t, "battery_status_full.txt")) {
		t.Error("a full battery must count as being on mains")
	}
	if parseBatteryCharging(fixture(t, "battery_status_discharging.txt")) {
		t.Error("a discharging battery was read as on mains; the alarm would never fire")
	}
	if !parseBatteryCharging("Charging\n") {
		t.Error("a charging battery was read as not on mains")
	}
}

func TestParseLidOpen(t *testing.T) {
	if !parseLidOpen(fixture(t, "lid_state_open.txt")) {
		t.Error("an open lid was read as closed")
	}
	if parseLidOpen(fixture(t, "lid_state_closed.txt")) {
		t.Error("a closed lid was read as open; the alarm would never fire")
	}
}

func TestParseDPMSOn(t *testing.T) {
	if !parseDPMSOn(fixture(t, "xset_q_on.txt")) {
		t.Error("an active monitor was read as off")
	}
	if parseDPMSOn(fixture(t, "xset_q_dpms_off.txt")) {
		t.Error("a blanked monitor was read as on")
	}
	for _, state := range []string{"Monitor is Off", "Monitor is Standby", "Monitor is Suspend"} {
		if parseDPMSOn("DPMS is Enabled\n  " + state + "\n") {
			t.Errorf("%q was read as the monitor being on", state)
		}
	}
}

func TestParseUSBProductName(t *testing.T) {
	if got := parseUSBProductName("SanDisk Ultra\n"); got != "SanDisk Ultra" {
		t.Errorf("parseUSBProductName() = %q, want %q", got, "SanDisk Ultra")
	}
	if got := parseUSBProductName("  \n"); got != "" {
		t.Errorf("a blank product file yielded %q, want an empty name", got)
	}
}
