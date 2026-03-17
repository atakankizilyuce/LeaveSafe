//go:build windows

package monitor

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// LidSensor monitors the laptop lid state on Windows.
type LidSensor struct {
	lastOpen bool
	initialized bool
}

func NewLidSensor() *LidSensor {
	return &LidSensor{}
}

func (s *LidSensor) Name() string        { return "lid" }
func (s *LidSensor) DisplayName() string  { return "Lid State" }

func (s *LidSensor) Available() bool {
	// Check if this is a laptop by querying battery info
	out, err := exec.Command("powershell", "-Command",
		"(Get-WmiObject -Class Win32_Battery).Count").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) != "0" && strings.TrimSpace(string(out)) != ""
}

func (s *LidSensor) Start(ctx context.Context, alerts chan<- Alert) error {
	s.lastOpen = true
	s.initialized = true

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			open, err := isLidOpenWindows()
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

func isLidOpenWindows() (bool, error) {
	out, err := exec.Command("powershell", "-Command",
		"(Get-WmiObject -Namespace root/WMI -Class MSAcpi_LidStatus).LidStatus").Output()
	if err != nil {
		return true, err // Assume open if we can't determine
	}
	status := strings.TrimSpace(strings.ToLower(string(out)))
	return status == "true" || status == "1", nil
}
