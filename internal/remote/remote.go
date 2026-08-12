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
	"crypto/x509"
	"fmt"
	"net"
	"strconv"
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

// Reach says what is known about whether a connection from outside can arrive
// here — as opposed to whether one was arranged for.
//
// The distinction is the point of this type. Everything else in State records
// what this program asked the network for and what the network said back; this
// records what was observed. A router accepting a port mapping and a lookup
// service naming an address are both agreements about intent, and neither is a
// packet completing the journey.
type Reach string

const (
	// ReachUnknown is the state before the question has been asked, and while
	// it is being asked.
	ReachUnknown Reach = ""

	// ReachVerified means a connection was opened to the public address and
	// this machine's own certificate answered it. It is the only value here
	// that is evidence rather than inference.
	ReachVerified Reach = "verified"

	// ReachUnproven means the check did not complete. It is not the same as
	// unreachable: plenty of routers refuse to let a machine inside reach its
	// own public address, and a phone arriving from mobile data takes a
	// different path entirely. Saying "not reachable" here would be reporting a
	// failure nobody observed.
	ReachUnproven Reach = "unproven"

	// ReachBlocked means something on the path makes it impossible, and the
	// something was identified — carrier-grade NAT, or a second router in front
	// of the one holding the mapping.
	ReachBlocked Reach = "blocked"
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
	Reach      Reach     `json:"reach,omitempty"`
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
	WANAddress() (string, error)
	Close() error
	KeepAlive(ctx context.Context)
}

// Deps are the outside-world calls, injected so every failure branch is
// testable without a router, a certificate authority or a network.
type Deps struct {
	Cert     func(configDir string) (tls.Certificate, string, error)
	OpenPort func(port int) (PortMapping, error)
	PublicIP func() (string, error)
	// Verify opens a connection to the public address and reports whether this
	// machine's own certificate answered it. A nil error is the only evidence
	// in this package that remote access works.
	Verify func(addr string, cert *x509.Certificate) error
	// FirewallHint is the command that lets this port in through the host
	// firewall, which is the likeliest reason a verified-looking setup is not
	// reachable. It is a hint printed to the user, never run.
	FirewallHint func(port int) string
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

	// The leaf is what the reachability check needs: it pins the connection to
	// this exact certificate. A certificate that will not parse is not worth
	// refusing to serve over — the listener is already up and working — so the
	// check simply has nothing to pin to and says as much.
	leaf := parseLeaf(cert)

	safe.Go("remote-reachability", func() { c.probe(ctx, gen, boundPort, fp, leaf) })
	return st
}

// probe asks the router for a port mapping and the internet for this machine's
// address, then publishes what it found.
//
// It runs on its own goroutine and can take the better part of a minute. Every
// exit publishes exactly once, through publish, which drops the result if
// remote access has been taken down or brought up again in the meantime.
func (c *Controller) probe(ctx context.Context, gen uint64, boundPort int,
	fp string, leaf *x509.Certificate) {
	next := State{Enabled: true, CertFP: fp, UPnP: UPnPOK}

	mapping, ok := c.mapPort(ctx, gen, boundPort, &next)
	if !ok {
		return
	}

	publicIP := c.publicAddress(mapping)
	if publicIP == "" {
		next.Reason = "No public address could be found, so there is no URL to scan from " +
			"another network. The local network is unaffected."
		c.publish(gen, next)
		return
	}

	// Two ways to find carrier-grade NAT, because it shows up in two places and
	// the second one was missing.
	//
	// This first check catches a lookup that answered with shared space itself,
	// which happens when the query never left the ISP's own network. It is the
	// check that has always been here — and on its own it almost never fires,
	// because the usual case is a lookup that does leave, and what comes back
	// then is the ISP's routable address: an ordinary public address that
	// passes every test while belonging to thousands of subscribers at once.
	if network.IsCarrierGradeNAT(publicIP) {
		log.Warnf("Public address %s is carrier-grade NAT — remote access cannot work here", publicIP)
		c.publishCarrierNAT(gen, publicIP)
		return
	}

	// Which is what the second one is for: ask the router where it sits. Under
	// carrier NAT the 100.64 address is the one the router holds, on the
	// inside, and nobody was asking the router. See network.RouterPlacement.
	if placement := c.placement(mapping, publicIP); placement != network.PlacementEdge {
		if c.reportBlocked(gen, placement, publicIP, boundPort, &next) {
			return
		}
	}

	next.PublicURL = fmt.Sprintf("https://%s:%d", publicIP, boundPort)
	c.checkReachable(&next, publicIP, boundPort, leaf)
	c.publish(gen, next)
}

// parseLeaf is the certificate the reachability check pins to, or nil.
//
// Nil is a perfectly good answer and the reason it is worth having: a
// certificate that cannot be parsed is not a reason to refuse to serve — the
// listener is up and working, and the phone on the local network neither knows
// nor cares. It only means there is nothing to pin a check to, which the check
// reports as having established nothing.
//
// The emptiness test is not decoration. Reaching into the first element of a
// certificate that has none would panic, in Enable, on the one program whose
// whole job is to still be running when somebody walks off with the laptop.
func parseLeaf(cert tls.Certificate) *x509.Certificate {
	if len(cert.Certificate) == 0 {
		log.Warn("The certificate carries no data, so reachability cannot be checked")
		return nil
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		log.Warnf("The certificate could not be parsed, so reachability cannot be checked: %v", err)
		return nil
	}
	return leaf
}

// mapPort asks the router for a port mapping and records it. It reports whether
// the probe should carry on: the one case where it should not is remote access
// having been taken down while the router was thinking.
//
// A router that refuses is not that case. There is still a public address to
// find and still something worth telling the user, so the state is marked and
// the probe continues.
func (c *Controller) mapPort(ctx context.Context, gen uint64, boundPort int, next *State) (PortMapping, bool) {
	mapping, err := c.deps.OpenPort(boundPort)
	if err != nil {
		log.Warnf("UPnP failed: %v — manual port forwarding required (port %d)", err, boundPort)
		next.UPnP = UPnPFailed
		next.ManualPort = boundPort
		next.Reason = fmt.Sprintf("Your router did not accept an automatic port mapping, so nothing "+
			"outside your network can reach this machine yet. Forward TCP port %d to it in the "+
			"router's admin page, or pair over the local network instead.", boundPort)
		return nil, true
	}

	c.mu.Lock()
	if gen != c.gen {
		// Taken down while the router was thinking. The mapping belongs to a
		// listener that is gone, and leaving it behind would leave a hole in
		// the router pointing at nothing.
		c.mu.Unlock()
		_ = mapping.Close()
		return nil, false
	}
	c.mapping = mapping
	keepCtx, cancel := context.WithCancel(ctx)
	c.keepCancel = cancel
	c.mu.Unlock()

	safe.Go("upnp-keepalive", func() { mapping.KeepAlive(keepCtx) })
	return mapping, true
}

// publicAddress is where the internet sees this network from.
//
// Asked of the internet first, and of the router only if that fails. The
// router's answer arrives over unauthenticated SSDP from whatever replied
// fastest on the local network, and this address goes into the QR code with the
// pairing key beside it.
func (c *Controller) publicAddress(mapping PortMapping) string {
	publicIP, err := c.deps.PublicIP()
	if err == nil {
		return publicIP
	}

	log.Warnf("Could not determine public IP: %v", err)
	if mapping == nil {
		return ""
	}
	ip, mapErr := mapping.ExternalIP()
	if mapErr != nil {
		log.Warnf("The router's idea of the public address was not usable: %v", mapErr)
		return ""
	}
	log.Infof("Using the address the router reports, %s, as the public one", ip)
	return ip
}

// placement is where the router holding the mapping sits relative to the
// internet.
//
// A router that would not give a mapping is not asked where it is: without a
// mapping there is nothing on the path either way, and the state already says
// so in words the user can act on.
func (c *Controller) placement(mapping PortMapping, publicIP string) network.Placement {
	if mapping == nil {
		return network.PlacementUnknown
	}
	wan, err := mapping.WANAddress()
	if err != nil {
		log.Warnf("The router would not say what its own address is: %v", err)
		return network.PlacementUnknown
	}
	return network.RouterPlacement(wan, publicIP)
}

// reportBlocked says what a placement other than the edge means, and reports
// whether the probe is finished.
//
// Only carrier-grade NAT ends it. That one is nobody's to fix — not the user's,
// not their router's — so the listener comes down rather than sitting there
// inviting somebody to keep trying port forwards at a problem that is not
// theirs. A second router in front is the user's to fix if they can reach it,
// so that listener stays up and the address stays listed: the moment they
// forward the port on the outer box, it starts working.
func (c *Controller) reportBlocked(gen uint64, placement network.Placement,
	publicIP string, boundPort int, next *State) bool {
	switch placement {
	case network.PlacementCarrier:
		log.Warnf("The router is behind carrier-grade NAT — remote access cannot work here")
		c.publishCarrierNAT(gen, publicIP)
		return true

	case network.PlacementDouble:
		log.Warnf("The router that took the port mapping is itself behind another router")
		next.Reach = ReachBlocked
		next.ManualPort = boundPort
		next.Reason = fmt.Sprintf("The router that accepted the port mapping is itself behind another "+
			"router, so the mapping stops one hop short of the internet. Forward TCP port %d on the "+
			"outer router too — the one holding the %s address — or pair over the local network.",
			boundPort, publicIP)
		return false

	default:
		return false
	}
}

// checkReachable tries to reach the public address from here and records what
// happened.
//
// A success is the only proof in this package that remote access works. A
// failure is not proof of the opposite and is not reported as one: a great many
// routers refuse to let a machine inside reach its own public address, and the
// phone arriving from mobile data never takes that path. So the wording says
// what was and was not established, and names the host firewall — the likeliest
// thing to be silently dropping the connection, and the one thing on this list
// that is on this machine.
func (c *Controller) checkReachable(next *State, publicIP string, boundPort int, leaf *x509.Certificate) {
	if c.deps.Verify == nil {
		return
	}

	addr := net.JoinHostPort(publicIP, strconv.Itoa(boundPort))
	err := c.deps.Verify(addr, leaf)
	if err == nil {
		// Evidence outranks everything inferred before it, including a reason
		// already written. A router that looked like it was behind another one,
		// or refused a mapping that somebody had forwarded by hand years ago,
		// is a router that has just been watched carrying a connection — and
		// leaving the earlier explanation on screen would be telling the user
		// to go and fix something that demonstrably works.
		log.Infof("Remote access verified: %s answered with this machine's certificate", addr)
		next.Reach = ReachVerified
		next.Reason = ""
		return
	}

	log.Warnf("Could not confirm that %s is reachable from outside: %v", addr, err)

	// A check that proved nothing does not get to overwrite something that was
	// established. Being behind a second router is a specific finding with a
	// specific fix; replacing it with "this could not be confirmed" would trade
	// an answer for a shrug.
	if next.Reach == ReachBlocked {
		return
	}
	next.Reach = ReachUnproven
	next.Reason = fmt.Sprintf("The port is mapped and there is a public address, but nothing could be "+
		"confirmed to reach this machine from outside — so this address may or may not work from "+
		"mobile data. Many routers simply refuse this check from inside the network, in which case "+
		"there is nothing wrong. If a phone cannot connect, the likeliest cause is this machine's own "+
		"firewall blocking TCP port %d: %s", boundPort, c.firewallHint(boundPort))
}

// firewallHint is the command that opens the port on this platform, or an empty
// note when there is nothing useful to say.
func (c *Controller) firewallHint(boundPort int) string {
	if c.deps.FirewallHint == nil {
		return "allow it in the firewall's settings."
	}
	return c.deps.FirewallHint(boundPort)
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
		UPnP:  UPnPCarrierNAT,
		Reach: ReachBlocked,
		Reason: fmt.Sprintf("Your ISP puts this connection behind carrier-grade NAT, so the address "+
			"the internet sees (%s) is shared and is not yours to be reached at. Nothing on this "+
			"machine or your router can change that, so remote access has been stopped. The local "+
			"network is unaffected.", publicIP),
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
