//go:build windows

package monitor

import (
	"context"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32           = syscall.NewLazyDLL("user32.dll")
	getLastInputInfo = user32.NewProc("GetLastInputInfo")
)

type lastInputInfoT struct {
	cbSize uint32
	dwTime uint32
}

// InputSensor detects mouse/keyboard activity on Windows.
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
	return getLastInputInfo.Find() == nil
}

func (s *InputSensor) Start(ctx context.Context, alerts chan<- Alert) error {
	baseline := getLastInput()

	// Grace period after arming
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
			current := getLastInput()
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

func getLastInput() uint32 {
	info := lastInputInfoT{cbSize: uint32(unsafe.Sizeof(lastInputInfoT{}))}
	ret, _, _ := getLastInputInfo.Call(uintptr(unsafe.Pointer(&info)))
	if ret == 0 {
		return 0
	}
	return info.dwTime
}
