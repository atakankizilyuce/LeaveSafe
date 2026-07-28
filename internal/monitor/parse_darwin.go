//go:build darwin

package monitor

import (
	"strconv"
	"strings"
)

// The functions here interpret what pmset, ioreg and system_profiler report.
// They are separated from the code that runs those tools so a format change —
// the kind that would silently turn the alarm into a no-op — is caught by a
// test against output macOS really produced. See testdata/PROVENANCE.md.

// parseOnACPower reads `pmset -g batt` output.
func parseOnACPower(raw string) bool {
	return strings.Contains(raw, "'AC Power'")
}

// parseClamshellOpen reads `ioreg -r -k AppleClamshellState -d 1`.
// AppleClamshellState = No means the lid is open. A machine without a clamshell
// reports nothing at all, which must not read as a closed lid.
func parseClamshellOpen(raw string) bool {
	return strings.Contains(raw, `"AppleClamshellState" = No`)
}

// parseDisplayOn reads `ioreg -r -d 1 -c IODisplayWrangler`. DevicePowerState 4
// is on; 0 means the display is asleep.
func parseDisplayOn(raw string) bool {
	return !strings.Contains(raw, `"DevicePowerState"=0`) &&
		!strings.Contains(raw, `"DevicePowerState" = 0`)
}

// parseHIDIdleSeconds reads `ioreg -c IOHIDSystem -d 4 -S` and converts the
// nanosecond HIDIdleTime to seconds. It returns -1 when the key is absent.
func parseHIDIdleSeconds(raw string) float64 {
	for _, line := range strings.Split(raw, "\n") {
		if !strings.Contains(line, "HIDIdleTime") {
			continue
		}
		parts := strings.Split(line, "=")
		if len(parts) < 2 {
			continue
		}
		ns, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
		if err != nil {
			continue
		}
		return float64(ns) / 1e9
	}
	return -1
}

// parseUSBDeviceNames reads `system_profiler SPUSBDataType -detailLevel mini`
// and returns the device section headings.
func parseUSBDeviceNames(raw string) []string {
	var names []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasSuffix(line, ":") && !strings.Contains(line, "USB") {
			names = append(names, strings.TrimSuffix(line, ":"))
		}
	}
	return names
}
