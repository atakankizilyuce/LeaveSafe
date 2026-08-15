//go:build windows

package monitor

import (
	"context"
	"fmt"
	"syscall"
	"time"
	"unsafe"
)

var (
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	getSystemPowerStatus = kernel32.NewProc("GetSystemPowerStatus")
)

type systemPowerStatus struct {
	ACLineStatus        byte
	BatteryFlag         byte
	BatteryLifePercent  byte
	SystemStatusFlag    byte
	BatteryLifeTime     uint32
	BatteryFullLifeTime uint32
}

// What GetSystemPowerStatus puts in ACLineStatus. Anything else — 255 is the
// documented one — means Windows would not say, which is not the same as the
// charger being out and must never be reported as it.
const (
	acOffline byte = 0
	acOnline  byte = 1
)

// PowerSensor monitors the charger/AC power state on Windows.
type PowerSensor struct {
	watch stateWatch[bool]

	// read is how the charger is asked and every is how often. Both are filled
	// in by the constructor; a test replaces them to drive the loop without a
	// laptop, and without waiting two seconds for every reading.
	read  func() (bool, error)
	every time.Duration
}

func NewPowerSensor() *PowerSensor {
	return &PowerSensor{read: readOnAC, every: 2 * time.Second}
}

func (s *PowerSensor) Name() string        { return "power" }
func (s *PowerSensor) DisplayName() string { return "Power/Charger" }

func (s *PowerSensor) Available() bool {
	var status systemPowerStatus
	ret, _, _ := getSystemPowerStatus.Call(uintptr(unsafe.Pointer(&status)))
	return ret != 0
}

func (s *PowerSensor) Start(ctx context.Context, alerts chan<- Alert) error {
	// This may be a restart. What the charger did while the loop was down
	// happened on a machine nobody was watching, and is not an event now.
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
				// One unreadable poll is not an event. The next one decides.
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

// readOnAC reports whether the charger is in.
//
// A reading Windows would not answer — 255 is the documented one — is returned
// as an error rather than as "unplugged". It is not evidence of anything, and
// the loop already knows what to do with a poll it could not take: nothing.
func readOnAC() (bool, error) {
	status, err := getPowerStatus()
	if err != nil {
		return false, err
	}
	return onACFrom(status.ACLineStatus)
}

// onACFrom reads what Windows put in ACLineStatus.
func onACFrom(line byte) (bool, error) {
	switch line {
	case acOffline:
		return false, nil
	case acOnline:
		return true, nil
	default:
		return false, fmt.Errorf("Windows would not say whether the charger is in (ACLineStatus %d)", line)
	}
}

func getPowerStatus() (*systemPowerStatus, error) {
	var status systemPowerStatus
	ret, _, err := getSystemPowerStatus.Call(uintptr(unsafe.Pointer(&status)))
	if ret == 0 {
		return nil, fmt.Errorf("GetSystemPowerStatus failed: %w", err)
	}
	return &status, nil
}
