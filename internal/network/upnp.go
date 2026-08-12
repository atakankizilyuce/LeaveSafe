package network

import (
	"context"
	"fmt"
	"math"
	"net"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	upnp "gitlab.com/NebulousLabs/go-upnp"
)

const (
	leaseRenewInterval = 30 * time.Minute
	portDescription    = "LeaveSafe"
)

// gateway is the part of a UPnP device this package uses.
//
// It is an interface so that what this file decides can be tested without a
// router answering on the network: which addresses are fit to hand a phone,
// where the router sits, what a renewal does when it fails. *upnp.IGD is the
// only implementation that ships.
type gateway interface {
	Forward(port uint16, desc string) error
	Clear(port uint16) error
	ExternalIP() (string, error)
}

// PortMapping represents an active UPnP port forwarding rule.
type PortMapping struct {
	InternalPort uint16
	ExternalPort uint16
	device       gateway
}

// OpenPort discovers a UPnP gateway and forwards the given TCP port.
// It returns the mapping or an error if UPnP is unavailable.
func OpenPort(port int) (*PortMapping, error) {
	if port < 1 || port > math.MaxUint16 {
		return nil, fmt.Errorf("invalid port %d", port)
	}
	p := uint16(port) // #nosec G115 -- range checked directly above

	d, err := upnp.Discover()
	if err != nil {
		return nil, fmt.Errorf("UPnP discovery failed: %w", err)
	}

	if err := d.Forward(p, portDescription); err != nil {
		return nil, fmt.Errorf("UPnP port forward failed: %w", err)
	}

	log.WithField("port", port).Info("UPnP port mapping created")
	return &PortMapping{
		InternalPort: p,
		ExternalPort: p,
		device:       d,
	}, nil
}

// ExternalIP returns the public IP address reported by the UPnP gateway.
//
// The gateway is whatever answered an unauthenticated SSDP multicast, and this
// address is not decoration: it goes to the front of the URL list, and the
// dashboard renders the first of those as a QR code with the pairing key in its
// fragment. Whoever chooses this string chooses where the owner's phone tries
// to pair — and the key goes with it. A machine on the café Wi-Fi that answers
// the discovery before the real router does gets to choose it.
//
// The certificate check does not save that case: the page doing the checking
// would be the attacker's own. So the claim is held to what a public address
// can look like. Private, loopback, link-local and unspecified addresses are
// refused along with anything that is not an IP at all — none of them is
// reachable from the internet, so none is an honest answer to this question,
// and every one of them would aim the phone somewhere on the local network.
//
// This does not make the router trustworthy; it takes away the easy answers.
// Preferring the independently discovered address is the other half, and that
// is the caller's to do.
func (pm *PortMapping) ExternalIP() (string, error) {
	raw, err := pm.device.ExternalIP()
	if err != nil {
		return "", err
	}
	return publicAddr(raw)
}

// WANAddress is whatever the gateway says its own external address is, with
// none of the filtering ExternalIP applies.
//
// The two exist separately because they answer different questions. ExternalIP
// is asked "what address should the owner's phone be pointed at", and a private
// or shared-space answer to that is not an answer at all. This one is asked
// "where does this router sit", and a private or shared-space answer is the
// whole point — it is how carrier-grade NAT and a second router are told from a
// network with nothing in between. See RouterPlacement.
//
// Nothing from here ever reaches a URL, so the trust that ExternalIP has to
// withhold does not arise.
func (pm *PortMapping) WANAddress() (string, error) {
	raw, err := pm.device.ExternalIP()
	if err != nil {
		return "", err
	}
	ip := net.ParseIP(strings.TrimSpace(raw))
	if ip == nil {
		return "", fmt.Errorf("the gateway reported %q as its own address, which is not an IP address", raw)
	}
	return ip.String(), nil
}

// publicAddr returns raw as a canonical IP string if it could be this machine's
// address on the internet, and an error otherwise.
func publicAddr(raw string) (string, error) {
	ip := net.ParseIP(strings.TrimSpace(raw))
	if ip == nil {
		return "", fmt.Errorf("the gateway reported %q as this machine's public address, which is not an IP address", raw)
	}
	if !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return "", fmt.Errorf("the gateway reported %s as this machine's public address, which is not a public one", ip)
	}
	return ip.String(), nil
}

// Close removes the port mapping from the router.
func (pm *PortMapping) Close() error {
	if err := pm.device.Clear(pm.ExternalPort); err != nil {
		log.WithError(err).Warn("Failed to remove UPnP port mapping")
		return err
	}
	log.WithField("port", pm.ExternalPort).Info("UPnP port mapping removed")
	return nil
}

// KeepAlive periodically renews the port mapping lease until ctx is canceled.
func (pm *PortMapping) KeepAlive(ctx context.Context) {
	ticker := time.NewTicker(leaseRenewInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := pm.device.Forward(pm.ExternalPort, portDescription); err != nil {
				log.WithError(err).Warn("UPnP lease renewal failed")
			} else {
				log.Debug("UPnP lease renewed")
			}
		}
	}
}
