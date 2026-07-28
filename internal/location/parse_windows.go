//go:build windows

package location

import (
	"regexp"
	"strconv"
	"strings"
)

// The functions here interpret what netsh reports. They are separated from the
// code that runs it so a format change is caught by a test against output
// Windows really produced. See testdata/PROVENANCE.md.

var (
	// bssidPattern matches a MAC address. netsh output is localized — the
	// labels are translated on a Turkish or German Windows — but a MAC address
	// is a MAC address, so matching the value rather than the label keeps this
	// working on every install.
	bssidPattern = regexp.MustCompile(`\b([0-9a-fA-F]{2}(?::[0-9a-fA-F]{2}){5})\b`)
	// percentPattern matches the signal quality. Signal is the only percentage
	// netsh prints in this report, so it needs no label either.
	percentPattern = regexp.MustCompile(`\b(\d{1,3})\s*%`)
)

// parseNetshBSSIDs reads `netsh wlan show networks mode=bssid`.
//
// The report is a flat, indented list: each BSSID line opens a radio and the
// lines under it describe it, until the next BSSID line or the next SSID block.
func parseNetshBSSIDs(raw string) []AccessPoint {
	var (
		aps     []AccessPoint
		current *AccessPoint
	)

	flush := func() {
		if current != nil {
			aps = append(aps, *current)
			current = nil
		}
	}

	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, "\r")

		if m := bssidPattern.FindStringSubmatch(line); m != nil {
			flush()
			current = &AccessPoint{BSSID: normalizeBSSID(m[1])}
			continue
		}

		if current == nil || current.SignalDBM != 0 {
			continue
		}
		if m := percentPattern.FindStringSubmatch(line); m != nil {
			if pct, err := strconv.Atoi(m[1]); err == nil {
				current.SignalDBM = percentToDBM(pct)
			}
		}
	}
	flush()

	return aps
}

// percentToDBM converts the signal quality percentage Windows reports into the
// dBm a geolocation service expects. The mapping Microsoft documents for
// wlanapi is linear over -100..-50 dBm, so 0% is -100 dBm and 100% is -50 dBm.
func percentToDBM(pct int) int {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	return pct/2 - 100
}

// normalizeBSSID lower-cases a MAC address, which is the form the geolocation
// APIs document.
func normalizeBSSID(mac string) string {
	return strings.ToLower(mac)
}
