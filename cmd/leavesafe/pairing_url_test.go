package main

import (
	"net/url"
	"strings"
	"testing"
)

// The pairing key must not be anywhere a browser would put on the wire.
//
// This is the whole reason the key lives in the fragment. As a query parameter
// it went out in the first request line the phone sent — to whatever server
// answered that address, before any page had run and before the certificate
// could be checked. On the default plain-HTTP path that is the pairing key in
// clear on a café network; over a stale port forward it is the key handed to a
// stranger. A fragment never leaves the browser.
func TestPairingURLKeepsTheKeyOffTheWire(t *testing.T) {
	const key = "1234567890123456"
	const fingerprint = "AA:BB:CC:DD"

	for _, base := range []string{"http://192.168.1.5:8080", "https://203.0.113.9:9443"} {
		raw := pairingURL(base, key, fingerprint)

		u, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("pairingURL produced something unparseable (%q): %v", raw, err)
		}

		// RequestURI is exactly what the browser writes into the request line.
		if strings.Contains(u.RequestURI(), key) {
			t.Errorf("the pairing key is in the request line: %q", u.RequestURI())
		}
		if u.RawQuery != "" {
			t.Errorf("the URL carries a query string, which is sent to the server: %q", u.RawQuery)
		}
		if !strings.Contains(u.Fragment, key) {
			t.Errorf("the fragment does not carry the key: %q", u.Fragment)
		}
	}
}

// The fingerprint has to survive the trip, or the phone has nothing to compare
// the server's greeting against. Colons are dropped to keep the QR code small
// and both ends compare on the bare hex.
func TestPairingURLCarriesTheFingerprintAsBareHex(t *testing.T) {
	raw := pairingURL("https://203.0.113.9:9443", "1234567890123456", "AA:BB:CC:DD")

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	values, err := url.ParseQuery(u.Fragment)
	if err != nil {
		t.Fatalf("the fragment is not parseable as key/value pairs: %v", err)
	}
	if got := values.Get("fp"); got != "aabbccdd" {
		t.Errorf("fp = %q, want %q", got, "aabbccdd")
	}
	if got := values.Get("key"); got != "1234567890123456" {
		t.Errorf("key = %q, want the pairing key", got)
	}
}

// On the plain-HTTP local path there is no certificate, so there is no
// fingerprint to name and claiming one would give the phone something
// meaningless to check against.
func TestPairingURLOmitsAnAbsentFingerprint(t *testing.T) {
	raw := pairingURL("http://192.168.1.5:8080", "1234567890123456", "")

	if strings.Contains(raw, "fp=") {
		t.Errorf("a URL with no certificate still names a fingerprint: %q", raw)
	}
}
