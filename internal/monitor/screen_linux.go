//go:build linux

package monitor

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// ScreenSensor monitors the display/screen state on Linux.
type ScreenSensor struct {
	lastOn bool
}

func NewScreenSensor() *ScreenSensor {
	return &ScreenSensor{lastOn: true}
}

func (s *ScreenSensor) Name() string        { return "screen" }
func (s *ScreenSensor) DisplayName() string  { return "Screen/Display" }

func (s *ScreenSensor) Available() bool {
	_, err := exec.LookPath("xset")
	return err == nil
}

func (s *ScreenSensor) Start(ctx context.Context, alerts chan<- Alert) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			on, err := isScreenOnLinux()
			if err != nil {
				continue
			}
			if on != s.lastOn {
				if !on {
					alerts <- Alert{
						Sensor:  "screen",
						Level:   AlertWarning,
						Message: "Screen turned off!",
					}
				} else {
					alerts <- Alert{
						Sensor:  "screen",
						Level:   AlertWarning,
						Message: "Screen turned on",
					}
				}
				s.lastOn = on
			}
		}
	}
}

func (s *ScreenSensor) Stop() error { return nil }

func isScreenOnLinux() (bool, error) {
	out, err := exec.Command("xset", "q").Output()
	if err != nil {
		return true, err
	}
	output := string(out)
	// DPMS: Monitor is On / Off / Standby / Suspend
	if strings.Contains(output, "Monitor is Off") ||
		strings.Contains(output, "Monitor is Standby") ||
		strings.Contains(output, "Monitor is Suspend") {
		return false, nil
	}
	return true, nil
}
