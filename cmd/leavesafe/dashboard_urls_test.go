package main

import (
	"context"
	"testing"
	"time"

	"github.com/leavesafe/leavesafe/internal/auth"
	"github.com/leavesafe/leavesafe/internal/monitor"
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
		"working": {
			remote.State{Enabled: true, PublicURL: "https://198.51.100.4:9443", UPnP: remote.UPnPOK},
			"ACTIVE — 198.51.100.4:9443",
		},
		"upnp refused": {
			remote.State{Enabled: true, UPnP: remote.UPnPFailed, ManualPort: 9443},
			"ACTIVE — forward TCP 9443 by hand",
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

// The public URL goes first because the dashboard renders the first URL as its
// QR code, and it is the one a phone on another network needs. The local
// addresses stay behind it rather than being replaced — with remote access on,
// scanning the public URL from the same Wi-Fi needs NAT hairpinning, which
// plenty of routers do not do.
func TestReachableURLsPutsThePublicAddressFirstWithoutDroppingTheLocalOnes(t *testing.T) {
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

	local := reachableURLs(srv, remote.State{})
	if len(local) == 0 {
		t.Fatal("no local URLs at all")
	}

	withPublic := reachableURLs(srv, remote.State{
		Enabled:   true,
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
