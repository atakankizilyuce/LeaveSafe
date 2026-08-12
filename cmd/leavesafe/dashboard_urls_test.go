package main

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/leavesafe/leavesafe/internal/auth"
	"github.com/leavesafe/leavesafe/internal/monitor"
	"github.com/leavesafe/leavesafe/internal/network"
	"github.com/leavesafe/leavesafe/internal/remote"
	"github.com/leavesafe/leavesafe/internal/server"
	"github.com/leavesafe/leavesafe/internal/ws"
)

// Turning remote access on adds a public URL, and turning it off takes it away.
// The QR codes are built from the URL list, so a list that does not change is a
// QR code that still points at an address nothing is listening on.
func TestSetURLsReplacesTheListAndItsQRCodes(t *testing.T) {
	sb := newHeadlessStatusBar(nil, nil, "1234 5678 9012 3456", "1234567890123456",
		[]string{"http://192.168.1.10:59353"}, "", "")

	sb.setURLs([]string{
		"https://198.51.100.4:9443",
		"http://192.168.1.10:59353",
	})

	got := sb.urlList()
	if len(got) != 2 {
		t.Fatalf("urlList() has %d entries, want 2: %v", len(got), got)
	}
	if got[0] != "https://198.51.100.4:9443" {
		t.Errorf("public URL is not first: %v", got)
	}

	sb.setURLs([]string{"http://192.168.1.10:59353"})

	if got := sb.urlList(); len(got) != 1 {
		t.Errorf("urlList() has %d entries after the public URL went away, want 1: %v", len(got), got)
	}
}

// `qr <n>` remembers which address the user chose. Losing a URL out from under
// that index would index past the end of the list on the next draw.
func TestSetURLsResetsAQRSelectionThatNoLongerExists(t *testing.T) {
	sb := newHeadlessStatusBar(nil, nil, "1234 5678 9012 3456", "1234567890123456",
		[]string{"https://198.51.100.4:9443", "http://192.168.1.10:59353"}, "", "")
	sb.qrURLIdx = 1

	sb.setURLs([]string{"https://198.51.100.4:9443"})

	if sb.qrURLIdx != 0 {
		t.Errorf("qrURLIdx = %d after the list shrank, want 0", sb.qrURLIdx)
	}
}

// The dashboard's one-line summary has to tell the three failures apart, since
// each one asks something different of the user: nothing, forward a port, or
// give up because the ISP will not allow it.
func TestRemoteStatusLineNamesWhatHappened(t *testing.T) {
	cases := map[string]struct {
		state remote.State
		want  string
	}{
		"off": {
			remote.State{},
			"",
		},
		// ACTIVE is a claim about the outside world, so it is only made about an
		// address something was seen to answer on.
		"working": {
			remote.State{Enabled: true, PublicURL: "https://198.51.100.4:9443",
				UPnP: remote.UPnPOK, Reach: remote.ReachVerified},
			"ACTIVE — 198.51.100.4:9443",
		},
		// The same setup with nothing established about it. It may well work —
		// most routers refuse this check from inside — so the address is still
		// shown, but not as a fact.
		"mapped but never confirmed": {
			remote.State{Enabled: true, PublicURL: "https://198.51.100.4:9443",
				UPnP: remote.UPnPOK, Reach: remote.ReachUnproven},
			"ON — 198.51.100.4:9443 (unconfirmed)",
		},
		// A second router in front of the one holding the mapping. Unlike
		// carrier NAT this one has a fix, and the fix is on the other box.
		"blocked one hop short": {
			remote.State{Enabled: true, PublicURL: "https://198.51.100.4:9443",
				UPnP: remote.UPnPOK, Reach: remote.ReachBlocked, ManualPort: 9443},
			"ON — blocked before the internet, forward TCP 9443 on the outer router",
		},
		// The listener is up and the router has not answered yet. Saying
		// anything definite here would be a wrong answer rather than an early
		// one, and the wait is long enough to look like a hang.
		"still asking the router": {
			remote.State{Enabled: true, Probing: true},
			"ON — checking whether it can be reached…",
		},
		"upnp refused": {
			remote.State{Enabled: true, UPnP: remote.UPnPFailed, ManualPort: 9443},
			"ON — not reachable yet, forward TCP 9443 by hand",
		},
		"no public address": {
			remote.State{Enabled: true, UPnP: remote.UPnPOK},
			"ACTIVE — public address unknown",
		},
		"carrier-grade NAT": {
			remote.State{UPnP: remote.UPnPCarrierNAT},
			"OFF — carrier-grade NAT",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := remoteStatusLine(tc.state); got != tc.want {
				t.Errorf("remoteStatusLine() = %q, want %q", got, tc.want)
			}
		})
	}
}

// testServer is a listening server the URL tests can ask for its addresses.
func testServer(t *testing.T) *server.Server {
	t.Helper()

	authMgr, err := auth.NewManager()
	if err != nil {
		t.Fatalf("auth manager: %v", err)
	}
	srv := server.New(server.Config{
		Hub:  ws.NewHub(authMgr, monitor.NewManager(), "test"),
		Port: 0,
	})
	if err := srv.Listen(); err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
	return srv
}

// The public URL goes first because the dashboard renders the first URL as its
// QR code, and it is the one a phone on another network needs. The local
// addresses stay behind it rather than being replaced — with remote access on,
// scanning the public URL from the same Wi-Fi needs NAT hairpinning, which
// plenty of routers do not do.
func TestReachableURLsPutsThePublicAddressFirstWithoutDroppingTheLocalOnes(t *testing.T) {
	srv := testServer(t)

	local := reachableURLs(srv, remote.State{})
	if len(local) == 0 {
		t.Fatal("no local URLs at all")
	}

	withPublic := reachableURLs(srv, remote.State{
		Enabled:   true,
		UPnP:      remote.UPnPOK,
		Reach:     remote.ReachVerified,
		PublicURL: "https://198.51.100.4:9443",
	})

	if len(withPublic) != len(local)+1 {
		t.Fatalf("got %d URLs with remote access on and %d without", len(withPublic), len(local))
	}
	if withPublic[0] != "https://198.51.100.4:9443" {
		t.Errorf("the public URL is not first: %v", withPublic)
	}
	for i, u := range local {
		if withPublic[i+1] != u {
			t.Errorf("local URL %d changed from %q to %q", i, u, withPublic[i+1])
		}
	}
}

// Without a port mapping the public address is a hope, not a route: this
// machine has an address on the internet and nothing on the path will carry a
// connection to it. The dashboard draws the first URL as its QR code, so
// leaving it first is how a phone ends up on a spinner forever — which is
// exactly what happened on a network with no UPnP gateway.
//
// It stays in the list, because the message beside it tells the user to forward
// the port by hand and it has to be there to scan once they have.
func TestReachableURLsDoesNotOfferAnUnmappedPublicAddressFirst(t *testing.T) {
	srv := testServer(t)

	local := reachableURLs(srv, remote.State{})

	for name, st := range map[string]remote.State{
		"router refused the mapping": {
			Enabled:    true,
			UPnP:       remote.UPnPFailed,
			ManualPort: 9443,
			PublicURL:  "https://198.51.100.4:9443",
		},
		"still asking the router": {
			Enabled:   true,
			Probing:   true,
			PublicURL: "https://198.51.100.4:9443",
		},
	} {
		t.Run(name, func(t *testing.T) {
			got := reachableURLs(srv, st)

			if len(got) != len(local)+1 {
				t.Fatalf("got %d URLs, want %d", len(got), len(local)+1)
			}
			if got[0] != local[0] {
				t.Errorf("an unreachable address is the QR code the user scans: %v", got)
			}
			if got[len(got)-1] != "https://198.51.100.4:9443" {
				t.Errorf("the public address was dropped rather than moved: %v", got)
			}
		})
	}
}

// The hint is shown to somebody whose remote access is not working, on the
// platform they are on. It is a string this program never runs: opening a hole
// in a firewall outlives the process that made it and needs privileges
// LeaveSafe does not otherwise ask for, so the user is told what to run and
// decides.
func TestTheFirewallHintIsForThisPlatform(t *testing.T) {
	got := firewallHint(9443)

	if got == "" {
		t.Fatal("no firewall hint at all")
	}
	if got != network.FirewallCommand(runtime.GOOS, 9443) {
		t.Errorf("firewallHint = %q, want the command for %s", got, runtime.GOOS)
	}
}
