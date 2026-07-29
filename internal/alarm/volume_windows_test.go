//go:build windows

package alarm

import (
	"fmt"
	"os"
	"syscall"
	"testing"
	"unsafe"
)

// Read-only IAudioEndpointVolume slots, used to observe what the alarm did to
// the endpoint. See the layout table in volume_windows.go.
const (
	vtGetChannelCount             = 5
	vtGetChannelVolumeLevelScalar = 13
	vtGetMute                     = 15
)

// TestAlarmVolumeLeavesChannelBalanceAlone is the regression test for a siren
// that muted one earpiece for good. The "unmute" call after forcing the volume
// up named vtable slot 11 — SetChannelVolumeLevelScalar, not SetMute — so it
// set channel 0 of the default output device to whatever float the pointer
// argument happened to decode to. Every sound on the machine came out of the
// right ear afterwards, and nothing ever put it back.
//
// It drives the real audio endpoint, so it runs on request rather than as part
// of `go test ./...`. See docs/manual-verification.md.
func TestAlarmVolumeLeavesChannelBalanceAlone(t *testing.T) {
	if os.Getenv("LEAVESAFE_AUDIO_HW_TEST") != "1" {
		t.Skip("changes the machine's real volume; set LEAVESAFE_AUDIO_HW_TEST=1 to run")
	}

	before, err := channelVolumes()
	if err != nil {
		t.Skipf("no usable audio endpoint: %v", err)
	}
	if len(before) < 2 {
		t.Skipf("endpoint has %d channel(s); balance needs at least 2", len(before))
	}

	saved, err := maxVolume()
	if err != nil {
		t.Fatalf("maxVolume: %v", err)
	}
	t.Cleanup(func() {
		if err := restoreVolume(saved); err != nil {
			t.Errorf("restoreVolume: %v", err)
		}
	})

	after, err := channelVolumes()
	if err != nil {
		t.Fatalf("read channel volumes after maxVolume: %v", err)
	}
	for i := range before {
		if before[i] != after[i] {
			t.Errorf("channel %d volume changed from %v to %v; the alarm must move the "+
				"master volume only, never the left/right balance", i, before[i], after[i])
		}
	}

	muted, err := endpointMuted()
	if err != nil {
		t.Fatalf("read mute state: %v", err)
	}
	if muted {
		// Slot 14 really is SetMute: a wrong slot leaves the endpoint muted and a
		// maximum volume inaudible.
		t.Error("endpoint still muted after maxVolume")
	}
}

func channelVolumes() ([]float32, error) {
	volume, cleanup, err := acquireEndpointVolume()
	if err != nil {
		return nil, err
	}
	defer cleanup()

	var count uint32
	hr, _, _ := syscall.SyscallN(comVtableMethod(volume, vtGetChannelCount),
		uintptr(volume), uintptr(unsafe.Pointer(&count)))
	if hr != 0 {
		return nil, fmt.Errorf("GetChannelCount failed: 0x%x", hr)
	}

	levels := make([]float32, count)
	for i := range levels {
		hr, _, _ := syscall.SyscallN(comVtableMethod(volume, vtGetChannelVolumeLevelScalar),
			uintptr(volume), uintptr(i), uintptr(unsafe.Pointer(&levels[i])))
		if hr != 0 {
			return nil, fmt.Errorf("GetChannelVolumeLevelScalar(%d) failed: 0x%x", i, hr)
		}
	}
	return levels, nil
}

func endpointMuted() (bool, error) {
	volume, cleanup, err := acquireEndpointVolume()
	if err != nil {
		return false, err
	}
	defer cleanup()

	var mute int32
	hr, _, _ := syscall.SyscallN(comVtableMethod(volume, vtGetMute),
		uintptr(volume), uintptr(unsafe.Pointer(&mute)))
	if hr != 0 {
		return false, fmt.Errorf("GetMute failed: 0x%x", hr)
	}
	return mute != 0, nil
}
