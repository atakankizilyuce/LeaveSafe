//go:build darwin

package monitor

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// ScreenSensor monitors the display/screen state on macOS.
type ScreenSensor struct {
	lastOn bool
}

func NewScreenSensor() *ScreenSensor {
	return &ScreenSensor{lastOn: true}
}

func (s *ScreenSensor) Name() string        { return "screen" }
func (s *ScreenSensor) DisplayName() string  { return "Screen/Display" }
func (s *ScreenSensor) Available() bool      { return true }

func (s *ScreenSensor) Start(ctx context.Context, alerts chan<- Alert) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			on, err := isScreenOnDarwin()
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

func isScreenOnDarwin() (bool, error) {
	out, err := exec.Command("ioreg", "-r", "-d", "1", "-c", "IODisplayWrangler").Output()
	if err != nil {
		return true, err
	}
	// DevicePowerState 4 = on, 0-3 = dimmed/off
	return !strings.Contains(string(out), `"DevicePowerState" = 0`), nil
}
