//go:build darwin

package monitor

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// LidSensor monitors the laptop lid state on macOS.
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
	out, err := exec.Command("ioreg", "-r", "-k", "AppleClamshellState", "-d", "1").Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "AppleClamshellState")
}

func (s *LidSensor) Start(ctx context.Context, alerts chan<- Alert) error {
	open, err := isLidOpenDarwin()
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
			open, err := isLidOpenDarwin()
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

func isLidOpenDarwin() (bool, error) {
	out, err := exec.Command("ioreg", "-r", "-k", "AppleClamshellState", "-d", "1").Output()
	if err != nil {
		return true, err
	}
	// AppleClamshellState = No means lid is open
	return strings.Contains(string(out), `"AppleClamshellState" = No`), nil
}
