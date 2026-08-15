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

	// read is how the machine's idle clock is asked, grace is how long after
	// arming its answers are ignored, and every is how often it is asked. All
	// three are filled in by the constructor; a test replaces them to drive the
	// loop without touching a mouse and without sitting through the grace
	// period.
	read  func() uint32
	grace time.Duration
	every time.Duration
}

func NewInputSensor() *InputSensor { return NewInputSensorWithThreshold(3) }

func NewInputSensorWithThreshold(n int) *InputSensor {
	if n < 1 {
		n = 1
	}
	return &InputSensor{
		threshold: n,
		read:      getLastInput,
		grace:     5 * time.Second,
		every:     1 * time.Second,
	}
}

func (s *InputSensor) Name() string        { return "input" }
func (s *InputSensor) DisplayName() string { return "Mouse/Keyboard" }

func (s *InputSensor) Available() bool {
	return getLastInputInfo.Find() == nil
}

func (s *InputSensor) Start(ctx context.Context, alerts chan<- Alert) error {
	baseline := s.read()

	// Grace period after arming: the hand that pressed Arm is still on the
	// machine, and it is not the hand this sensor is looking for.
	select {
	case <-ctx.Done():
		return nil
	case <-time.After(s.grace):
	}

	ticker := time.NewTicker(s.every)
	defer ticker.Stop()

	// The same rule the other two platforms use. Windows answers a tick count
	// rather than a number of idle seconds, so the reading is turned into one
	// before the rule sees it — what must not differ between platforms is when
	// this product decides somebody is at the machine.
	watch := activityWatch{threshold: s.threshold}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			current := s.read()
			touched := current != baseline
			baseline = current

			if !watch.sample(idleFrom(touched)) {
				continue
			}
			if !sendAlert(ctx, alerts, Alert{
				Sensor:  "input",
				Level:   AlertCritical,
				Message: "Sustained mouse or keyboard activity detected!",
			}) {
				return nil
			}
		}
	}
}

// idleFrom turns "did the idle clock move since the last poll" into the number
// of idle seconds activityWatch reads, which is what the other two platforms
// hand it directly.
func idleFrom(touched bool) float64 {
	if touched {
		return 0
	}
	return activityQuietSeconds
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
