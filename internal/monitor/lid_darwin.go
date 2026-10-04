//go:build darwin

package monitor

import (
	"context"
	"fmt"
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
	read   func(context.Context) (bool, error)
	every  time.Duration
	events func(context.Context) (<-chan bool, func(), error)
}

func NewLidSensor() *LidSensor {
	return &LidSensor{
		read:   func(context.Context) (bool, error) { return isLidOpenDarwin() },
		every:  2 * time.Second,
		events: subscribeLid,
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
	events, stop, err := s.events(ctx)
	if err != nil {
		return err
	}
	defer stop()
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
		case now, ok := <-events:
			if !ok {
				if ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("lid notifications stopped")
			}
			if !s.report(ctx, alerts, now) {
				return nil
			}
		case <-ticker.C:
			now, err := s.read(ctx)
			if err == nil && !s.report(ctx, alerts, now) {
				return nil
			}
		}
	}
}

func (s *LidSensor) report(ctx context.Context, alerts chan<- Alert, open bool) bool {
	return !s.watch.sample(open) || sendAlert(ctx, alerts, lidAlert(open))
}

func (s *LidSensor) Stop() error { return nil }

func isLidOpenDarwin() (bool, error) {
	out, err := exec.Command("ioreg", "-r", "-k", "AppleClamshellState", "-d", "1").Output()
	if err != nil {
		return true, err
	}
	return parseClamshellOpen(string(out)), nil
}
