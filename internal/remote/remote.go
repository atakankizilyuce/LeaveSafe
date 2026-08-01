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
	Enabled    bool      `json:"enabled"`
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
}

// NewController returns a controller that publishes srv on port when enabled.
func NewController(srv Listener, configDir string, port int, deps Deps) *Controller {
	return &Controller{srv: srv, configDir: configDir, port: port, deps: deps}
}

// State returns what remote access is currently doing.
func (c *Controller) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

// Enable brings remote access up and returns what it managed to achieve.
//
// It does not return an error. Every failure here is one the user has to be
// told about in words rather than one a caller can usefully retry, so the
// reason travels in State.Reason to the phone and the dashboard alike.
func (c *Controller) Enable(ctx context.Context) State {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.state.Enabled {
		return c.state
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
		return c.state
	}

	boundPort, err := c.srv.StartRemote(cert, fp, c.port)
	if err != nil {
		log.Errorf("Remote listener error: %v", err)
		c.state = State{Reason: fmt.Sprintf("Could not open port %d: %v. "+
			"The local network is unaffected.", c.port, err)}
		return c.state
	}

	next := State{Enabled: true, CertFP: fp, UPnP: UPnPOK}

	mapping, err := c.deps.OpenPort(boundPort)
	if err != nil {
		log.Warnf("UPnP failed: %v — manual port forwarding required (port %d)", err, boundPort)
		next.UPnP = UPnPFailed
		next.ManualPort = boundPort
		next.Reason = fmt.Sprintf("Your router did not accept an automatic port mapping. "+
			"Forward TCP port %d to this machine in the router's admin page.", boundPort)
	} else {
		c.mapping = mapping
		keepCtx, cancel := context.WithCancel(ctx)
		c.keepCancel = cancel
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
		if c.mapping != nil {
			if ip, mapErr := c.mapping.ExternalIP(); mapErr == nil {
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
		c.teardownLocked()
		c.state = State{
			UPnP: UPnPCarrierNAT,
			Reason: fmt.Sprintf("Your ISP puts this connection behind carrier-grade NAT (%s). "+
				"Nothing on this machine or your router can make it reachable from the internet, "+
				"so remote access has been stopped. The local network is unaffected.", publicIP),
		}
		return c.state
	default:
		next.PublicURL = fmt.Sprintf("https://%s:%d", publicIP, boundPort)
	}

	c.state = next
	return c.state
}

// Disable takes remote access down. Safe to call when it is already down.
func (c *Controller) Disable() State {
	c.mu.Lock()
	defer c.mu.Unlock()

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
