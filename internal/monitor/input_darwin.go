//go:build darwin

package monitor

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// InputSensor detects mouse/keyboard activity on macOS using
// CGEventSourceSecondsSinceLastEventType via the ioreg/hidutil tools.
type InputSensor struct{}

func NewInputSensor() *InputSensor { return &InputSensor{} }

func (s *InputSensor) Name() string        { return "input" }
func (s *InputSensor) DisplayName() string  { return "Mouse/Keyboard" }

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

	alerted := false
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			idle := getIdleSeconds()
			// If idle time is less than 2 seconds, someone is actively using the machine
			if idle >= 0 && idle < 2 && !alerted {
				alerted = true
				alerts <- Alert{
					Sensor:  "input",
					Level:   AlertCritical,
					Message: "Mouse or keyboard activity detected!",
				}
			}
			if idle >= 5 {
				alerted = false
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
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "HIDIdleTime") {
			parts := strings.Split(line, "=")
			if len(parts) < 2 {
				continue
			}
			val := strings.TrimSpace(parts[1])
			ns, err := strconv.ParseInt(val, 10, 64)
			if err != nil {
				continue
			}
			return float64(ns) / 1e9
		}
	}
	return -1
}
