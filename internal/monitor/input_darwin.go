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

// activityWatch turns a stream of idle readings into the one question this
// sensor asks: has someone been at this machine long enough to say so.
//
// It reports once and then stays quiet until the machine has been left alone
// again for five seconds, so a person working at the laptop raises one alert
// rather than one every second.
type activityWatch struct {
	threshold int
	busy      int
	quiet     int
	alerted   bool
}

// sample records one idle reading and reports whether it completes an alert.
//
// A negative reading means ioreg could not be asked at all, which is not
// evidence of anyone touching the machine — so it counts as quiet rather than
// as activity.
func (w *activityWatch) sample(idleSeconds float64) bool {
	if idleSeconds < 0 || idleSeconds >= 2 {
		w.busy = 0
		w.quiet++
		if w.alerted && w.quiet >= 5 {
			w.alerted = false
		}
		return false
	}

	w.quiet = 0
	if w.alerted {
		return false
	}
	w.busy++
	if w.busy < w.threshold {
		return false
	}
	w.alerted = true
	w.busy = 0
	return true
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
