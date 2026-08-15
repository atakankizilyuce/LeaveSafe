//go:build darwin

package monitor

import (
	"context"
	"os/exec"
	"time"
)

// ScreenSensor monitors the display/screen state on macOS.
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
		read:  func(context.Context) (bool, error) { return isScreenOnDarwin() },
		every: 2 * time.Second,
	}
}

func (s *ScreenSensor) Name() string        { return "screen" }
func (s *ScreenSensor) DisplayName() string { return "Screen/Display" }
func (s *ScreenSensor) Available() bool     { return true }

func (s *ScreenSensor) Start(ctx context.Context, alerts chan<- Alert) error {
	// Where the display is now is the baseline, and it is read rather than
	// assumed. Assuming it would put "Screen turned off!" on the phone of anybody
	// who armed a machine that was already that way.
	//
	// A first reading that cannot be taken is reported rather than worked
	// around: the supervisor records the failure and the panel shows the gap,
	// which is the whole difference between a sensor that is not watching and
	// one that says it is.
	s.watch.forget()
	on, err := s.read(ctx)
	if err != nil {
		return err
	}
	s.watch.sample(on)

	ticker := time.NewTicker(s.every)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			on, err := s.read(ctx)
			if err != nil {
				// One unreadable poll is not the hardware moving.
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

func isScreenOnDarwin() (bool, error) {
	out, err := exec.Command("ioreg", "-r", "-d", "1", "-c", "IODisplayWrangler").Output()
	if err != nil {
		return true, err
	}
	return parseDisplayOn(string(out)), nil
}
