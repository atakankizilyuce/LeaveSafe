//go:build linux

package location

import (
	"strconv"
	"strings"
)

// The functions here interpret what nmcli and iw report. They are separated
// from the code that runs those commands so a format change is caught by a test
// against real output. See testdata/PROVENANCE.md.

// parseNmcliWiFi reads `nmcli -t -f BSSID,SIGNAL dev wifi list`.
//
// Terse mode separates fields with colons, which a MAC address is full of, so
// nmcli escapes the ones inside a value as `\:`. Splitting on a bare colon
// would shred every BSSID into six fields.
func parseNmcliWiFi(raw string) []AccessPoint {
	var aps []AccessPoint

	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		if line == "" {
			continue
		}

		fields := splitEscaped(line, ':')
		if len(fields) < 2 {
			continue
		}

		bssid := normalizeBSSID(strings.TrimSpace(fields[0]))
		if !looksLikeBSSID(bssid) {
			continue
		}

		ap := AccessPoint{BSSID: bssid}
		if pct, err := strconv.Atoi(strings.TrimSpace(fields[1])); err == nil {
			ap.SignalDBM = percentToDBM(pct)
		}
		if len(fields) >= 3 {
			if ch, err := strconv.Atoi(strings.TrimSpace(fields[2])); err == nil {
				ap.Channel = ch
			}
		}

		aps = append(aps, ap)
	}

	return aps
}

// splitEscaped splits on sep, treating a backslash as escaping the next byte
// and dropping the backslash from the result. This is nmcli's terse quoting.
func splitEscaped(s string, sep byte) []string {
	var (
		fields []string
		cur    strings.Builder
	)
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '\\' && i+1 < len(s):
			i++
			cur.WriteByte(s[i])
		case s[i] == sep:
			fields = append(fields, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(s[i])
		}
	}
	fields = append(fields, cur.String())
	return fields
}

// parseIWScan reads `iw dev <iface> scan`.
//
// Unlike nmcli this reports signal in dBm directly, which is what the
// geolocation API wants, so nothing is converted.
func parseIWScan(raw string) []AccessPoint {
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
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))

		if rest, ok := strings.CutPrefix(line, "BSS "); ok {
			flush()
			current = beginAP(rest)
			continue
		}

		if current != nil {
			readAPDetail(current, line)
		}
	}
	flush()

	return aps
}

// beginAP reads the header line that opens each network in the scan, and
// returns nil for one whose address does not survive parsing — a truncated line
// is better dropped than recorded as a network with a nonsense address.
//
//	BSS aa:bb:cc:dd:ee:ff(on wlan0) -- associated
func beginAP(rest string) *AccessPoint {
	mac := rest
	if idx := strings.IndexAny(mac, "( \t"); idx >= 0 {
		mac = mac[:idx]
	}
	mac = normalizeBSSID(strings.TrimSpace(mac))
	if !looksLikeBSSID(mac) {
		return nil
	}
	return &AccessPoint{BSSID: mac}
}

// readAPDetail fills in whichever of the indented lines under a network this
// one is. Anything else in the block is of no use to a geolocation lookup and
// is passed over.
func readAPDetail(ap *AccessPoint, line string) {
	if rest, ok := strings.CutPrefix(line, "signal:"); ok {
		// "signal: -65.00 dBm"
		value := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(rest), "dBm"))
		if dbm, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
			ap.SignalDBM = int(dbm)
		}
		return
	}

	if rest, ok := strings.CutPrefix(line, "DS Parameter set: channel"); ok {
		if ch, err := strconv.Atoi(strings.TrimSpace(rest)); err == nil {
			ap.Channel = ch
		}
	}
}

// looksLikeBSSID reports whether s is a colon-separated six-octet MAC address.
// nmcli prints headers and iw prints stanza text; neither should become a radio.
func looksLikeBSSID(s string) bool {
	octets := strings.Split(s, ":")
	if len(octets) != 6 {
		return false
	}
	for _, o := range octets {
		if len(o) != 2 {
			return false
		}
		for i := 0; i < len(o); i++ {
			c := o[i]
			isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
			if !isHex {
				return false
			}
		}
	}
	return true
}

// normalizeBSSID lower-cases a MAC address, which is the form the geolocation
// APIs document.
func normalizeBSSID(mac string) string {
	return strings.ToLower(mac)
}

// percentToDBM converts the 0-100 signal quality nmcli reports into dBm, over
// the same -100..-50 range NetworkManager derives it from.
func percentToDBM(pct int) int {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	return pct/2 - 100
}
