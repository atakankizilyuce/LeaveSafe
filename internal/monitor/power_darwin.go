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
	read  func() (bool, error)
	every time.Duration
}

func NewPowerSensor() *PowerSensor {
	return &PowerSensor{read: isOnACPower, every: 2 * time.Second}
}

func (s *PowerSensor) Name() string        { return "power" }
func (s *PowerSensor) DisplayName() string { return "Power/Charger" }

func (s *PowerSensor) Available() bool {
	_, err := exec.LookPath("pmset")
	return err == nil
}

func (s *PowerSensor) Start(ctx context.Context, alerts chan<- Alert) error {
	// Where the charger is now is the baseline, and it is read rather than
	// assumed. Assuming it would put "Charger disconnected!" on the phone of anybody
	// who armed a machine that was already that way.
	//
	// A first reading that cannot be taken is reported rather than worked
	// around: the supervisor records the failure and the panel shows the gap,
	// which is the whole difference between a sensor that is not watching and
	// one that says it is.
	s.watch.forget()
	onAC, err := s.read()
	if err != nil {
		return err
	}
	s.watch.sample(onAC)

	ticker := time.NewTicker(s.every)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			onAC, err := s.read()
			if err != nil {
				// One unreadable poll is not the hardware moving.
				continue
			}
			if !s.watch.sample(onAC) {
				continue
			}
			if !sendAlert(ctx, alerts, chargerAlert(onAC)) {
				return nil
			}
		}
	}
}
func (s *PowerSensor) Stop() error { return nil }

func isOnACPower() (bool, error) {
	out, err := exec.Command("pmset", "-g", "batt").Output()
	if err != nil {
		return false, err
	}
	return parseOnACPower(string(out)), nil
}
