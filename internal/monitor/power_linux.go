//go:build linux

package monitor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// PowerSensor monitors the charger/AC power state on Linux.
type PowerSensor struct {
	supplyPath string
	lastOnAC   bool
	initialized bool
}

func NewPowerSensor() *PowerSensor {
	return &PowerSensor{}
}

func (s *PowerSensor) Name() string        { return "power" }
func (s *PowerSensor) DisplayName() string  { return "Power/Charger" }

func (s *PowerSensor) Available() bool {
	path := findPowerSupplyPath()
	if path != "" {
		s.supplyPath = path
		return true
	}
	return false
}

func (s *PowerSensor) Start(ctx context.Context, alerts chan<- Alert) error {
	if s.supplyPath == "" {
		s.supplyPath = findPowerSupplyPath()
		if s.supplyPath == "" {
			return nil
		}
	}

	onAC, err := isACOnline(s.supplyPath)
	if err != nil {
		return err
	}
	s.lastOnAC = onAC
	s.initialized = true

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			onAC, err := isACOnline(s.supplyPath)
			if err != nil {
				continue
			}
			if onAC != s.lastOnAC {
				if !onAC {
					alerts <- Alert{
						Sensor:  "power",
						Level:   AlertCritical,
						Message: "Charger disconnected!",
					}
				} else {
					alerts <- Alert{
						Sensor:  "power",
						Level:   AlertWarning,
						Message: "Charger reconnected",
					}
				}
				s.lastOnAC = onAC
			}
		}
	}
}

func (s *PowerSensor) Stop() error { return nil }

func findPowerSupplyPath() string {
	base := "/sys/class/power_supply"
	entries, err := os.ReadDir(base)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		typePath := filepath.Join(base, entry.Name(), "type")
		data, err := os.ReadFile(typePath)
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(data)) == "Mains" {
			return filepath.Join(base, entry.Name())
		}
	}
	// Fallback: look for battery status
	for _, entry := range entries {
		statusPath := filepath.Join(base, entry.Name(), "status")
		if _, err := os.Stat(statusPath); err == nil {
			return filepath.Join(base, entry.Name())
		}
	}
	return ""
}

func isACOnline(supplyPath string) (bool, error) {
	// Try "online" file first (for AC adapters)
	onlinePath := filepath.Join(supplyPath, "online")
	data, err := os.ReadFile(onlinePath)
	if err == nil {
		return strings.TrimSpace(string(data)) == "1", nil
	}

	// Fallback: check "status" file (for batteries)
	statusPath := filepath.Join(supplyPath, "status")
	data, err = os.ReadFile(statusPath)
	if err != nil {
		return false, err
	}
	status := strings.TrimSpace(string(data))
	return status == "Charging" || status == "Full", nil
}
