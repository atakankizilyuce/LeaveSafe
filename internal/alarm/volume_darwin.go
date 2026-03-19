//go:build darwin

package alarm

import (
	"fmt"
	"math"
	"unsafe"

	"github.com/ebitengine/purego"
)

var (
	coreAudio                  uintptr
	audioObjectGetPropertyData uintptr
	audioObjectSetPropertyData uintptr
)

func init() {
	lib, err := purego.Dlopen("/System/Library/Frameworks/CoreAudio.framework/CoreAudio", purego.RTLD_LAZY)
	if err != nil {
		return
	}
	coreAudio = lib
	audioObjectGetPropertyData, _ = purego.Dlsym(lib, "AudioObjectGetPropertyData")
	audioObjectSetPropertyData, _ = purego.Dlsym(lib, "AudioObjectSetPropertyData")
}

type audioObjectPropertyAddress struct {
	Selector uint32
	Scope    uint32
	Element  uint32
}

const (
	kAudioHardwareServiceDeviceProperty_VirtualMainVolume = 0x766D7663
	kAudioObjectPropertyScopeOutput                      = 0x6F757470
	kAudioObjectPropertyElementMain                      = 0
	kAudioHardwarePropertyDefaultOutputDevice            = 0x644F7574
	kAudioObjectSystemObject                             = 1
)

func getDefaultOutputDevice() (uint32, error) {
	if audioObjectGetPropertyData == 0 {
		return 0, fmt.Errorf("CoreAudio not available")
	}
	addr := audioObjectPropertyAddress{
		Selector: kAudioHardwarePropertyDefaultOutputDevice,
		Scope:    kAudioObjectPropertyScopeOutput,
		Element:  kAudioObjectPropertyElementMain,
	}
	var deviceID uint32
	dataSize := uint32(unsafe.Sizeof(deviceID))
	ret, _, _ := purego.SyscallN(audioObjectGetPropertyData,
		uintptr(kAudioObjectSystemObject),
		uintptr(unsafe.Pointer(&addr)), 0, 0,
		uintptr(unsafe.Pointer(&dataSize)),
		uintptr(unsafe.Pointer(&deviceID)),
	)
	if ret != 0 {
		return 0, fmt.Errorf("get default output device failed: %d", ret)
	}
	return deviceID, nil
}

var volumeAddr = audioObjectPropertyAddress{
	Selector: kAudioHardwareServiceDeviceProperty_VirtualMainVolume,
	Scope:    kAudioObjectPropertyScopeOutput,
	Element:  kAudioObjectPropertyElementMain,
}

func maxVolume() (float64, error) {
	deviceID, err := getDefaultOutputDevice()
	if err != nil {
		return 0, err
	}

	var currentVolume float32
	dataSize := uint32(unsafe.Sizeof(currentVolume))
	purego.SyscallN(audioObjectGetPropertyData,
		uintptr(deviceID), uintptr(unsafe.Pointer(&volumeAddr)), 0, 0,
		uintptr(unsafe.Pointer(&dataSize)),
		uintptr(unsafe.Pointer(&currentVolume)),
	)

	maxVol := float32(1.0)
	dataSize = uint32(unsafe.Sizeof(maxVol))
	ret, _, _ := purego.SyscallN(audioObjectSetPropertyData,
		uintptr(deviceID), uintptr(unsafe.Pointer(&volumeAddr)), 0, 0,
		uintptr(dataSize), uintptr(unsafe.Pointer(&maxVol)),
	)
	if ret != 0 {
		return float64(currentVolume), fmt.Errorf("set volume failed: %d", ret)
	}

	return float64(currentVolume), nil
}

func restoreVolume(level float64) error {
	deviceID, err := getDefaultOutputDevice()
	if err != nil {
		return err
	}

	vol := float32(math.Min(level, 1.0))
	dataSize := uint32(unsafe.Sizeof(vol))
	ret, _, _ := purego.SyscallN(audioObjectSetPropertyData,
		uintptr(deviceID), uintptr(unsafe.Pointer(&volumeAddr)), 0, 0,
		uintptr(dataSize), uintptr(unsafe.Pointer(&vol)),
	)
	if ret != 0 {
		return fmt.Errorf("set volume failed: %d", ret)
	}
	return nil
}
