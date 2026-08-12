package network

import (
	"errors"
	"testing"
)

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

// fakeGateway is a router that says whatever a test needs it to say. The real
// one answers unauthenticated SSDP from somewhere on the local network, which
// is precisely why what this package does with its answers is worth testing.
type fakeGateway struct {
	wan    string
	wanErr error
}

func (g fakeGateway) Forward(uint16, string) error { return nil }
func (g fakeGateway) Clear(uint16) error           { return nil }
func (g fakeGateway) ExternalIP() (string, error)  { return g.wan, g.wanErr }

// The two accessors answer different questions and are allowed to disagree.
// ExternalIP is asked what address to point a phone at, and a private answer to
// that is not an answer. WANAddress is asked where the router sits, and a
// private answer is the finding — it is how a second router in front of this
// one is detected at all.
func TestWhereTheRouterSitsIsAskedDifferentlyFromWhereToPointAPhone(t *testing.T) {
	pm := &PortMapping{device: fakeGateway{wan: "192.168.1.2"}}

	if _, err := pm.ExternalIP(); err == nil {
		t.Error("a private address was offered as somewhere to point a phone")
	}

	wan, err := pm.WANAddress()
	if err != nil {
		t.Fatalf("WANAddress = %v, want the address the router reported", err)
	}
	if wan != "192.168.1.2" {
		t.Errorf("WANAddress = %q, want the router's own address unfiltered", wan)
	}
}

func TestARouterThatWillNotSayWhereItIsReportsSo(t *testing.T) {
	pm := &PortMapping{device: fakeGateway{wanErr: errors.New("no route to the gateway")}}

	if _, err := pm.WANAddress(); err == nil {
		t.Error("a router that answered with an error was reported as having answered")
	}
}

func TestSomethingThatIsNotAnAddressIsNotOne(t *testing.T) {
	pm := &PortMapping{device: fakeGateway{wan: "the router, obviously"}}

	if _, err := pm.WANAddress(); err == nil {
		t.Error("a string that is not an IP address was accepted as the router's address")
	}
}
