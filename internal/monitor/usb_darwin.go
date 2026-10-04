//go:build darwin

package monitor

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// USBSensor monitors USB device changes on macOS.
type USBSensor struct {
	lastHash        string
	lastDeviceNames []string
	read            func() (string, []string, error)
	every           time.Duration
}

func NewUSBSensor() *USBSensor {
	return &USBSensor{read: getUSBSnapshotDarwin, every: 3 * time.Second}
}

func (s *USBSensor) Name() string        { return "usb" }
func (s *USBSensor) DisplayName() string { return "USB Devices" }

func (s *USBSensor) Available() bool {
	_, err := exec.LookPath("system_profiler")
	return err == nil
}

func (s *USBSensor) Start(ctx context.Context, alerts chan<- Alert) error {
	hash, names, err := s.read()
	if err != nil {
		return err
	}
	s.lastHash = hash
	s.lastDeviceNames = names

	ticker := time.NewTicker(s.every)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			hash, names, err := s.read()
			if err != nil {
				continue
			}
			if hash != s.lastHash {
				if !sendAlert(ctx, alerts, Alert{
					Sensor: "usb", Level: AlertCritical,
					Message: "USB device configuration changed!",
				}) {
					return nil
				}
				s.lastHash = hash
				s.lastDeviceNames = names
			}
		}
	}
}

func (s *USBSensor) Stop() error { return nil }

func getUSBSnapshotDarwin() (string, []string, error) {
	return usbSnapshotDarwin(func(kind string) ([]byte, error) {
		return exec.Command("/usr/sbin/system_profiler", kind, "-detailLevel", "mini").Output()
	})
}

// macOS 26 renamed the data type to SPUSBHostDataType. An unsupported
// type exits successfully with no output; hashing that would watch nothing.
func usbSnapshotDarwin(query func(string) ([]byte, error)) (string, []string, error) {
	out, err := query("SPUSBDataType")
	if err != nil {
		return "", nil, err
	}
	if strings.TrimSpace(string(out)) == "" {
		out, err = query("SPUSBHostDataType")
		if err != nil {
			return "", nil, err
		}
		if strings.TrimSpace(string(out)) == "" {
			return "", nil, fmt.Errorf("USB profiler returned no data for either supported data type")
		}
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(out))
	return hash, parseUSBDeviceNames(string(out)), nil
}
