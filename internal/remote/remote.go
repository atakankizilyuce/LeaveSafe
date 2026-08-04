// Package remote owns the lifecycle of LeaveSafe's internet-facing listener:
// the certificate it needs, the port mapping that makes it reachable, and the
// public address it is reachable at.
//
// It exists as a package because three callers drive it — the startup path, the
// phone's settings screen and the console's `mode` command — and the sequence
// has enough failure branches that three copies of it would mean three slightly
// different answers to "did that work?".
package remote

import (
	"context"
	"crypto/tls"
	"fmt"
	"sync"

	log "github.com/sirupsen/logrus"

	"github.com/leavesafe/leavesafe/internal/network"
	"github.com/leavesafe/leavesafe/internal/safe"
)

// UPnPState says how the port mapping went, which is the difference between
// "this works", "forward a port yourself" and "this cannot work here".
type UPnPState string

const (
	UPnPUnknown    UPnPState = ""
	UPnPOK         UPnPState = "ok"
	UPnPFailed     UPnPState = "failed"
	UPnPCarrierNAT UPnPState = "cgnat"
)

// State is what remote access is actually doing, as opposed to what the config
// says the user asked for. The two are kept apart deliberately: a failure has to
// be reportable without being mistaken for the user changing their mind.
type State struct {
	Enabled bool `json:"enabled"`
	// Probing is true between the listener coming up and the router and the
	// internet having answered how reachable it is.
	//
	// The two used to be one step, and the second one is slow: a network with
	// no UPnP gateway spends about thirty-five seconds discovering that, and
	// every one of them was spent before the dashboard drew its first line.
	// They are now separate, so this is the difference between "no public
	// address" and "no public address yet" — and a phone told the first when
	// the second is true is being told something false.
	Probing    bool      `json:"probing,omitempty"`
	PublicURL  string    `json:"public_url,omitempty"`
	CertFP     string    `json:"cert_fp,omitempty"`
	UPnP       UPnPState `json:"upnp,omitempty"`
	ManualPort int       `json:"manual_port,omitempty"`
	Reason     string    `json:"reason,omitempty"`
}

// Listener is the part of *server.Server this package drives.
type Listener interface {
	StartRemote(cert tls.Certificate, certFP string, port int) (int, error)
	StopRemote()
}

// PortMapping is the part of *network.PortMapping this package drives.
type PortMapping interface {
	ExternalIP() (string, error)
	Close() error
	KeepAlive(ctx context.Context)
}

// Deps are the outside-world calls, injected so every failure branch is
// testable without a router, a certificate authority or a network.
type Deps struct {
	Cert     func(configDir string) (tls.Certificate, string, error)
	OpenPort func(port int) (PortMapping, error)
	PublicIP func() (string, error)
}

// Controller starts and stops remote access.
type Controller struct {
	mu        sync.Mutex
	srv       Listener
	configDir string
	port      int
	deps      Deps

	state      State
	mapping    PortMapping
	keepCancel context.CancelFunc
	onUpdate   func(State)

	// gen counts times remote access has been brought up. A probe carries the
	// generation it started under and publishes nothing if that is no longer
	// the current one, so an answer about a listener that has since been taken
	// down cannot arrive late and describe the one running now.
	gen uint64
}

// NewController returns a controller that publishes srv on port when enabled.
func NewController(srv Listener, configDir string, port int, deps Deps) *Controller {
	return &Controller{srv: srv, configDir: configDir, port: port, deps: deps}
}

// SetOnUpdate registers what to do with a state that arrives on its own, which
// is every state after the first: the reachability probe reports minutes after
// Enable has returned.
//
// It does not fire on registration. The caller has the state Enable returned
// and pushes that itself, so firing here would be a second first draw.
func (c *Controller) SetOnUpdate(fn func(State)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onUpdate = fn
}

// State returns what remote access is currently doing.
func (c *Controller) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

// Enable brings the internet-facing listener up and returns as soon as it is
// listening.
//
// What it does not do is wait to find out how reachable that listener is from
// outside. Asking the router for a port mapping and the internet for this
// machine's address are network round trips, and the first of them takes about
// thirty-five seconds to fail on a network with no UPnP gateway — which is a
// common network, not an exotic one. That wait used to sit between the user
// answering the connection question and the dashboard appearing at all, with
// nothing on screen to say what was being waited for.
//
// So the answer arrives later, through the callback SetOnUpdate registers, and
// the state carries Probing until it does. Nothing that matters is deferred:
// the listener is up, and the local network never depended on any of this.
//
// It does not return an error. Every failure here is one the user has to be
// told about in words rather than one a caller can usefully retry, so the
// reason travels in State.Reason to the phone and the dashboard alike.
func (c *Controller) Enable(ctx context.Context) State {
	c.mu.Lock()

	if c.state.Enabled {
		st := c.state
		c.mu.Unlock()
		return st
	}

	cert, fp, err := c.deps.Cert(c.configDir)
	if err != nil {
		// Serving an internet-facing port over plain HTTP would put the pairing
		// key — the only thing guarding the alarm — in cleartext on the wire.
		// Staying on the LAN is the safe failure.
		log.Errorf("TLS certificate error: %v", err)
		log.Error("Remote access DISABLED: refusing to expose this port without TLS.")
		log.Error("Pairing is still available on the local network. Fix the certificate to restore remote access.")
		c.state = State{Reason: fmt.Sprintf("Could not create the TLS certificate: %v. "+
			"Remote access will not run without one; the local network is unaffected.", err)}
		st := c.state
		c.mu.Unlock()
		return st
	}

	boundPort, err := c.srv.StartRemote(cert, fp, c.port)
	if err != nil {
		log.Errorf("Remote listener error: %v", err)
		c.state = State{Reason: fmt.Sprintf("Could not open port %d: %v. "+
			"The local network is unaffected.", c.port, err)}
		st := c.state
		c.mu.Unlock()
		return st
	}

	c.gen++
	gen := c.gen
	c.state = State{Enabled: true, Probing: true, CertFP: fp}
	st := c.state
	c.mu.Unlock()

	safe.Go("remote-reachability", func() { c.probe(ctx, gen, boundPort, fp) })
	return st
}

// probe asks the router for a port mapping and the internet for this machine's
// address, then publishes what it found.
//
// It runs on its own goroutine and can take the better part of a minute. Every
// exit publishes exactly once, through publish, which drops the result if
// remote access has been taken down or brought up again in the meantime.
func (c *Controller) probe(ctx context.Context, gen uint64, boundPort int, fp string) {
	next := State{Enabled: true, CertFP: fp, UPnP: UPnPOK}

	mapping, err := c.deps.OpenPort(boundPort)
	if err != nil {
		log.Warnf("UPnP failed: %v — manual port forwarding required (port %d)", err, boundPort)
		next.UPnP = UPnPFailed
		next.ManualPort = boundPort
		next.Reason = fmt.Sprintf("Your router did not accept an automatic port mapping, so nothing "+
			"outside your network can reach this machine yet. Forward TCP port %d to it in the "+
			"router's admin page, or pair over the local network instead.", boundPort)
	} else {
		c.mu.Lock()
		if gen != c.gen {
			// Taken down while the router was thinking. The mapping belongs to a
			// listener that is gone, and leaving it behind would leave a hole in
			// the router pointing at nothing.
			c.mu.Unlock()
			_ = mapping.Close()
			return
		}
		c.mapping = mapping
		keepCtx, cancel := context.WithCancel(ctx)
		c.keepCancel = cancel
		c.mu.Unlock()
		safe.Go("upnp-keepalive", func() { mapping.KeepAlive(keepCtx) })
	}

	// Asked of the internet first, and of the router only if that fails. The
	// router's answer arrives over unauthenticated SSDP from whatever replied
	// fastest on the local network, and this address goes into the QR code with
	// the pairing key beside it.
	publicIP, ipErr := c.deps.PublicIP()
	if ipErr != nil {
		log.Warnf("Could not determine public IP: %v", ipErr)
		publicIP = ""
		if mapping != nil {
			if ip, mapErr := mapping.ExternalIP(); mapErr == nil {
				log.Infof("Using the address the router reports, %s, as the public one", ip)
				publicIP = ip
			} else {
				log.Warnf("The router's idea of the public address was not usable: %v", mapErr)
			}
		}
	}

	switch {
	case publicIP == "":
		next.Reason = "No public address could be found, so there is no URL to scan from " +
			"another network. The local network is unaffected."
	case network.IsCarrierGradeNAT(publicIP):
		// Nothing the user can do to their own router changes this, so leaving
		// the listener up would be inviting them to keep trying.
		log.Warnf("Public address %s is carrier-grade NAT — remote access cannot work here", publicIP)
		c.publishCarrierNAT(gen, publicIP)
		return
	default:
		next.PublicURL = fmt.Sprintf("https://%s:%d", publicIP, boundPort)
	}

	c.publish(gen, next)
}

// publish stores a probe's result and hands it on, unless the listener it
// describes is no longer the current one.
func (c *Controller) publish(gen uint64, st State) {
	c.mu.Lock()
	if gen != c.gen {
		c.mu.Unlock()
		return
	}
	c.state = st
	fn := c.onUpdate
	c.mu.Unlock()

	if fn != nil {
		fn(st)
	}
}

// publishCarrierNAT is publish for the one outcome that also takes the listener
// down, which has to happen under the same lock that checks the generation.
func (c *Controller) publishCarrierNAT(gen uint64, publicIP string) {
	c.mu.Lock()
	if gen != c.gen {
		c.mu.Unlock()
		return
	}
	c.teardownLocked()
	c.state = State{
		UPnP: UPnPCarrierNAT,
		Reason: fmt.Sprintf("Your ISP puts this connection behind carrier-grade NAT (%s). "+
			"Nothing on this machine or your router can make it reachable from the internet, "+
			"so remote access has been stopped. The local network is unaffected.", publicIP),
	}
	st := c.state
	fn := c.onUpdate
	c.mu.Unlock()

	if fn != nil {
		fn(st)
	}
}

// Disable takes remote access down. Safe to call when it is already down.
func (c *Controller) Disable() State {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Bumped whether or not there is anything to tear down, so a probe still
	// talking to the router cannot come back and describe a listener the user
	// has just switched off.
	c.gen++

	if !c.state.Enabled && c.mapping == nil {
		c.state = State{}
		return c.state
	}
	c.teardownLocked()
	c.state = State{}
	return c.state
}

// teardownLocked closes the listener and the port mapping. The caller holds mu.
func (c *Controller) teardownLocked() {
	if c.keepCancel != nil {
		c.keepCancel()
		c.keepCancel = nil
	}
	if c.mapping != nil {
		_ = c.mapping.Close()
		c.mapping = nil
	}
	c.srv.StopRemote()
}
