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
	read  func(context.Context) (bool, error)
	every time.Duration
}

func NewPowerSensor() *PowerSensor {
	return &PowerSensor{
		read:  func(context.Context) (bool, error) { return readOnAC() },
		every: 2 * time.Second,
	}
}

func (s *PowerSensor) Name() string        { return "power" }
func (s *PowerSensor) DisplayName() string { return "Power/Charger" }

func (s *PowerSensor) Available() bool {
	var status systemPowerStatus
	ret, _, _ := getSystemPowerStatus.Call(uintptr(unsafe.Pointer(&status)))
	return ret != 0
}

func (s *PowerSensor) Start(ctx context.Context, alerts chan<- Alert) error {
	return poll{
		every: s.every,
		read:  s.read,
		alert: chargerAlert,
		watch: &s.watch,
	}.run(ctx, alerts)
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
		return false, fmt.Errorf("the charger's state was not reported (ACLineStatus %d)", line)
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
