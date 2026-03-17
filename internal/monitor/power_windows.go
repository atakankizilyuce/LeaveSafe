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
	kernel32              = syscall.NewLazyDLL("kernel32.dll")
	getSystemPowerStatus  = kernel32.NewProc("GetSystemPowerStatus")
)

type systemPowerStatus struct {
	ACLineStatus        byte
	BatteryFlag         byte
	BatteryLifePercent  byte
	SystemStatusFlag    byte
	BatteryLifeTime     uint32
	BatteryFullLifeTime uint32
}

// PowerSensor monitors the charger/AC power state on Windows.
type PowerSensor struct {
	lastACState byte
}

func NewPowerSensor() *PowerSensor {
	return &PowerSensor{lastACState: 255} // 255 = unknown initial state
}

func (s *PowerSensor) Name() string        { return "power" }
func (s *PowerSensor) DisplayName() string  { return "Power/Charger" }

func (s *PowerSensor) Available() bool {
	var status systemPowerStatus
	ret, _, _ := getSystemPowerStatus.Call(uintptr(unsafe.Pointer(&status)))
	return ret != 0
}

func (s *PowerSensor) Start(ctx context.Context, alerts chan<- Alert) error {
	// Get initial state
	status, err := getPowerStatus()
	if err != nil {
		return err
	}
	s.lastACState = status.ACLineStatus

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			status, err := getPowerStatus()
			if err != nil {
				continue
			}
			if status.ACLineStatus != s.lastACState {
				if status.ACLineStatus == 0 {
					alerts <- Alert{
						Sensor:  "power",
						Level:   AlertCritical,
						Message: "Charger disconnected!",
					}
				} else if status.ACLineStatus == 1 {
					alerts <- Alert{
						Sensor:  "power",
						Level:   AlertWarning,
						Message: "Charger reconnected",
					}
				}
				s.lastACState = status.ACLineStatus
			}
		}
	}
}

func (s *PowerSensor) Stop() error { return nil }

func getPowerStatus() (*systemPowerStatus, error) {
	var status systemPowerStatus
	ret, _, err := getSystemPowerStatus.Call(uintptr(unsafe.Pointer(&status)))
	if ret == 0 {
		return nil, fmt.Errorf("GetSystemPowerStatus failed: %v", err)
	}
	return &status, nil
}
