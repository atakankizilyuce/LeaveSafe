//go:build darwin

package monitor

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// PowerSensor monitors the charger/AC power state on macOS.
type PowerSensor struct {
	lastOnAC bool
	initialized bool
}

func NewPowerSensor() *PowerSensor {
	return &PowerSensor{}
}

func (s *PowerSensor) Name() string        { return "power" }
func (s *PowerSensor) DisplayName() string  { return "Power/Charger" }

func (s *PowerSensor) Available() bool {
	_, err := exec.LookPath("pmset")
	return err == nil
}

func (s *PowerSensor) Start(ctx context.Context, alerts chan<- Alert) error {
	onAC, err := isOnACPower()
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
			onAC, err := isOnACPower()
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

func isOnACPower() (bool, error) {
	out, err := exec.Command("pmset", "-g", "batt").Output()
	if err != nil {
		return false, err
	}
	output := string(out)
	return strings.Contains(output, "'AC Power'"), nil
}
