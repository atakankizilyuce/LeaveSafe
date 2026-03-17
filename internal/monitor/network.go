package monitor

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"
)

// NetworkSensor monitors network interface changes (IP address changes, disconnections).
type NetworkSensor struct {
	lastSnapshot string
}

func NewNetworkSensor() *NetworkSensor {
	return &NetworkSensor{}
}

func (s *NetworkSensor) Name() string        { return "network" }
func (s *NetworkSensor) DisplayName() string  { return "IP Address Change" }
func (s *NetworkSensor) Available() bool      { return true }

func (s *NetworkSensor) Start(ctx context.Context, alerts chan<- Alert) error {
	s.lastSnapshot = networkSnapshot()

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			current := networkSnapshot()
			if current != s.lastSnapshot {
				alerts <- Alert{
					Sensor:  "network",
					Level:   AlertWarning,
					Message: fmt.Sprintf("IP address changed (possible network switch). IPs: %s", current),
				}
				s.lastSnapshot = current
			}
		}
	}
}

func (s *NetworkSensor) Stop() error { return nil }

// networkSnapshot creates a string representation of current network state.
func networkSnapshot() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "error"
	}

	var ips []string
	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() && ipNet.IP.To4() != nil {
			ips = append(ips, ipNet.IP.String())
		}
	}
	sort.Strings(ips)
	return strings.Join(ips, ",")
}
