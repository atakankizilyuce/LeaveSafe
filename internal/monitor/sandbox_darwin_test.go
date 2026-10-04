//go:build darwin

package monitor

import (
	"context"
	"os"
	"testing"
)

// Compiled with go test -c and launched as an inherited-sandbox child of
// the smoke app. Never read the host's sensors in the ordinary unit suite.
func TestSandboxHardware(t *testing.T) {
	if os.Getenv("LEAVESAFE_SANDBOX_SMOKE") != "1" {
		t.Skip("opt-in signed sandbox smoke")
	}
	if _, err := isOnACPower(); err != nil {
		t.Errorf("power: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, stop, err := subscribeLid(ctx)
	if err != nil {
		t.Errorf("lid notifications: %v", err)
	} else {
		stop()
	}
	if _, err := isLidOpenDarwin(); err != nil {
		t.Errorf("lid: %v", err)
	}
	if _, err := isScreenOnDarwin(); err != nil {
		t.Errorf("screen: %v", err)
	}
	if getIdleSeconds() < 0 {
		t.Error("input: no HID idle measurement")
	}
	if _, _, err := getUSBSnapshotDarwin(); err != nil {
		t.Errorf("usb: %v", err)
	}
	if networkSnapshot() == "error" {
		t.Error("network: interface query failed")
	}
	if path := os.Getenv("LEAVESAFE_SANDBOX_SENTINEL"); path != "" {
		if _, err := os.ReadFile(path); err == nil {
			t.Error("sandbox allowed access to outside-container sentinel")
		}
	}
}
