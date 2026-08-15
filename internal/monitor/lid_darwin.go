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
	// Where the lid is now is the baseline, and it is read rather than
	// assumed. Assuming it would put "Lid closed!" on the phone of anybody
	// who armed a machine that was already that way.
	//
	// A first reading that cannot be taken is reported rather than worked
	// around: the supervisor records the failure and the panel shows the gap,
	// which is the whole difference between a sensor that is not watching and
	// one that says it is.
	s.watch.forget()
	open, err := s.read(ctx)
	if err != nil {
		return err
	}
	s.watch.sample(open)

	ticker := time.NewTicker(s.every)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			open, err := s.read(ctx)
			if err != nil {
				// One unreadable poll is not the hardware moving.
				continue
			}
			if !s.watch.sample(open) {
				continue
			}
			if !sendAlert(ctx, alerts, lidAlert(open)) {
				return nil
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
	return parseClamshellOpen(string(out)), nil
}
