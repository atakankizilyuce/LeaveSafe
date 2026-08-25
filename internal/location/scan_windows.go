//go:build windows

package location

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/leavesafe/leavesafe/internal/syspath"
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
	// Absolute path, not a PATH lookup — see internal/syspath.
	//
	// #nosec G204 -- the arguments are literals and the executable is resolved
	// from the Windows directory; neither comes from user input.
	cmd := exec.CommandContext(ctx, syspath.System32("netsh.exe"),
		"wlan", "show", "networks", "mode=bssid")
	// And no window for it. See syspath.HideWindow.
	syspath.HideWindow(cmd)

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("netsh wlan show networks: %w", err)
	}
	return parseNetshBSSIDs(string(out)), nil
}
