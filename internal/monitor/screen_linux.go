//go:build linux

package monitor

import (
	"context"
	"os/exec"
	"time"
)

// ScreenSensor monitors the display/screen state on Linux.
type ScreenSensor struct {
	watch stateWatch[bool]

	// read is how the display is asked and every is how often. Both are filled
	// in by the constructor; a test replaces them to drive the loop without the
	// hardware, and without waiting two seconds for every reading.
	read  func(context.Context) (bool, error)
	every time.Duration
}

func NewScreenSensor() *ScreenSensor {
	return &ScreenSensor{
		read:  func(context.Context) (bool, error) { return isScreenOnLinux() },
		every: 2 * time.Second,
	}
}

func (s *ScreenSensor) Name() string        { return "screen" }
func (s *ScreenSensor) DisplayName() string { return "Screen/Display" }

func (s *ScreenSensor) Available() bool {
	_, err := exec.LookPath("xset")
	return err == nil
}

func (s *ScreenSensor) Start(ctx context.Context, alerts chan<- Alert) error {
	return poll{
		every: s.every,
		read:  s.read,
		alert: screenAlert,
		watch: &s.watch,
	}.run(ctx, alerts)
}

func (s *ScreenSensor) Stop() error { return nil }

func isScreenOnLinux() (bool, error) {
	out, err := exec.Command("xset", "q").Output()
	if err != nil {
		return true, err
	}
	return parseDPMSOn(string(out)), nil
}
