//go:build linux

package monitor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const usbDevicesPath = "/sys/bus/usb/devices"

// USBSensor monitors USB device changes on Linux.
type USBSensor struct {
	lastDevices map[string]bool
}

func NewUSBSensor() *USBSensor {
	return &USBSensor{}
}

func (s *USBSensor) Name() string        { return "usb" }
func (s *USBSensor) DisplayName() string  { return "USB Devices" }

func (s *USBSensor) Available() bool {
	_, err := os.Stat(usbDevicesPath)
	return err == nil
}

func (s *USBSensor) Start(ctx context.Context, alerts chan<- Alert) error {
	devices, err := listUSBDevicesLinux()
	if err != nil {
		return err
	}
	s.lastDevices = devices

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			current, err := listUSBDevicesLinux()
			if err != nil {
				continue
			}

			for dev := range s.lastDevices {
				if !current[dev] {
					alerts <- Alert{
						Sensor:  "usb",
						Level:   AlertCritical,
						Message: fmt.Sprintf("USB device removed: %s", dev),
					}
				}
			}

			for dev := range current {
				if !s.lastDevices[dev] {
					alerts <- Alert{
						Sensor:  "usb",
						Level:   AlertWarning,
						Message: fmt.Sprintf("USB device connected: %s", dev),
					}
				}
			}

			s.lastDevices = current
		}
	}
}

func (s *USBSensor) Stop() error { return nil }

func listUSBDevicesLinux() (map[string]bool, error) {
	entries, err := os.ReadDir(usbDevicesPath)
	if err != nil {
		return nil, err
	}

	devices := make(map[string]bool)
	for _, entry := range entries {
		productPath := filepath.Join(usbDevicesPath, entry.Name(), "product")
		data, err := os.ReadFile(productPath)
		if err != nil {
			continue
		}
		name := strings.TrimSpace(string(data))
		if name != "" {
			devices[entry.Name()+"="+name] = true
		}
	}
	return devices, nil
}
