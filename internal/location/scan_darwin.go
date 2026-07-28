//go:build darwin

package location

import (
	"context"
	"fmt"
)

// Wi-Fi positioning is not available on macOS, and this file exists to say so
// rather than to pretend otherwise.
//
// Resolving a position from Wi-Fi requires the BSSIDs of nearby access points.
// macOS no longer hands those to an ordinary process:
//
//   - `airport -s`, the tool every guide still points at, was removed in
//     macOS 14.4.
//   - `system_profiler SPAirPortDataType` lists neighboring networks by SSID,
//     channel and signal, but reports no BSSID for any of them. The one MAC
//     address it does print belongs to this machine's own card.
//   - CoreWLAN returns BSSIDs only to a process holding a Location Services
//     authorization, which means a signed bundle carrying the entitlement and a
//     consent prompt — not something a single self-contained binary can do.
//   - `wdutil info` exposes them but requires root.
//
// So the Wi-Fi provider reports itself unavailable here and the tracker never
// polls it. macOS is served by the phone anchor and the IP lookup, which is
// stated in the README rather than left for a user to discover from an empty
// panel.

func wifiScanSupported() bool { return false }

func wifiScanUnsupportedReason() string {
	return "macOS does not expose access point BSSIDs to unentitled processes; " +
		"airport was removed in 14.4 and CoreWLAN requires a Location Services authorization"
}

func scanAccessPoints(_ context.Context) ([]AccessPoint, error) {
	return nil, fmt.Errorf("wi-fi scanning unavailable: %s", wifiScanUnsupportedReason())
}
