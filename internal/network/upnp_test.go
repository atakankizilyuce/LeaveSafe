package network

import "testing"

// The address the router claims is not a fact about the internet — it is a
// string from whatever answered an unauthenticated multicast on the local
// network. It ends up in the QR code the owner scans, with the pairing key in
// the same URL, so a claim that could not possibly be this machine's public
// address must not be passed on as one.
func TestTheRoutersClaimHasToLookLikeAPublicAddress(t *testing.T) {
	refused := map[string]string{
		"a hostname":                "evil.example",
		"empty":                     "",
		"a private address":         "192.168.1.50",
		"a private ten-net address": "10.4.4.4",
		// 172.16/12, not 100.64/10 — this is RFC 1918 private space. Carrier-grade
		// NAT is a separate question with its own answer in IsCarrierGradeNAT,
		// and publicAddr deliberately accepts those addresses.
		"another private one, higher up the block": "172.16.0.9",
		"loopback":            "127.0.0.1",
		"link-local":          "169.254.10.10",
		"unspecified":         "0.0.0.0",
		"multicast":           "224.0.0.1",
		"a private IPv6":      "fd00::1",
		"an IPv6 loopback":    "::1",
		"a URL":               "http://198.51.100.4/",
		"an address and port": "198.51.100.4:9443",
	}
	for what, claim := range refused {
		if got, err := publicAddr(claim); err == nil {
			t.Errorf("%s (%q) was accepted as a public address and became %q", what, claim, got)
		}
	}
}

// And it must still accept a real one, or turning remote access on would stop
// producing a URL the phone can reach.
func TestARealPublicAddressIsAccepted(t *testing.T) {
	accepted := map[string]string{
		"198.51.100.4":       "198.51.100.4",
		" 203.0.113.9 ":      "203.0.113.9",
		"2001:db8::1":        "2001:db8::1",
		"100.64.0.1":         "100.64.0.1", // shared address space: reachable or not, it is not ours to refuse
		"::ffff:198.51.10.4": "198.51.10.4",
	}
	for claim, want := range accepted {
		got, err := publicAddr(claim)
		if err != nil {
			t.Errorf("%q was refused: %v", claim, err)
			continue
		}
		if got != want {
			t.Errorf("%q became %q, want %q", claim, got, want)
		}
	}
}
