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
	watch stateWatch[bool]

	// read is how the lid is asked and every is how often. Both are filled
	// in by the constructor; a test replaces them to drive the loop without the
	// hardware, and without waiting two seconds for every reading.
	read  func(context.Context) (bool, error)
	every time.Duration
}

func NewLidSensor() *LidSensor {
	return &LidSensor{
		read:  func(context.Context) (bool, error) { return isLidOpenDarwin() },
		every: 2 * time.Second,
	}
}

func (s *LidSensor) Name() string        { return "lid" }
func (s *LidSensor) DisplayName() string { return "Lid State" }

func (s *LidSensor) Available() bool {
	out, err := exec.Command("ioreg", "-r", "-k", "AppleClamshellState", "-d", "1").Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "AppleClamshellState")
}

func (s *LidSensor) Start(ctx context.Context, alerts chan<- Alert) error {
	return poll{
		every: s.every,
		read:  s.read,
		alert: lidAlert,
		watch: &s.watch,
	}.run(ctx, alerts)
}

func (s *LidSensor) Stop() error { return nil }

func isLidOpenDarwin() (bool, error) {
	out, err := exec.Command("ioreg", "-r", "-k", "AppleClamshellState", "-d", "1").Output()
	if err != nil {
		return true, err
	}
	return parseClamshellOpen(string(out)), nil
}
