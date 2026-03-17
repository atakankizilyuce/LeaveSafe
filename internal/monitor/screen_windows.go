//go:build windows

package monitor

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// ScreenSensor monitors the display/screen state on Windows.
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
			on, err := isScreenOnWindows()
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

func isScreenOnWindows() (bool, error) {
	// Check if the console display is active via PowerShell
	out, err := exec.Command("powershell", "-Command",
		"[System.Windows.Forms.Screen]::PrimaryScreen -ne $null").Output()
	if err != nil {
		// Fallback: check if session is locked
		out2, err2 := exec.Command("powershell", "-Command",
			"(Get-Process -Name LogonUI -ErrorAction SilentlyContinue) -ne $null").Output()
		if err2 != nil {
			return true, err
		}
		// If LogonUI is running, screen is locked
		return !strings.Contains(strings.TrimSpace(string(out2)), "True"), nil
	}
	return strings.TrimSpace(string(out)) == "True", nil
}
