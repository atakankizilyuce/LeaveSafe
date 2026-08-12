package network

import (
	"strconv"
	"strings"
	"testing"
)

// The command is shown to somebody whose remote access is not working, so what
// matters is that it names this port and this platform's firewall. A hint that
// says "check your firewall" is the message they already have.
func TestEachPlatformIsToldWhatToRunOnIt(t *testing.T) {
	const port = 9443

	cases := map[string]string{
		"windows": "netsh",
		"linux":   "ufw",
		"darwin":  "socketfilterfw",
	}
	for goos, want := range cases {
		t.Run(goos, func(t *testing.T) {
			got := FirewallCommand(goos, port)
			if !strings.Contains(got, want) {
				t.Errorf("the %s hint is %q, want it to name %q", goos, got, want)
			}
		})
	}
}

// macOS filters by application rather than by port, so its hint is the one that
// legitimately has no port number in it. Every other one has to carry it, or
// the user is left guessing which port to open.
func TestTheHintCarriesThePortWhereThePlatformUsesOne(t *testing.T) {
	const port = 51820
	number := strconv.Itoa(port)

	for _, goos := range []string{"windows", "linux", "plan9"} {
		t.Run(goos, func(t *testing.T) {
			if got := FirewallCommand(goos, port); !strings.Contains(got, number) {
				t.Errorf("the %s hint is %q, want it to name port %s", goos, got, number)
			}
		})
	}
}

// A platform with no backend still gets something true and actionable rather
// than an empty string, which would read on the dashboard as a sentence that
// stopped halfway.
func TestAnUnknownPlatformStillGetsAnInstruction(t *testing.T) {
	got := FirewallCommand("plan9", 9443)

	if got == "" {
		t.Fatal("an unknown platform was told nothing")
	}
	if !strings.Contains(got, "firewall") {
		t.Errorf("the fallback hint is %q, want it to mention the firewall", got)
	}
}
