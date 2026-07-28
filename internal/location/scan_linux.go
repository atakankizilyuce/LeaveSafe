//go:build linux

package location

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// wirelessSysfsPath lists the wireless interfaces the kernel knows about. If it
// is empty there is no radio to scan with, whatever tools are installed.
const wirelessSysfsPath = "/sys/class/ieee80211"

// wifiScanSupported reports whether a Wi-Fi scan can be run here.
func wifiScanSupported() bool {
	if !hasWirelessDevice() {
		return false
	}
	return hasNmcli() || hasIW()
}

func wifiScanUnsupportedReason() string {
	if !hasWirelessDevice() {
		return "no wireless device in " + wirelessSysfsPath
	}
	return "neither nmcli nor iw was found on PATH"
}

func hasWirelessDevice() bool {
	entries, err := os.ReadDir(wirelessSysfsPath)
	return err == nil && len(entries) > 0
}

func hasNmcli() bool {
	_, err := exec.LookPath("nmcli")
	return err == nil
}

func hasIW() bool {
	_, err := exec.LookPath("iw")
	return err == nil
}

// scanAccessPoints lists the Wi-Fi radios in range.
//
// nmcli is tried first: it reads the scan results NetworkManager already
// maintains, so it needs no privileges. iw is the fallback for systems without
// NetworkManager, and it does generally need root — which is why it is second
// rather than first.
func scanAccessPoints(ctx context.Context) ([]AccessPoint, error) {
	var errs []string

	if hasNmcli() {
		out, err := exec.CommandContext(ctx, "nmcli", "-t", "-f", "BSSID,SIGNAL,CHAN", "dev", "wifi", "list").Output()
		if err == nil {
			if aps := parseNmcliWiFi(string(out)); len(aps) > 0 {
				return aps, nil
			}
			errs = append(errs, "nmcli listed no access points")
		} else {
			errs = append(errs, fmt.Sprintf("nmcli: %v", err))
		}
	}

	if hasIW() {
		aps, err := scanWithIW(ctx)
		if err == nil && len(aps) > 0 {
			return aps, nil
		}
		if err != nil {
			errs = append(errs, fmt.Sprintf("iw: %v", err))
		} else {
			errs = append(errs, "iw listed no access points")
		}
	}

	if len(errs) == 0 {
		errs = append(errs, "no wi-fi scanning tool available")
	}
	return nil, fmt.Errorf("%s", strings.Join(errs, "; "))
}

func scanWithIW(ctx context.Context) ([]AccessPoint, error) {
	iface, err := firstWirelessInterface()
	if err != nil {
		return nil, err
	}

	// #nosec G204 -- iface comes from a kernel directory listing under /sys,
	// not from user input.
	out, err := exec.CommandContext(ctx, "iw", "dev", iface, "scan").Output()
	if err != nil {
		return nil, fmt.Errorf("iw dev %s scan: %w", iface, err)
	}
	return parseIWScan(string(out)), nil
}

// firstWirelessInterface returns the name of a wireless netdev, by walking from
// the phy in /sys/class/ieee80211 down to the interface it owns.
func firstWirelessInterface() (string, error) {
	phys, err := os.ReadDir(wirelessSysfsPath)
	if err != nil {
		return "", fmt.Errorf("list wireless devices: %w", err)
	}
	for _, phy := range phys {
		devices, err := os.ReadDir(filepath.Join(wirelessSysfsPath, phy.Name(), "device", "net"))
		if err != nil {
			continue
		}
		for _, dev := range devices {
			return dev.Name(), nil
		}
	}
	return "", fmt.Errorf("no wireless interface found under %s", wirelessSysfsPath)
}
