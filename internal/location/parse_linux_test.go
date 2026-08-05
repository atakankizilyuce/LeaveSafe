//go:build linux

package location

import (
	"os"
	"path/filepath"
	"testing"
)

func readFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(data)
}

func TestParseNmcliWiFi(t *testing.T) {
	aps := parseNmcliWiFi(readFixture(t, "linux/nmcli_wifi_terse.txt"))

	want := []AccessPoint{
		{BSSID: "a4:b1:c1:00:11:22", SignalDBM: -59, Channel: 6},
		{BSSID: "a4:b1:c1:00:11:23", SignalDBM: -73, Channel: 44},
		{BSSID: "3c:7a:8a:ff:0e:01", SignalDBM: -88, Channel: 11},
		{BSSID: "00:1a:2b:3c:4d:5e", SignalDBM: -50, Channel: 1},
	}

	if len(aps) != len(want) {
		t.Fatalf("parsed %d access points, want %d: %+v", len(aps), len(want), aps)
	}
	for i, w := range want {
		if aps[i] != w {
			t.Errorf("ap %d = %+v, want %+v", i, aps[i], w)
		}
	}
}

// TestSplitEscaped covers the reason this parser cannot just use strings.Split:
// nmcli's field separator is a colon, and so is every separator inside a MAC
// address.
func TestSplitEscaped(t *testing.T) {
	got := splitEscaped(`A4\:B1\:C1\:00\:11\:22:82:6`, ':')
	want := []string{"A4:B1:C1:00:11:22", "82", "6"}

	if len(got) != len(want) {
		t.Fatalf("split into %d fields %q, want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("field %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestParseNmcliWiFi_SkipsNonBSSIDLines(t *testing.T) {
	raw := "BSSID:SIGNAL:CHAN\n--\n\nA4\\:B1\\:C1\\:00\\:11\\:22:82:6\n"
	aps := parseNmcliWiFi(raw)
	if len(aps) != 1 {
		t.Fatalf("parsed %d access points, want 1: %+v", len(aps), aps)
	}
}

func TestParseIWScan(t *testing.T) {
	aps := parseIWScan(readFixture(t, "linux/iw_scan.txt"))

	want := []AccessPoint{
		{BSSID: "a4:b1:c1:00:11:22", SignalDBM: -59, Channel: 6},
		// The 5 GHz stanza carries no DS Parameter set, so channel stays zero
		// and is omitted from the geolocation request.
		{BSSID: "3c:7a:8a:ff:0e:01", SignalDBM: -45, Channel: 0},
		{BSSID: "00:1a:2b:3c:4d:5e", SignalDBM: -88, Channel: 1},
	}

	if len(aps) != len(want) {
		t.Fatalf("parsed %d access points, want %d: %+v", len(aps), len(want), aps)
	}
	for i, w := range want {
		if aps[i] != w {
			t.Errorf("ap %d = %+v, want %+v", i, aps[i], w)
		}
	}
}

// TestParseIWScan_AssociatedSuffix pins the one line shape that differs: the
// BSS the machine is connected to has " -- associated" appended.
func TestParseIWScan_AssociatedSuffix(t *testing.T) {
	aps := parseIWScan("BSS 3c:7a:8a:ff:0e:01(on wlan0) -- associated\n\tsignal: -45.00 dBm\n")
	if len(aps) != 1 {
		t.Fatalf("parsed %d access points, want 1", len(aps))
	}
	if aps[0].BSSID != "3c:7a:8a:ff:0e:01" {
		t.Errorf("BSSID = %q, want the address without the suffix", aps[0].BSSID)
	}
}

// A scan line whose address does not survive parsing is dropped rather than
// recorded. These addresses are sent to a geolocation service to be turned into
// a position, so a truncated line kept as an access point is a made-up network
// weighing in on where the laptop is.
func TestParseIWScan_DropsANetworkWithoutAUsableAddress(t *testing.T) {
	raw := "BSS not-an-address(on wlan0)\n\tsignal: -45.00 dBm\n" +
		"BSS 3c:7a:8a:ff:0e:01(on wlan0)\n\tsignal: -50.00 dBm\n"

	aps := parseIWScan(raw)

	if len(aps) != 1 {
		t.Fatalf("parsed %d access points, want only the one with a real address: %+v", len(aps), aps)
	}
	if aps[0].BSSID != "3c:7a:8a:ff:0e:01" {
		t.Errorf("BSSID = %q, want the network that did parse", aps[0].BSSID)
	}
	// The signal lines under the dropped network must not be attached to
	// anything either — least of all to the network that follows it.
	if aps[0].SignalDBM != -50 {
		t.Errorf("signal = %d dBm, want -50 from its own block", aps[0].SignalDBM)
	}
}

func TestLooksLikeBSSID(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"a4:b1:c1:00:11:22", true},
		{"A4:B1:C1:00:11:22", true},
		{"BSSID", false},
		{"a4:b1:c1:00:11", false}, // too few octets
		{"a4:b1:c1:00:11:22:33", false},
		{"a4:b1:c1:00:11:2z", false}, // not hex
		{"", false},
	}
	for _, tt := range tests {
		if got := looksLikeBSSID(tt.in); got != tt.want {
			t.Errorf("looksLikeBSSID(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestPercentToDBM(t *testing.T) {
	tests := []struct {
		pct  int
		want int
	}{
		{0, -100},
		{50, -75},
		{100, -50},
		{-5, -100},
		{140, -50},
	}
	for _, tt := range tests {
		if got := percentToDBM(tt.pct); got != tt.want {
			t.Errorf("percentToDBM(%d) = %d, want %d", tt.pct, got, tt.want)
		}
	}
}
