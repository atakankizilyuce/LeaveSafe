//go:build windows

package monitor

import (
	"context"
	"strings"
	"time"
)

// ScreenSensor monitors the display/screen state on Windows.
type ScreenSensor struct {
	watch stateWatch[bool]

	// read is how the display is asked and every is how often. Both are filled
	// in by the constructor; a test replaces them to drive the loop without a
	// display, and without waiting two seconds for every reading.
	read  func(context.Context) (bool, error)
	every time.Duration
}

func NewScreenSensor() *ScreenSensor {
	return &ScreenSensor{read: isScreenOnWindows, every: 2 * time.Second}
}

func (s *ScreenSensor) Name() string        { return "screen" }
func (s *ScreenSensor) DisplayName() string { return "Screen/Display" }
func (s *ScreenSensor) Available() bool     { return true }

func (s *ScreenSensor) Start(ctx context.Context, alerts chan<- Alert) error {
	// Read rather than assumed, for the same reason as the lid: a screen that
	// had already gone to sleep before the machine was armed is not somebody
	// turning it off.
	s.watch.forget()
	if on, err := s.read(ctx); err == nil {
		s.watch.sample(on)
	}

	ticker := time.NewTicker(s.every)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			on, err := s.read(ctx)
			if err != nil {
				continue
			}
			if !s.watch.sample(on) {
				continue
			}
			if !sendAlert(ctx, alerts, screenAlert(on)) {
				return nil
			}
		}
	}
}

func (s *ScreenSensor) Stop() error { return nil }

// isScreenOnWindows reports whether the display is awake. ctx is the sensor's
// own context, so disarming stops the poll rather than waiting on a query that
// may not return.
func isScreenOnWindows(ctx context.Context) (bool, error) {
	// Check if the console display is active via PowerShell
	out, err := powershellOutput(ctx, pollTimeout,
		"[System.Windows.Forms.Screen]::PrimaryScreen -ne $null")
	if err != nil {
		// Fallback: check if session is locked
		out2, err2 := powershellOutput(ctx, pollTimeout,
			"(Get-Process -Name LogonUI -ErrorAction SilentlyContinue) -ne $null")
		if err2 != nil {
			return true, err
		}
		// If LogonUI is running, screen is locked
		return !parseLogonUIPresent(string(out2)), nil
	}
	return strings.TrimSpace(string(out)) == "True", nil
}
