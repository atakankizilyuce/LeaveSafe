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
// answered that address, before any page had run. On the local network that is
// the pairing key in clear on a café network. A fragment never leaves the
// browser.
func TestPairingURLKeepsTheKeyOffTheWire(t *testing.T) {
	const key = "1234567890123456"

	raw := pairingURL("http://192.168.1.5:8080", key)

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("pairingURL produced something unparsable (%q): %v", raw, err)
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

// The fragment is read as key/value pairs by the page, so it has to parse as
// them — and carry the key under the name the page looks for.
func TestPairingURLNamesTheKeyInTheFragment(t *testing.T) {
	raw := pairingURL("http://192.168.1.5:8080", "1234567890123456")

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	values, err := url.ParseQuery(u.Fragment)
	if err != nil {
		t.Fatalf("the fragment is not parseable as key/value pairs: %v", err)
	}
	if got := values.Get("key"); got != "1234567890123456" {
		t.Errorf("key = %q, want the pairing key", got)
	}
}

// The address is kept whole, because it is what the phone dials. A base that
// lost its port or gained a path would point the phone at nothing.
func TestPairingURLKeepsTheAddressItWasGiven(t *testing.T) {
	const base = "http://192.168.1.5:8080"

	raw := pairingURL(base, "1234567890123456")

	if !strings.HasPrefix(raw, base+"/#") {
		t.Errorf("pairingURL = %q, want it to open with %q", raw, base+"/#")
	}
}
