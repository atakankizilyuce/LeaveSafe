//go:build windows

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

func TestParseNetshBSSIDs(t *testing.T) {
	aps := parseNetshBSSIDs(readFixture(t, "windows/netsh_networks_bssid.txt"))

	want := []AccessPoint{
		{BSSID: "a4:b1:c1:00:11:22", SignalDBM: -59},
		{BSSID: "a4:b1:c1:00:11:23", SignalDBM: -73},
		{BSSID: "3c:7a:8a:ff:0e:01", SignalDBM: -88},
		{BSSID: "00:1a:2b:3c:4d:5e", SignalDBM: -50},
	}

	if len(aps) != len(want) {
		t.Fatalf("parsed %d access points, want %d: %+v", len(aps), len(want), aps)
	}
	for i, w := range want {
		if aps[i].BSSID != w.BSSID {
			t.Errorf("ap %d: BSSID = %q, want %q", i, aps[i].BSSID, w.BSSID)
		}
		if aps[i].SignalDBM != w.SignalDBM {
			t.Errorf("ap %d (%s): signal = %d dBm, want %d", i, w.BSSID, aps[i].SignalDBM, w.SignalDBM)
		}
	}
}

// TestParseNetshBSSIDs_UppercaseIsNormalised pins the lower-casing. The
// geolocation APIs document lower-case MAC addresses, and netsh has been seen
// printing them either way.
func TestParseNetshBSSIDs_UppercaseIsNormalised(t *testing.T) {
	aps := parseNetshBSSIDs("    BSSID 1 : 3C:7A:8A:FF:0E:01\n         Signal : 50%\n")
	if len(aps) != 1 {
		t.Fatalf("parsed %d access points, want 1", len(aps))
	}
	if aps[0].BSSID != "3c:7a:8a:ff:0e:01" {
		t.Errorf("BSSID = %q, want lower-case", aps[0].BSSID)
	}
}

// TestParseNetshBSSIDs_ServiceStopped uses output this machine really produced.
// A desktop with the WLAN AutoConfig service stopped prints a sentence instead
// of a report, and the parser must not invent radios from it.
func TestParseNetshBSSIDs_ServiceStopped(t *testing.T) {
	aps := parseNetshBSSIDs(readFixture(t, "windows/netsh_wlansvc_stopped.txt"))
	if len(aps) != 0 {
		t.Errorf("parsed %d access points from an error message, want 0: %+v", len(aps), aps)
	}
}

// TestParseNetshBSSIDs_RatesAreNotSignals guards the one real risk in matching
// values rather than localized labels: the rate lines carry bare numbers, and
// none of them may be mistaken for a signal reading.
func TestParseNetshBSSIDs_RatesAreNotSignals(t *testing.T) {
	raw := "    BSSID 1 : 00:11:22:33:44:55\n" +
		"         Basic rates (Mbps) : 1 2 5.5 11\n" +
		"         Signal             : 60%\n"

	aps := parseNetshBSSIDs(raw)
	if len(aps) != 1 {
		t.Fatalf("parsed %d access points, want 1", len(aps))
	}
	if got, want := aps[0].SignalDBM, percentToDBM(60); got != want {
		t.Errorf("signal = %d dBm, want %d", got, want)
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
		{-5, -100}, // clamped
		{140, -50}, // clamped
	}
	for _, tt := range tests {
		if got := percentToDBM(tt.pct); got != tt.want {
			t.Errorf("percentToDBM(%d) = %d, want %d", tt.pct, got, tt.want)
		}
	}
}
