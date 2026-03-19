//go:build windows

package alarm

import (
	"fmt"
	"math"
	"syscall"
	"unsafe"
)

type comGUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

var (
	clsidMMDeviceEnumerator = comGUID{0xBCDE0395, 0xE52F, 0x467C, [8]byte{0x8E, 0x3D, 0xC4, 0x57, 0x92, 0x91, 0x69, 0x2E}}
	iidIMMDeviceEnumerator  = comGUID{0xA95664D2, 0x9614, 0x4F35, [8]byte{0xA7, 0x46, 0xDE, 0x8D, 0xB6, 0x36, 0x17, 0xE6}}
	iidIAudioEndpointVolume = comGUID{0x5CDF2C82, 0x841E, 0x4546, [8]byte{0x97, 0x22, 0x0C, 0xF7, 0x40, 0x78, 0x22, 0x9A}}
)

var (
	ole32                = syscall.NewLazyDLL("ole32.dll")
	procCoInitializeEx   = ole32.NewProc("CoInitializeEx")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")
	procCoUninitialize   = ole32.NewProc("CoUninitialize")
)

const (
	clsctxAll           = 0x17
	coinitMultithreaded = 0x0
	eRender             = 0
	eConsole            = 1
)

var ptrSize = unsafe.Sizeof(uintptr(0))

func comVtableMethod(obj unsafe.Pointer, index int) uintptr {
	vtable := *(*unsafe.Pointer)(obj)
	return *(*uintptr)(unsafe.Add(vtable, index*int(ptrSize)))
}

func comRelease(obj unsafe.Pointer) {
	if obj != nil {
		fn := comVtableMethod(obj, 2)
		syscall.SyscallN(fn, uintptr(obj))
	}
}

// maxVolume saves the current volume level and sets it to 100%.
func maxVolume() (float64, error) {
	hr, _, _ := procCoInitializeEx.Call(0, coinitMultithreaded)
	if hr != 0 && hr != 1 {
		hr, _, _ = procCoInitializeEx.Call(0, 0x2)
		if hr != 0 && hr != 1 {
			return 0, fmt.Errorf("CoInitializeEx failed: 0x%x", hr)
		}
	}
	defer procCoUninitialize.Call()

	var enumerator unsafe.Pointer
	hr, _, _ = procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidMMDeviceEnumerator)),
		0, clsctxAll,
		uintptr(unsafe.Pointer(&iidIMMDeviceEnumerator)),
		uintptr(unsafe.Pointer(&enumerator)),
	)
	if hr != 0 {
		return 0, fmt.Errorf("CoCreateInstance failed: 0x%x", hr)
	}
	defer comRelease(enumerator)

	// IMMDeviceEnumerator::GetDefaultAudioEndpoint (vtable 4)
	var device unsafe.Pointer
	fn := comVtableMethod(enumerator, 4)
	hr, _, _ = syscall.SyscallN(fn,
		uintptr(enumerator),
		uintptr(eRender), uintptr(eConsole),
		uintptr(unsafe.Pointer(&device)),
	)
	if hr != 0 {
		return 0, fmt.Errorf("GetDefaultAudioEndpoint failed: 0x%x", hr)
	}
	defer comRelease(device)

	// IMMDevice::Activate (vtable 3) -> IAudioEndpointVolume
	var volume unsafe.Pointer
	fn = comVtableMethod(device, 3)
	hr, _, _ = syscall.SyscallN(fn,
		uintptr(device),
		uintptr(unsafe.Pointer(&iidIAudioEndpointVolume)),
		clsctxAll, 0,
		uintptr(unsafe.Pointer(&volume)),
	)
	if hr != 0 {
		return 0, fmt.Errorf("Activate failed: 0x%x", hr)
	}
	defer comRelease(volume)

	// GetMasterVolumeLevelScalar (vtable 9)
	var prevLevel float32
	fn = comVtableMethod(volume, 9)
	hr, _, _ = syscall.SyscallN(fn, uintptr(volume), uintptr(unsafe.Pointer(&prevLevel)))
	if hr != 0 {
		prevLevel = 0
	}

	// SetMasterVolumeLevelScalar (vtable 7)
	maxLevel := float32(1.0)
	var emptyGUID comGUID
	fn = comVtableMethod(volume, 7)
	hr, _, _ = syscall.SyscallN(fn,
		uintptr(volume),
		uintptr(math.Float32bits(maxLevel)),
		uintptr(unsafe.Pointer(&emptyGUID)),
	)
	if hr != 0 {
		return float64(prevLevel), fmt.Errorf("SetMasterVolumeLevelScalar failed: 0x%x", hr)
	}

	// SetMute (vtable 11)
	fn = comVtableMethod(volume, 11)
	syscall.SyscallN(fn, uintptr(volume), 0, uintptr(unsafe.Pointer(&emptyGUID)))

	return float64(prevLevel), nil
}

// restoreVolume sets the system volume back to the saved level.
func restoreVolume(level float64) error {
	hr, _, _ := procCoInitializeEx.Call(0, coinitMultithreaded)
	if hr != 0 && hr != 1 {
		hr, _, _ = procCoInitializeEx.Call(0, 0x2)
		if hr != 0 && hr != 1 {
			return fmt.Errorf("CoInitializeEx failed: 0x%x", hr)
		}
	}
	defer procCoUninitialize.Call()

	var enumerator unsafe.Pointer
	hr, _, _ = procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidMMDeviceEnumerator)),
		0, clsctxAll,
		uintptr(unsafe.Pointer(&iidIMMDeviceEnumerator)),
		uintptr(unsafe.Pointer(&enumerator)),
	)
	if hr != 0 {
		return fmt.Errorf("CoCreateInstance failed: 0x%x", hr)
	}
	defer comRelease(enumerator)

	var device unsafe.Pointer
	fn := comVtableMethod(enumerator, 4)
	hr, _, _ = syscall.SyscallN(fn,
		uintptr(enumerator),
		uintptr(eRender), uintptr(eConsole),
		uintptr(unsafe.Pointer(&device)),
	)
	if hr != 0 {
		return fmt.Errorf("GetDefaultAudioEndpoint failed: 0x%x", hr)
	}
	defer comRelease(device)

	var volume unsafe.Pointer
	fn = comVtableMethod(device, 3)
	hr, _, _ = syscall.SyscallN(fn,
		uintptr(device),
		uintptr(unsafe.Pointer(&iidIAudioEndpointVolume)),
		clsctxAll, 0,
		uintptr(unsafe.Pointer(&volume)),
	)
	if hr != 0 {
		return fmt.Errorf("Activate failed: 0x%x", hr)
	}
	defer comRelease(volume)

	restoreLevel := float32(level)
	var emptyGUID comGUID
	fn = comVtableMethod(volume, 7)
	hr, _, _ = syscall.SyscallN(fn,
		uintptr(volume),
		uintptr(math.Float32bits(restoreLevel)),
		uintptr(unsafe.Pointer(&emptyGUID)),
	)
	if hr != 0 {
		return fmt.Errorf("SetMasterVolumeLevelScalar failed: 0x%x", hr)
	}

	return nil
}
