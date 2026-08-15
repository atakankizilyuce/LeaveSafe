//go:build linux

package monitor

import (
	"context"
	"os"
	"time"
)

const lidStatePath = "/proc/acpi/button/lid/LID0/state"

// LidSensor monitors the laptop lid state on Linux.
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
		read:  func(context.Context) (bool, error) { return isLidOpenLinux() },
		every: 2 * time.Second,
	}
}

func (s *LidSensor) Name() string        { return "lid" }
func (s *LidSensor) DisplayName() string { return "Lid State" }

func (s *LidSensor) Available() bool {
	_, err := os.Stat(lidStatePath)
	return err == nil
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

func isLidOpenLinux() (bool, error) {
	data, err := os.ReadFile(lidStatePath)
	if err != nil {
		return true, err
	}
	return parseLidOpen(string(data)), nil
}
