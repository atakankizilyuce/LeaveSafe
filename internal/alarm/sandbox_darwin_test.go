//go:build darwin

package alarm

import (
	"os"
	"testing"
	"unsafe"

	"github.com/ebitengine/purego"
)

func TestSandboxVolumeDevice(t *testing.T) {
	if os.Getenv("LEAVESAFE_SANDBOX_SMOKE") != "1" {
		t.Skip("opt-in signed sandbox smoke")
	}
	device, err := getDefaultOutputDevice()
	if err != nil {
		t.Fatal(err)
	}
	var current float32
	size := uint32(unsafe.Sizeof(current))
	status, _, _ := purego.SyscallN(audioObjectGetPropertyData,
		uintptr(device), uintptr(unsafe.Pointer(&volumeAddr)), 0, 0,
		uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&current)))
	if int32(status) != 0 {
		t.Fatalf("read output volume: %d", int32(status))
	}
	// Test write permission using the existing level, without raising the volume.
	if _, err := setVolume(float64(current)); err != nil {
		t.Fatal(err)
	}
	if err := restoreVolume(float64(current)); err != nil {
		t.Fatal(err)
	}
}
