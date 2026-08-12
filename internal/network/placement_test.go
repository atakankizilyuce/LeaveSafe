package network

import "testing"

// The case this whole file exists for, and the bug it names.
//
// Under carrier-grade NAT the address the internet reports is the ISP's own
// routable one: an ordinary public address that passes every check while
// belonging to thousands of subscribers at once. The 100.64 address is the one
// the router holds, on the inside — and nothing was asking the router. So a
// machine behind carrier NAT mapped a port, was handed a public URL, printed it
// as the code to scan, and could not be reached by anybody. Asking both and
// comparing them is the whole check.
func TestCarrierNATIsFoundByAskingTheRouterRatherThanTheInternet(t *testing.T) {
	// What the internet says is a perfectly ordinary public address.
	const asTheInternetSeesIt = "85.105.20.30"
	if IsCarrierGradeNAT(asTheInternetSeesIt) {
		t.Fatal("the premise of this test is wrong: the public address looks ordinary")
	}

	if got := RouterPlacement("100.64.13.7", asTheInternetSeesIt); got != PlacementCarrier {
		t.Errorf("RouterPlacement = %q, want %q — the router is inside the ISP's NAT", got, PlacementCarrier)
	}
}

func TestARouterAtTheEdgeIsTheOneTheInternetSees(t *testing.T) {
	if got := RouterPlacement("85.105.20.30", "85.105.20.30"); got != PlacementEdge {
		t.Errorf("RouterPlacement = %q, want %q", got, PlacementEdge)
	}
}

// A modem in router mode, a landlord's box, a phone's hotspot. Unlike carrier
// NAT this one is the user's to fix if they can reach the outer router, so it
// is told apart from it rather than lumped in.
func TestARouterBehindAnotherRouterIsToldApartFromCarrierNAT(t *testing.T) {
	for _, wan := range []string{"192.168.1.2", "10.0.0.8", "172.16.4.9"} {
		t.Run(wan, func(t *testing.T) {
			if got := RouterPlacement(wan, "85.105.20.30"); got != PlacementDouble {
				t.Errorf("RouterPlacement(%q) = %q, want %q", wan, got, PlacementDouble)
			}
		})
	}
}

// Link-local is what a router reports when its uplink never came up. It is not
// a second router, but it is certainly not the edge either.
func TestAnUplinkThatNeverCameUpIsNotTheEdge(t *testing.T) {
	if got := RouterPlacement("169.254.7.7", "85.105.20.30"); got != PlacementDouble {
		t.Errorf("RouterPlacement = %q, want %q", got, PlacementDouble)
	}
}

// Nothing is claimed from a question that was not answered. Reporting somebody's
// network as broken because a router would not say where it is would be worse
// than saying nothing.
func TestAnUnanswerableComparisonClaimsNothing(t *testing.T) {
	cases := map[string][2]string{
		"no router address": {"", "85.105.20.30"},
		"no public address": {"100.64.1.1", ""},
		"neither":           {"", ""},
		"not an address":    {"the router", "85.105.20.30"},
		"public but not it": {"203.0.113.9", "85.105.20.30"},
	}
	for name, pair := range cases {
		t.Run(name, func(t *testing.T) {
			if got := RouterPlacement(pair[0], pair[1]); got != PlacementUnknown {
				t.Errorf("RouterPlacement(%q, %q) = %q, want %q", pair[0], pair[1], got, PlacementUnknown)
			}
		})
	}
}
