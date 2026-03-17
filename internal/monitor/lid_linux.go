//go:build linux

package monitor

import (
	"context"
	"os"
	"strings"
	"time"
)

const lidStatePath = "/proc/acpi/button/lid/LID0/state"

// LidSensor monitors the laptop lid state on Linux.
type LidSensor struct {
	lastOpen    bool
	initialized bool
}

func NewLidSensor() *LidSensor {
	return &LidSensor{}
}

func (s *LidSensor) Name() string        { return "lid" }
func (s *LidSensor) DisplayName() string  { return "Lid State" }

func (s *LidSensor) Available() bool {
	_, err := os.Stat(lidStatePath)
	return err == nil
}

func (s *LidSensor) Start(ctx context.Context, alerts chan<- Alert) error {
	open, err := isLidOpenLinux()
	if err != nil {
		return err
	}
	s.lastOpen = open
	s.initialized = true

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			open, err := isLidOpenLinux()
			if err != nil {
				continue
			}
			if open != s.lastOpen {
				if !open {
					alerts <- Alert{
						Sensor:  "lid",
						Level:   AlertCritical,
						Message: "Lid closed!",
					}
				} else {
					alerts <- Alert{
						Sensor:  "lid",
						Level:   AlertWarning,
						Message: "Lid opened",
					}
				}
				s.lastOpen = open
			}
		}
	}
}

func (s *LidSensor) Stop() error { return nil }

func isLidOpenLinux() (bool, error) {
	data, err := os.ReadFile(lidStatePath)
	if err != nil {
		return true, err
	}
	return strings.Contains(string(data), "open"), nil
}
