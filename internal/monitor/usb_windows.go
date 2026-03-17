//go:build windows

package monitor

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// USBSensor monitors USB device changes on Windows.
type USBSensor struct {
	lastDevices map[string]string // PNPDeviceID -> FriendlyName
}

func NewUSBSensor() *USBSensor {
	return &USBSensor{}
}

func (s *USBSensor) Name() string        { return "usb" }
func (s *USBSensor) DisplayName() string  { return "USB Devices" }
func (s *USBSensor) Available() bool      { return true }

func (s *USBSensor) Start(ctx context.Context, alerts chan<- Alert) error {
	devices, err := listUSBDevicesWindows()
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
			current, err := listUSBDevicesWindows()
			if err != nil {
				continue
			}

			// Check for removed devices
			for id, name := range s.lastDevices {
				if _, exists := current[id]; !exists {
					alerts <- Alert{
						Sensor:  "usb",
						Level:   AlertCritical,
						Message: fmt.Sprintf("USB device removed: %s", name),
					}
				}
			}

			// Check for added devices
			for id, name := range current {
				if _, exists := s.lastDevices[id]; !exists {
					alerts <- Alert{
						Sensor:  "usb",
						Level:   AlertWarning,
						Message: fmt.Sprintf("USB device connected: %s", name),
					}
				}
			}

			s.lastDevices = current
		}
	}
}

func (s *USBSensor) Stop() error { return nil }

func listUSBDevicesWindows() (map[string]string, error) {
	out, err := exec.Command("powershell", "-NoProfile", "-Command",
		`Get-CimInstance Win32_PnPEntity | Where-Object { `+
			`$_.PNPDeviceID -like 'USB\VID_*' -and `+
			`$_.Service -ne 'usbhub' -and `+
			`$_.Service -ne 'usbhub3' -and `+
			`$_.Status -eq 'OK' } | `+
			`ForEach-Object { $_.PNPDeviceID + '|' + $_.Name }`).Output()
	if err != nil {
		return nil, err
	}
	devices := make(map[string]string)
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		id := parts[0]
		name := id
		if len(parts) == 2 && parts[1] != "" {
			name = parts[1]
		}
		devices[id] = name
	}
	return devices, nil
}
