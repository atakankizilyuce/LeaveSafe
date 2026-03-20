//go:build linux

package monitor

import (
	"context"
	"os"
	"path/filepath"
	"time"
)

// InputSensor detects mouse/keyboard activity on Linux
// by monitoring /dev/input/event* modification times.
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

func (s *InputSensor) Name() string       { return "input" }
func (s *InputSensor) DisplayName() string { return "Mouse/Keyboard" }

func (s *InputSensor) Available() bool {
	matches, _ := filepath.Glob("/dev/input/event*")
	return len(matches) > 0
}

func (s *InputSensor) Start(ctx context.Context, alerts chan<- Alert) error {
	baseline := inputSnapshot()

	// Grace period: ignore input for 5 seconds after arming
	select {
	case <-ctx.Done():
		return nil
	case <-time.After(5 * time.Second):
	}

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	consecutiveCount := 0
	alerted := false
	idleCount := 0

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			current := inputSnapshot()
			if current != baseline {
				baseline = current
				idleCount = 0
				if !alerted {
					consecutiveCount++
					if consecutiveCount >= s.threshold {
						alerted = true
						consecutiveCount = 0
						alerts <- Alert{
							Sensor:  "input",
							Level:   AlertCritical,
							Message: "Sustained mouse or keyboard activity detected!",
						}
					}
				}
			} else {
				consecutiveCount = 0
				idleCount++
				if alerted && idleCount >= 5 {
					alerted = false
				}
			}
		}
	}
}

func (s *InputSensor) Stop() error { return nil }

// inputSnapshot returns the latest modification time across /dev/input/event* devices.
func inputSnapshot() int64 {
	matches, _ := filepath.Glob("/dev/input/event*")
	var latest int64
	for _, path := range matches {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		t := info.ModTime().UnixNano()
		if t > latest {
			latest = t
		}
	}
	return latest
}
