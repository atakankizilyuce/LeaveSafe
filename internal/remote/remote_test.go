package remote

import (
	"context"
	"crypto/tls"
	"errors"
	"testing"

	"github.com/leavesafe/leavesafe/internal/server"
)

type fakeListener struct {
	started int
	stopped int
	port    int
	err     error
}

func (f *fakeListener) StartRemote(_ tls.Certificate, _ string, port int) (int, error) {
	if f.err != nil {
		return 0, f.err
	}
	f.started++
	if port == 0 {
		port = 9443
	}
	f.port = port
	return port, nil
}

func (f *fakeListener) StopRemote() { f.stopped++; f.port = 0 }

type fakeMapping struct {
	externalIP string
	ipErr      error
	closed     int
}

func (m *fakeMapping) ExternalIP() (string, error) { return m.externalIP, m.ipErr }
func (m *fakeMapping) Close() error                { m.closed++; return nil }
func (m *fakeMapping) KeepAlive(_ context.Context) {}

// workingDeps is the everything-succeeds case; each test bends one part of it.
func workingDeps(mapping *fakeMapping, publicIP string) Deps {
	return Deps{
		Cert:     server.GenerateOrLoadCert,
		OpenPort: func(int) (PortMapping, error) { return mapping, nil },
		PublicIP: func() (string, error) { return publicIP, nil },
	}
}

func TestEnableReportsThePublicURL(t *testing.T) {
	ln := &fakeListener{}
	c := NewController(ln, t.TempDir(), 9443, workingDeps(&fakeMapping{}, "198.51.100.4"))

	got := c.Enable(context.Background())

	if !got.Enabled {
		t.Fatalf("Enabled = false, reason %q", got.Reason)
	}
	if got.PublicURL != "https://198.51.100.4:9443" {
		t.Errorf("PublicURL = %q, want https://198.51.100.4:9443", got.PublicURL)
	}
	if got.UPnP != UPnPOK {
		t.Errorf("UPnP = %q, want %q", got.UPnP, UPnPOK)
	}
	if got.CertFP == "" {
		t.Error("CertFP is empty, but the listener is serving a certificate")
	}
	if ln.started != 1 {
		t.Errorf("listener started %d times, want 1", ln.started)
	}
}

// Enable is called from three places — startup, the phone, the console — and
// none of them coordinates with the others.
func TestEnableTwiceStartsOneListener(t *testing.T) {
	ln := &fakeListener{}
	c := NewController(ln, t.TempDir(), 9443, workingDeps(&fakeMapping{}, "198.51.100.4"))

	c.Enable(context.Background())
	c.Enable(context.Background())

	if ln.started != 1 {
		t.Errorf("listener started %d times, want 1", ln.started)
	}
}

func TestDisableTwiceStopsOnce(t *testing.T) {
	ln := &fakeListener{}
	mapping := &fakeMapping{}
	c := NewController(ln, t.TempDir(), 9443, workingDeps(mapping, "198.51.100.4"))

	c.Enable(context.Background())
	c.Disable()
	c.Disable()

	if ln.stopped != 1 {
		t.Errorf("listener stopped %d times, want 1", ln.stopped)
	}
	if mapping.closed != 1 {
		t.Errorf("port mapping closed %d times, want 1", mapping.closed)
	}
	if c.State().Enabled {
		t.Error("still reporting enabled after Disable")
	}
}

// Publishing a port to the internet without a certificate would put the pairing
// key on the wire in cleartext. The listener must not come up at all.
func TestACertificateFailureLeavesRemoteAccessOff(t *testing.T) {
	ln := &fakeListener{}
	deps := workingDeps(&fakeMapping{}, "198.51.100.4")
	deps.Cert = func(string) (tls.Certificate, string, error) {
		return tls.Certificate{}, "", errors.New("disk is full")
	}
	c := NewController(ln, t.TempDir(), 9443, deps)

	got := c.Enable(context.Background())

	if got.Enabled {
		t.Fatal("remote access came up without a certificate")
	}
	if ln.started != 0 {
		t.Errorf("listener started %d times, want 0", ln.started)
	}
	if got.Reason == "" {
		t.Error("no reason given for refusing to start")
	}
}

// UPnP being off is the common case, not a fatal one: the user can forward the
// port by hand, and the listener has to be up for that to be worth doing.
func TestUPnPFailureKeepsTheListenerUpAndNamesThePort(t *testing.T) {
	ln := &fakeListener{}
	deps := workingDeps(&fakeMapping{}, "198.51.100.4")
	deps.OpenPort = func(int) (PortMapping, error) { return nil, errors.New("no IGD found") }
	c := NewController(ln, t.TempDir(), 9443, deps)

	got := c.Enable(context.Background())

	if !got.Enabled {
		t.Fatalf("listener was taken down over a UPnP failure, reason %q", got.Reason)
	}
	if got.UPnP != UPnPFailed {
		t.Errorf("UPnP = %q, want %q", got.UPnP, UPnPFailed)
	}
	if got.ManualPort != 9443 {
		t.Errorf("ManualPort = %d, want 9443", got.ManualPort)
	}
	if got.PublicURL != "https://198.51.100.4:9443" {
		t.Errorf("PublicURL = %q — the address is known even without a mapping", got.PublicURL)
	}
}

// Behind CGNAT there is nothing to forward and nothing to wait for. Leaving the
// listener up would be telling the user to keep trying.
func TestCarrierNATStopsTheListenerAndSaysWhy(t *testing.T) {
	ln := &fakeListener{}
	c := NewController(ln, t.TempDir(), 9443, workingDeps(&fakeMapping{}, "100.100.50.7"))

	got := c.Enable(context.Background())

	if got.Enabled {
		t.Fatal("remote access stayed on behind CGNAT")
	}
	if got.UPnP != UPnPCarrierNAT {
		t.Errorf("UPnP = %q, want %q", got.UPnP, UPnPCarrierNAT)
	}
	if ln.stopped != 1 {
		t.Errorf("listener stopped %d times, want 1", ln.stopped)
	}
	if got.Reason == "" {
		t.Error("no reason given for the CGNAT refusal")
	}
	if got.PublicURL != "" {
		t.Errorf("PublicURL = %q, want empty — nothing is listening there", got.PublicURL)
	}
}

// No public address is not a failure of the listener: someone on a network with
// no outbound STUN can still reach it by an address they know.
func TestAnUnknownPublicAddressLeavesTheListenerUp(t *testing.T) {
	ln := &fakeListener{}
	deps := workingDeps(&fakeMapping{ipErr: errors.New("no answer")}, "")
	deps.PublicIP = func() (string, error) { return "", errors.New("STUN timed out") }
	c := NewController(ln, t.TempDir(), 9443, deps)

	got := c.Enable(context.Background())

	if !got.Enabled {
		t.Fatalf("listener came down over an unknown address, reason %q", got.Reason)
	}
	if got.PublicURL != "" {
		t.Errorf("PublicURL = %q, want empty", got.PublicURL)
	}
	if got.Reason == "" {
		t.Error("no reason given for the missing URL")
	}
}

// The router's answer is the fallback, and it only gets asked when the internet
// did not answer — it arrives over unauthenticated SSDP from whatever replied
// fastest on the local network.
func TestTheRouterIsAskedOnlyWhenTheInternetDoesNotAnswer(t *testing.T) {
	ln := &fakeListener{}
	mapping := &fakeMapping{externalIP: "203.0.113.9"}
	deps := workingDeps(mapping, "")
	deps.PublicIP = func() (string, error) { return "", errors.New("STUN timed out") }
	c := NewController(ln, t.TempDir(), 9443, deps)

	got := c.Enable(context.Background())

	if got.PublicURL != "https://203.0.113.9:9443" {
		t.Errorf("PublicURL = %q, want the router's address to have been used", got.PublicURL)
	}
}

// A listener that will not bind is not a state to report as working.
func TestAListenerThatWillNotBindLeavesRemoteAccessOff(t *testing.T) {
	ln := &fakeListener{err: errors.New("address already in use")}
	c := NewController(ln, t.TempDir(), 9443, workingDeps(&fakeMapping{}, "198.51.100.4"))

	got := c.Enable(context.Background())

	if got.Enabled {
		t.Fatal("reported enabled after the listener refused to bind")
	}
	if got.Reason == "" {
		t.Error("no reason given for the bind failure")
	}
}
