//go:build windows

package monitor

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// USBSensor monitors USB device changes on Windows using WMI event subscriptions.
// Unlike polling, WMI events fire within ~1 second of the hardware change.
type USBSensor struct{}

func NewUSBSensor() *USBSensor { return &USBSensor{} }

func (s *USBSensor) Name() string        { return "usb" }
func (s *USBSensor) DisplayName() string { return "USB Devices" }
func (s *USBSensor) Available() bool     { return true }
func (s *USBSensor) Stop() error         { return nil }

// Start launches a long-running PowerShell process that subscribes to WMI
// device-arrival and device-removal events and prints one line per event.
// This gives near-instant detection without WMI polling overhead.
func (s *USBSensor) Start(ctx context.Context, alerts chan<- Alert) error {
	// WITHIN 1 → WMI polls its own device tree at 1-second intervals.
	// We skip usbhub / usbhub3 (USB hub controllers) to avoid noise.
	// TargetInstance.PNPDeviceID LIKE 'USB\\VID_%' matches all USB devices
	// whose hardware ID starts with USB\VID_ (real peripheral devices).
	const script = `
$rmQ = "SELECT * FROM __InstanceDeletionEvent WITHIN 1 WHERE TargetInstance ISA 'Win32_PnPEntity' AND TargetInstance.PNPDeviceID LIKE 'USB\\VID_%'"
$adQ = "SELECT * FROM __InstanceCreationEvent WITHIN 1 WHERE TargetInstance ISA 'Win32_PnPEntity' AND TargetInstance.PNPDeviceID LIKE 'USB\\VID_%'"
Register-WmiEvent -Query $rmQ -SourceIdentifier 'R' | Out-Null
Register-WmiEvent -Query $adQ -SourceIdentifier 'A' | Out-Null
while ($true) {
    $e = Wait-Event -Timeout 5
    if ($null -eq $e) { continue }
    $t = $e.SourceEventArgs.NewEvent.TargetInstance
    if ($null -ne $t) {
        $svc = [string]$t.Service
        if ($svc -ne 'usbhub' -and $svc -ne 'usbhub3') {
            $name = if ($t.Name) { $t.Name } else { $t.PNPDeviceID }
            [Console]::WriteLine($e.SourceIdentifier + '|' + $name)
            [Console]::Out.Flush()
        }
    }
    Remove-Event -EventIdentifier $e.EventIdentifier
}
`

	cmd := exec.CommandContext(ctx,
		"powershell", "-NoProfile", "-NonInteractive", "-Command", script)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("usb sensor pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("usb sensor start: %w", err)
	}

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		if len(parts) != 2 {
			continue
		}
		eventType, name := parts[0], parts[1]

		var alert Alert
		switch eventType {
		case "R":
			alert = Alert{
				Sensor:  "usb",
				Level:   AlertCritical,
				Message: fmt.Sprintf("USB removed: %s", name),
			}
		case "A":
			alert = Alert{
				Sensor:  "usb",
				Level:   AlertWarning,
				Message: fmt.Sprintf("USB connected: %s", name),
			}
		default:
			continue
		}

		select {
		case alerts <- alert:
		case <-ctx.Done():
			_ = cmd.Wait()
			return nil
		}
	}

	_ = cmd.Wait()
	return nil
}
