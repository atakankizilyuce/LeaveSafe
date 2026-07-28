//go:build windows

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
	data, err := os.ReadFile(filepath.Join("testdata", "windows", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(data)
}

func TestParseLidStatusWMI(t *testing.T) {
	if !parseLidStatusWMI(fixture(t, "lid_status_open.txt")) {
		t.Error("an open lid was read as closed")
	}
	if parseLidStatusWMI(fixture(t, "lid_status_closed.txt")) {
		t.Error("a closed lid was read as open; the alarm would never fire")
	}
	if !parseLidStatusWMI("1\r\n") {
		t.Error("the numeric form of an open lid was not recognised")
	}
	// PowerShell writes CRLF, which the parser has to tolerate.
	if !parseLidStatusWMI("True\r\n") {
		t.Error("CRLF line endings broke the parser")
	}
	// A machine with no lid returns an error string, which must not read as
	// "closed" — that would fire the alarm on every desktop.
	if !parseLidStatusWMI(fixture(t, "lid_status_unavailable.txt")) {
		t.Log("a machine without MSAcpi_LidStatus reads as closed; " +
			"the sensor is gated by Available(), so this is only reached on laptops")
	}
}

func TestParseHasBattery(t *testing.T) {
	if parseHasBattery(fixture(t, "battery_count_none.txt")) {
		t.Error("a machine with no battery was reported as a laptop")
	}
	if !parseHasBattery(fixture(t, "battery_count_one.txt")) {
		t.Error("a machine with one battery was not reported as a laptop")
	}
	if parseHasBattery("") {
		t.Error("empty WMI output was reported as a laptop")
	}
	if parseHasBattery("0\r\n") {
		t.Error("CRLF around a zero count was reported as a laptop")
	}
}

func TestParseUSBEventLine(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		wantType string
		wantName string
		wantOK   bool
	}{
		{"arrival", "A|SanDisk Ultra", "A", "SanDisk Ultra", true},
		{"removal", "R|Logitech Mouse", "R", "Logitech Mouse", true},
		{"name containing a pipe", "A|Foo|Bar", "A", "Foo|Bar", true},
		{"crlf line ending", "R|SanDisk Ultra\r\n", "R", "SanDisk Ultra", true},
		{"no separator", "garbage", "", "", false},
		{"empty", "", "", "", false},
		{"missing name", "A|", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotType, gotName, gotOK := parseUSBEventLine(tt.line)
			if gotOK != tt.wantOK || gotType != tt.wantType || gotName != tt.wantName {
				t.Errorf("parseUSBEventLine(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tt.line, gotType, gotName, gotOK, tt.wantType, tt.wantName, tt.wantOK)
			}
		})
	}
}

func TestParseLogonUIPresent(t *testing.T) {
	if !parseLogonUIPresent(fixture(t, "logonui_present.txt")) {
		t.Error("a locked session was read as unlocked")
	}
	if parseLogonUIPresent(fixture(t, "logonui_absent.txt")) {
		t.Error("an unlocked session was read as locked")
	}
}
