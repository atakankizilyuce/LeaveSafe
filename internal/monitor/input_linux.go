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
type InputSensor struct{}

func NewInputSensor() *InputSensor { return &InputSensor{} }

func (s *InputSensor) Name() string        { return "input" }
func (s *InputSensor) DisplayName() string  { return "Mouse/Keyboard" }

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

	alerted := false
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			current := inputSnapshot()
			if current != baseline && !alerted {
				alerted = true
				alerts <- Alert{
					Sensor:  "input",
					Level:   AlertCritical,
					Message: "Mouse or keyboard activity detected!",
				}
			}
			if alerted && current != baseline {
				baseline = current
				alerted = false
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
