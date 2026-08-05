//go:build darwin

package monitor

import (
	"context"
	"os/exec"
	"time"
)

// InputSensor detects mouse/keyboard activity on macOS using
// CGEventSourceSecondsSinceLastEventType via the ioreg/hidutil tools.
type InputSensor struct {
	threshold int // consecutive detections needed before alerting
}

func NewInputSensor() *InputSensor { return &InputSensor{threshold: 3} }

func NewInputSensorWithThreshold(n int) *InputSensor {
	if n < 1 {
		n = 1
	}
	return &InputSensor{threshold: n}
}

func (s *InputSensor) Name() string        { return "input" }
func (s *InputSensor) DisplayName() string { return "Mouse/Keyboard" }

func (s *InputSensor) Available() bool {
	// ioreg is always available on macOS
	_, err := exec.LookPath("ioreg")
	return err == nil
}

func (s *InputSensor) Start(ctx context.Context, alerts chan<- Alert) error {
	// Grace period: ignore input for 5 seconds after arming
	select {
	case <-ctx.Done():
		return nil
	case <-time.After(5 * time.Second):
	}

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	watch := activityWatch{threshold: s.threshold}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if watch.sample(getIdleSeconds()) {
				alerts <- Alert{
					Sensor:  "input",
					Level:   AlertCritical,
					Message: "Sustained mouse or keyboard activity detected!",
				}
			}
		}
	}
}

func (s *InputSensor) Stop() error { return nil }

// getIdleSeconds returns the system idle time in seconds using ioreg.
// Returns -1 on error.
func getIdleSeconds() float64 {
	out, err := exec.Command("ioreg", "-c", "IOHIDSystem", "-d", "4", "-S").Output()
	if err != nil {
		return -1
	}
	return parseHIDIdleSeconds(string(out))
}
