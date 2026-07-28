//go:build windows

package location

import (
	"context"
	"fmt"
	"os/exec"
)

// wifiScanSupported reports whether a Wi-Fi scan can be run here.
func wifiScanSupported() bool {
	_, err := exec.LookPath("netsh")
	return err == nil
}

func wifiScanUnsupportedReason() string {
	return "netsh was not found on PATH"
}

// scanAccessPoints lists the Wi-Fi radios in range.
//
// netsh exits non-zero when the machine has no wireless interface, and prints
// an explanation rather than a list when the WLAN AutoConfig service is
// stopped. Both are ordinary states on a desktop, so neither is fatal: they
// surface as an empty result the caller reports as a failed lookup.
func scanAccessPoints(ctx context.Context) ([]AccessPoint, error) {
	out, err := exec.CommandContext(ctx, "netsh", "wlan", "show", "networks", "mode=bssid").Output()
	if err != nil {
		return nil, fmt.Errorf("netsh wlan show networks: %w", err)
	}
	return parseNetshBSSIDs(string(out)), nil
}
