//go:build linux

package monitor

import "strings"

// The functions here interpret what the kernel and X server report. They are
// separated from the code that reads them so a format change — the kind that
// would silently turn the alarm into a no-op — is caught by a test against
// output the system really produced. See testdata/PROVENANCE.md.

// parseACOnline reads /sys/class/power_supply/<supply>/online.
func parseACOnline(raw string) bool {
	return strings.TrimSpace(raw) == "1"
}

// parseBatteryCharging reads /sys/class/power_supply/<supply>/status. A full
// battery still means the machine is on mains power.
func parseBatteryCharging(raw string) bool {
	status := strings.TrimSpace(raw)
	return status == "Charging" || status == "Full"
}

// parseLidOpen reads /proc/acpi/button/lid/<id>/state.
func parseLidOpen(raw string) bool {
	return strings.Contains(raw, "open")
}

// parseDPMSOn reads `xset q` output. DPMS reports On, Off, Standby or Suspend,
// and everything but On means the screen is dark.
func parseDPMSOn(raw string) bool {
	return !strings.Contains(raw, "Monitor is Off") &&
		!strings.Contains(raw, "Monitor is Standby") &&
		!strings.Contains(raw, "Monitor is Suspend")
}

// parseUSBProductName reads /sys/bus/usb/devices/<dev>/product. An empty result
// means the entry is not a device worth naming.
func parseUSBProductName(raw string) string {
	return strings.TrimSpace(raw)
}
