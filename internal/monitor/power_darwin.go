//go:build darwin

package monitor

import (
	"context"
	"os/exec"
	"time"
)

// PowerSensor monitors the charger/AC power state on macOS.
type PowerSensor struct {
	watch stateWatch[bool]

	// read is how the charger is asked and every is how often. Both are filled
	// in by the constructor; a test replaces them to drive the loop without the
	// hardware, and without waiting two seconds for every reading.
	read  func(context.Context) (bool, error)
	every time.Duration
}

func NewPowerSensor() *PowerSensor {
	return &PowerSensor{
		read:  func(context.Context) (bool, error) { return isOnACPower() },
		every: 2 * time.Second,
	}
}

func (s *PowerSensor) Name() string        { return "power" }
func (s *PowerSensor) DisplayName() string { return "Power/Charger" }

func (s *PowerSensor) Available() bool {
	_, err := exec.LookPath("pmset")
	return err == nil
}

func (s *PowerSensor) Start(ctx context.Context, alerts chan<- Alert) error {
	return poll{
		every: s.every,
		read:  s.read,
		alert: chargerAlert,
		watch: &s.watch,
	}.run(ctx, alerts)
}

func (s *PowerSensor) Stop() error { return nil }

func isOnACPower() (bool, error) {
	out, err := exec.Command("pmset", "-g", "batt").Output()
	if err != nil {
		return false, err
	}
	return parseOnACPower(string(out)), nil
}
