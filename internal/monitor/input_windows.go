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
type InputSensor struct{}

func NewInputSensor() *InputSensor { return &InputSensor{} }

func (s *InputSensor) Name() string        { return "input" }
func (s *InputSensor) DisplayName() string  { return "Mouse/Keyboard" }

func (s *InputSensor) Available() bool {
	return getLastInputInfo.Find() == nil
}

func (s *InputSensor) Start(ctx context.Context, alerts chan<- Alert) error {
	// Record the last input time at arm-time so we only detect NEW input
	baseline := getLastInput()

	// Ignore input during the first 5 seconds (grace period after arming)
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
			current := getLastInput()
			if current != baseline && !alerted {
				alerted = true
				alerts <- Alert{
					Sensor:  "input",
					Level:   AlertCritical,
					Message: "Mouse or keyboard activity detected!",
				}
			}
			// Update baseline so we can detect subsequent activity after dismissal
			if alerted && current != baseline {
				baseline = current
				alerted = false
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
