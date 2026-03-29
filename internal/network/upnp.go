package network

import (
	"context"
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"
	upnp "gitlab.com/NebulousLabs/go-upnp"
)

const (
	leaseRenewInterval = 30 * time.Minute
	portDescription    = "LeaveSafe"
)

// PortMapping represents an active UPnP port forwarding rule.
type PortMapping struct {
	InternalPort int
	ExternalPort int
	device       *upnp.IGD
}

// OpenPort discovers a UPnP gateway and forwards the given TCP port.
// It returns the mapping or an error if UPnP is unavailable.
func OpenPort(port int) (*PortMapping, error) {
	d, err := upnp.Discover()
	if err != nil {
		return nil, fmt.Errorf("UPnP discovery failed: %w", err)
	}

	if err := d.Forward(uint16(port), portDescription); err != nil {
		return nil, fmt.Errorf("UPnP port forward failed: %w", err)
	}

	log.WithField("port", port).Info("UPnP port mapping created")
	return &PortMapping{
		InternalPort: port,
		ExternalPort: port,
		device:       d,
	}, nil
}

// ExternalIP returns the public IP address reported by the UPnP gateway.
func (pm *PortMapping) ExternalIP() (string, error) {
	return pm.device.ExternalIP()
}

// Close removes the port mapping from the router.
func (pm *PortMapping) Close() error {
	if err := pm.device.Clear(uint16(pm.ExternalPort)); err != nil {
		log.WithError(err).Warn("Failed to remove UPnP port mapping")
		return err
	}
	log.WithField("port", pm.ExternalPort).Info("UPnP port mapping removed")
	return nil
}

// KeepAlive periodically renews the port mapping lease until ctx is cancelled.
func (pm *PortMapping) KeepAlive(ctx context.Context) {
	ticker := time.NewTicker(leaseRenewInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := pm.device.Forward(uint16(pm.ExternalPort), portDescription); err != nil {
				log.WithError(err).Warn("UPnP lease renewal failed")
			} else {
				log.Debug("UPnP lease renewed")
			}
		}
	}
}
