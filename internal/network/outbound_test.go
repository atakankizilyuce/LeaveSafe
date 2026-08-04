package network

import "testing"

// The geolocation endpoints are fetched by the laptop and configurable by the
// phone, so the dial guard is what stops one from being aimed at the laptop's
// own network. It has to refuse every address that only means something locally
// while still letting an ordinary public one through.
func TestOutboundGuardRefusesNonPublicAddresses(t *testing.T) {
	refused := map[string]string{
		"loopback":              "127.0.0.1:443",
		"private ten-net":       "10.1.2.3:443",
		"private 172.16/12":     "172.16.9.9:443",
		"private 192.168/16":    "192.168.1.20:443",
		"link-local":            "169.254.10.10:443",
		"unspecified":           "0.0.0.0:443",
		"multicast":             "224.0.0.1:443",
		"IPv6 loopback":         "[::1]:443",
		"IPv6 ULA":              "[fd00::1]:443",
		"IPv6 link-local":       "[fe80::1]:443",
		"a name, not a literal": "metadata.internal:443",
	}
	for what, addr := range refused {
		if err := guardOutboundAddress(addr); err == nil {
			t.Errorf("%s (%q) was allowed as an outbound destination", what, addr)
		}
	}
}

// A public destination is exactly what turning the feature on needs to reach,
// so the guard must not stand in front of one.
func TestOutboundGuardAllowsPublicAddresses(t *testing.T) {
	allowed := map[string]string{
		"an ordinary public v4": "198.51.100.4:443",
		"another public v4":     "203.0.113.9:443",
		"carrier-grade NAT":     "100.64.0.1:443", // reachable or not, not ours to refuse
		"a public v6":           "[2001:db8::1]:443",
	}
	for what, addr := range allowed {
		if err := guardOutboundAddress(addr); err != nil {
			t.Errorf("%s (%q) was refused: %v", what, addr, err)
		}
	}
}
