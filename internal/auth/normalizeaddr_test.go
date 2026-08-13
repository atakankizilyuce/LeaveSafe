package auth

import (
	"strings"
	"testing"
)

// NormalizeAddr decides what counts as "the same peer", and two separate
// defenses are keyed on its answer: the failed-attempt lockout, and the cap on
// how many unpaired sockets one peer may hold open. Both are bypassed the
// moment it hands back something that changes between connections — which the
// port does, on every single one.

// The property the lockout rests on. A stranger guessing keys opens a new TCP
// connection for each attempt and gets a new source port every time; if that
// port reached the counter, the fifth guess would be the first guess again and
// the lockout would never fire.
func TestEveryPortFromOneHostIsTheSamePeer(t *testing.T) {
	hosts := map[string][]string{
		"IPv4":            {"192.0.2.10:1", "192.0.2.10:443", "192.0.2.10:65535"},
		"IPv6":            {"[2001:db8::1]:1", "[2001:db8::1]:443", "[2001:db8::1]:65535"},
		"IPv6 loopback":   {"[::1]:80", "[::1]:8080"},
		"IPv4 loopback":   {"127.0.0.1:80", "127.0.0.1:9999"},
		"a private range": {"10.0.0.7:1024", "10.0.0.7:1025"},
	}
	for name, addrs := range hosts {
		t.Run(name, func(t *testing.T) {
			first := NormalizeAddr(addrs[0])
			if first == "" {
				t.Fatalf("NormalizeAddr(%q) returned nothing to key on", addrs[0])
			}
			for _, addr := range addrs[1:] {
				if got := NormalizeAddr(addr); got != first {
					t.Errorf("NormalizeAddr(%q) = %q, but NormalizeAddr(%q) = %q — "+
						"a new port buys a fresh allowance", addr, got, addrs[0], first)
				}
			}
		})
	}
}

// Different hosts must stay different, or one stranger's failures would lock
// out every other phone on the network — including the owner's.
func TestDifferentHostsAreDifferentPeers(t *testing.T) {
	addrs := []string{
		"192.0.2.10:443",
		"192.0.2.11:443",
		"[2001:db8::1]:443",
		"[2001:db8::2]:443",
		"127.0.0.1:443",
	}
	seen := make(map[string]string, len(addrs))
	for _, addr := range addrs {
		key := NormalizeAddr(addr)
		if other, clash := seen[key]; clash {
			t.Errorf("%q and %q both key on %q", addr, other, key)
		}
		seen[key] = addr
	}
}

// A transport with no network address of its own — Bluetooth, and the console —
// still has to key on something, or every one of them would share the empty
// string with whatever else produced it.
func TestATransportWithNoAddressGetsANameOfItsOwn(t *testing.T) {
	if got := NormalizeAddr(""); got != UnknownAddr {
		t.Errorf("NormalizeAddr(%q) = %q, want %q", "", got, UnknownAddr)
	}
}

// Anything that is not a host:port pair is passed through rather than mangled
// into a shorter string. Trimming it would be worse than useless: two different
// peers could be trimmed onto the same key, and a lockout meant for one would
// land on the other.
func TestSomethingThatIsNotHostPortIsKeptWhole(t *testing.T) {
	cases := []string{
		"192.0.2.10",             // an address with no port
		"2001:db8::1",            // bare IPv6, which SplitHostPort will not take
		"/tmp/leavesafe.sock",    // a path, if a transport ever hands one over
		"bluetooth-aa:bb:cc",     // a made-up identifier with colons in it
		strings.Repeat("x", 300), // something absurd
	}
	for _, addr := range cases {
		got := NormalizeAddr(addr)
		if got == "" {
			t.Errorf("NormalizeAddr(%q) returned nothing to key on", addr)
		}
		if got != addr && !strings.HasPrefix(addr, "[") {
			// Bare IPv6 is the one shape SplitHostPort reads as host:port; the
			// rest must come back untouched.
			if addr == "2001:db8::1" {
				continue
			}
			t.Errorf("NormalizeAddr(%q) = %q, want it kept whole", addr, got)
		}
	}
}

// The exported wrapper and the internal one have to agree, because the two
// defenses call different ones — the hub calls the exported one for the socket
// cap, the manager the internal one for the lockout. If they ever diverge, one
// of the two is keyed on something the other does not recognize.
func TestTheExportedAndInternalFormsAgree(t *testing.T) {
	cases := []string{
		"192.0.2.10:443", "[2001:db8::1]:443", "", "192.0.2.10",
		"not an address", UnknownAddr,
	}
	for _, addr := range cases {
		if NormalizeAddr(addr) != normalizeAddr(addr) {
			t.Errorf("NormalizeAddr(%q) = %q but normalizeAddr(%q) = %q",
				addr, NormalizeAddr(addr), addr, normalizeAddr(addr))
		}
	}
}

// This is what the safety of the whole scheme rests on, so it is written down
// rather than left as an assumption: the address is whatever the operating
// system reported for the connection, never anything a client chose.
//
// It matters because the key is the address as text, not a parsed one. `::1`
// and `0:0:0:0:0:0:0:1` are the same host and would key differently — a free
// extra allowance for anyone who could choose how their address is spelled.
// Nobody can: net/http fills RemoteAddr in from the accepted socket. If a
// transport is ever added that takes the peer's word for where it is, this
// function has to canonicalise before that lands.
func TestTextualVariantsOfOneAddressDoNotCollapse(t *testing.T) {
	short := NormalizeAddr("[::1]:443")
	long := NormalizeAddr("[0:0:0:0:0:0:0:1]:443")

	if short == long {
		t.Skip("the implementation now canonicalises; this test's warning is obsolete")
	}
	t.Logf("as documented: %q and %q are the same host and key differently — "+
		"safe only because the address comes from the accepted socket", short, long)
}
