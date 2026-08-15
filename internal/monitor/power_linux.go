//go:build linux

package monitor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// PowerSensor monitors the charger/AC power state on Linux.
type PowerSensor struct {
	supplyPath string
	watch      stateWatch[bool]

	// read is how the charger is asked and every is how often. Both are filled
	// in by the constructor; a test replaces them to drive the loop without the
	// hardware, and without waiting two seconds for every reading.
	read  func() (bool, error)
	every time.Duration
}

func NewPowerSensor() *PowerSensor {
	s := &PowerSensor{every: 2 * time.Second}
	s.read = s.readOnAC
	return s
}

// readOnAC finds the machine's mains supply the first time it is asked, and
// reads it from then on.
//
// A machine with nothing under /sys/class/power_supply is answered with an
// error rather than with "unplugged": there is no charger to watch, and saying
// there is one that has just come out would be an alarm about a desktop.
func (s *PowerSensor) readOnAC() (bool, error) {
	if s.supplyPath == "" {
		s.supplyPath = findPowerSupplyPath()
		if s.supplyPath == "" {
			return false, errors.New("no power supply to watch under /sys/class/power_supply")
		}
	}
	return isACOnline(s.supplyPath)
}

func (s *PowerSensor) Name() string        { return "power" }
func (s *PowerSensor) DisplayName() string { return "Power/Charger" }

func (s *PowerSensor) Available() bool {
	path := findPowerSupplyPath()
	if path != "" {
		s.supplyPath = path
		return true
	}
	return false
}

func (s *PowerSensor) Start(ctx context.Context, alerts chan<- Alert) error {
	// Where the charger is now is the baseline, and it is read rather than
	// assumed. Assuming it would put "Charger disconnected!" on the phone of anybody
	// who armed a machine that was already that way.
	//
	// A first reading that cannot be taken is reported rather than worked
	// around: the supervisor records the failure and the panel shows the gap,
	// which is the whole difference between a sensor that is not watching and
	// one that says it is.
	s.watch.forget()
	onAC, err := s.read()
	if err != nil {
		return err
	}
	s.watch.sample(onAC)

	ticker := time.NewTicker(s.every)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			onAC, err := s.read()
			if err != nil {
				// One unreadable poll is not the hardware moving.
				continue
			}
			if !s.watch.sample(onAC) {
				continue
			}
			if !sendAlert(ctx, alerts, chargerAlert(onAC)) {
				return nil
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
		return parseACOnline(string(data)), nil
	}

	// Fallback: check "status" file (for batteries)
	statusPath := filepath.Join(supplyPath, "status")
	data, err = os.ReadFile(statusPath)
	if err != nil {
		return false, err
	}
	return parseBatteryCharging(string(data)), nil
}
