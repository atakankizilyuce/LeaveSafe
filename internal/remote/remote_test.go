package remote

import (
	"context"
	"crypto/tls"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeListener struct {
	mu      sync.Mutex
	started int
	stopped int
	port    int
	err     error
}

func (f *fakeListener) StartRemote(_ tls.Certificate, _ string, port int) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
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

func (f *fakeListener) StopRemote() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopped++
	f.port = 0
}

func (f *fakeListener) counts() (started, stopped int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.started, f.stopped
}

type fakeMapping struct {
	mu         sync.Mutex
	externalIP string
	ipErr      error
	closed     int
}

func (m *fakeMapping) ExternalIP() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.externalIP, m.ipErr
}

func (m *fakeMapping) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed++
	return nil
}

func (m *fakeMapping) KeepAlive(_ context.Context) {}

func (m *fakeMapping) closeCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closed
}

// testFingerprint stands in for a real certificate's SHA-256. Generating one
// would mean importing internal/server, which imports internal/ws, which
// imports this package — and producing a certificate is that package's
// concern, tested there. What this package owes is that whatever fingerprint
// it is handed reaches State.
const testFingerprint = "aa:bb:cc:dd:ee:ff:00:11:22:33:44:55:66:77:88:99"

// workingDeps is the everything-succeeds case; each test bends one part of it.
func workingDeps(mapping *fakeMapping, publicIP string) Deps {
	return Deps{
		Cert: func(string) (tls.Certificate, string, error) {
			return tls.Certificate{}, testFingerprint, nil
		},
		OpenPort: func(int) (PortMapping, error) { return mapping, nil },
		PublicIP: func() (string, error) { return publicIP, nil },
	}
}

// enableAndWait brings remote access up and returns the state the reachability
// probe settled on.
//
// Every test that cares about a public address, a port mapping or CGNAT has to
// go through here now: Enable returns as soon as the listener is up, and the
// router and the internet answer afterwards. Reading Enable's return value for
// those would be reading the state before the question was asked.
func enableAndWait(t *testing.T, c *Controller) State {
	t.Helper()

	settled := make(chan State, 1)
	c.SetOnUpdate(func(st State) {
		select {
		case settled <- st:
		default:
		}
	})

	if got := c.Enable(context.Background()); !got.Enabled && got.Reason != "" {
		// Failed before the probe could start, so there is nothing to wait for.
		return got
	}

	select {
	case st := <-settled:
		return st
	case <-time.After(5 * time.Second):
		t.Fatal("the reachability probe never reported")
		return State{}
	}
}

func TestEnableReportsThePublicURL(t *testing.T) {
	ln := &fakeListener{}
	c := NewController(ln, t.TempDir(), 9443, workingDeps(&fakeMapping{}, "198.51.100.4"))

	got := enableAndWait(t, c)

	if !got.Enabled {
		t.Fatalf("Enabled = false, reason %q", got.Reason)
	}
	if got.PublicURL != "https://198.51.100.4:9443" {
		t.Errorf("PublicURL = %q, want https://198.51.100.4:9443", got.PublicURL)
	}
	if got.UPnP != UPnPOK {
		t.Errorf("UPnP = %q, want %q", got.UPnP, UPnPOK)
	}
	if got.CertFP != testFingerprint {
		t.Errorf("CertFP = %q, want the fingerprint the certificate came with", got.CertFP)
	}
	if started, _ := ln.counts(); started != 1 {
		t.Errorf("listener started %d times, want 1", started)
	}
}

// The listener is what the user is waiting on, and it is up the moment Enable
// returns. Asking the router for a port mapping takes about thirty-five seconds
// to fail on a network with no UPnP gateway, and that used to be thirty-five
// seconds before the dashboard drew anything.
func TestEnableReturnsBeforeTheNetworkHasAnswered(t *testing.T) {
	ln := &fakeListener{}
	release := make(chan struct{})
	deps := workingDeps(&fakeMapping{}, "198.51.100.4")
	deps.OpenPort = func(int) (PortMapping, error) {
		<-release
		return &fakeMapping{}, nil
	}
	c := NewController(ln, t.TempDir(), 9443, deps)

	done := make(chan State, 1)
	go func() { done <- c.Enable(context.Background()) }()

	select {
	case got := <-done:
		if !got.Enabled {
			t.Fatalf("Enabled = false, reason %q", got.Reason)
		}
		if !got.Probing {
			t.Error("Probing = false — the state does not say the answer is still coming")
		}
		if got.PublicURL != "" {
			t.Errorf("PublicURL = %q before the probe finished", got.PublicURL)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Enable blocked on the port mapping")
	}
	close(release)
}

// Enable is called from three places — startup, the phone, the console — and
// none of them coordinates with the others.
func TestEnableTwiceStartsOneListener(t *testing.T) {
	ln := &fakeListener{}
	c := NewController(ln, t.TempDir(), 9443, workingDeps(&fakeMapping{}, "198.51.100.4"))

	enableAndWait(t, c)
	c.Enable(context.Background())

	if started, _ := ln.counts(); started != 1 {
		t.Errorf("listener started %d times, want 1", started)
	}
}

func TestDisableTwiceStopsOnce(t *testing.T) {
	ln := &fakeListener{}
	mapping := &fakeMapping{}
	c := NewController(ln, t.TempDir(), 9443, workingDeps(mapping, "198.51.100.4"))

	enableAndWait(t, c)
	c.Disable()
	c.Disable()

	if _, stopped := ln.counts(); stopped != 1 {
		t.Errorf("listener stopped %d times, want 1", stopped)
	}
	if got := mapping.closeCount(); got != 1 {
		t.Errorf("port mapping closed %d times, want 1", got)
	}
	if c.State().Enabled {
		t.Error("still reporting enabled after Disable")
	}
}

// A probe that is still talking to the router when the user switches remote
// access off must not come back and describe a listener that is gone — nor
// leave its port mapping behind, which would be a hole in the router pointing
// at nothing.
func TestAProbeThatOutlivesItsListenerPublishesNothing(t *testing.T) {
	ln := &fakeListener{}
	mapping := &fakeMapping{}
	release := make(chan struct{})
	deps := workingDeps(mapping, "198.51.100.4")
	deps.OpenPort = func(int) (PortMapping, error) {
		<-release
		return mapping, nil
	}
	c := NewController(ln, t.TempDir(), 9443, deps)

	var updates int
	var mu sync.Mutex
	c.SetOnUpdate(func(State) {
		mu.Lock()
		updates++
		mu.Unlock()
	})

	c.Enable(context.Background())
	c.Disable()
	close(release)

	// The probe has to get far enough to try to publish before this proves
	// anything, and there is no signal for "did not happen" other than time.
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	got := updates
	mu.Unlock()
	if got != 0 {
		t.Errorf("a stale probe published %d updates, want 0", got)
	}
	if c.State().Enabled {
		t.Error("a stale probe brought remote access back")
	}
	if closed := mapping.closeCount(); closed == 0 {
		t.Error("the mapping a stale probe opened was left on the router")
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
	if started, _ := ln.counts(); started != 0 {
		t.Errorf("listener started %d times, want 0", started)
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

	got := enableAndWait(t, c)

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
	if got.Probing {
		t.Error("Probing = true after the probe reported")
	}
}

// Behind CGNAT there is nothing to forward and nothing to wait for. Leaving the
// listener up would be telling the user to keep trying.
func TestCarrierNATStopsTheListenerAndSaysWhy(t *testing.T) {
	ln := &fakeListener{}
	c := NewController(ln, t.TempDir(), 9443, workingDeps(&fakeMapping{}, "100.100.50.7"))

	got := enableAndWait(t, c)

	if got.Enabled {
		t.Fatal("remote access stayed on behind CGNAT")
	}
	if got.UPnP != UPnPCarrierNAT {
		t.Errorf("UPnP = %q, want %q", got.UPnP, UPnPCarrierNAT)
	}
	if _, stopped := ln.counts(); stopped != 1 {
		t.Errorf("listener stopped %d times, want 1", stopped)
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

	got := enableAndWait(t, c)

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

	got := enableAndWait(t, c)

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
