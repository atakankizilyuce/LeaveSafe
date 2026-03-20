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
		syscall.SyscallN(comVtableMethod(obj, 2), uintptr(obj))
	}
}

// acquireEndpointVolume initializes COM, gets the default audio endpoint,
// and returns the IAudioEndpointVolume interface with a cleanup function.
func acquireEndpointVolume() (vol unsafe.Pointer, cleanup func(), err error) {
	hr, _, _ := procCoInitializeEx.Call(0, coinitMultithreaded)
	if hr != 0 && hr != 1 {
		hr, _, _ = procCoInitializeEx.Call(0, 0x2)
		if hr != 0 && hr != 1 {
			return nil, nil, fmt.Errorf("CoInitializeEx failed: 0x%x", hr)
		}
	}

	var enumerator unsafe.Pointer
	hr, _, _ = procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidMMDeviceEnumerator)),
		0, clsctxAll,
		uintptr(unsafe.Pointer(&iidIMMDeviceEnumerator)),
		uintptr(unsafe.Pointer(&enumerator)),
	)
	if hr != 0 {
		procCoUninitialize.Call()
		return nil, nil, fmt.Errorf("CoCreateInstance failed: 0x%x", hr)
	}

	var device unsafe.Pointer
	hr, _, _ = syscall.SyscallN(comVtableMethod(enumerator, 4),
		uintptr(enumerator),
		uintptr(eRender), uintptr(eConsole),
		uintptr(unsafe.Pointer(&device)),
	)
	if hr != 0 {
		comRelease(enumerator)
		procCoUninitialize.Call()
		return nil, nil, fmt.Errorf("GetDefaultAudioEndpoint failed: 0x%x", hr)
	}

	var volume unsafe.Pointer
	hr, _, _ = syscall.SyscallN(comVtableMethod(device, 3),
		uintptr(device),
		uintptr(unsafe.Pointer(&iidIAudioEndpointVolume)),
		clsctxAll, 0,
		uintptr(unsafe.Pointer(&volume)),
	)
	if hr != 0 {
		comRelease(device)
		comRelease(enumerator)
		procCoUninitialize.Call()
		return nil, nil, fmt.Errorf("Activate failed: 0x%x", hr)
	}

	return volume, func() {
		comRelease(volume)
		comRelease(device)
		comRelease(enumerator)
		procCoUninitialize.Call()
	}, nil
}

func maxVolume() (float64, error) {
	volume, cleanup, err := acquireEndpointVolume()
	if err != nil {
		return 0, err
	}
	defer cleanup()

	var prevLevel float32
	hr, _, _ := syscall.SyscallN(comVtableMethod(volume, 9),
		uintptr(volume), uintptr(unsafe.Pointer(&prevLevel)))
	if hr != 0 {
		prevLevel = 0
	}

	maxLevel := float32(1.0)
	var emptyGUID comGUID
	hr, _, _ = syscall.SyscallN(comVtableMethod(volume, 7),
		uintptr(volume),
		uintptr(math.Float32bits(maxLevel)),
		uintptr(unsafe.Pointer(&emptyGUID)),
	)
	if hr != 0 {
		return float64(prevLevel), fmt.Errorf("SetMasterVolumeLevelScalar failed: 0x%x", hr)
	}

	syscall.SyscallN(comVtableMethod(volume, 11),
		uintptr(volume), 0, uintptr(unsafe.Pointer(&emptyGUID)))

	return float64(prevLevel), nil
}

func setVolume(level float64) (float64, error) {
	volume, cleanup, err := acquireEndpointVolume()
	if err != nil {
		return 0, err
	}
	defer cleanup()

	var prevLevel float32
	hr, _, _ := syscall.SyscallN(comVtableMethod(volume, 9),
		uintptr(volume), uintptr(unsafe.Pointer(&prevLevel)))
	if hr != 0 {
		prevLevel = 0
	}

	target := float32(level)
	var emptyGUID comGUID
	hr, _, _ = syscall.SyscallN(comVtableMethod(volume, 7),
		uintptr(volume),
		uintptr(math.Float32bits(target)),
		uintptr(unsafe.Pointer(&emptyGUID)),
	)
	if hr != 0 {
		return float64(prevLevel), fmt.Errorf("SetMasterVolumeLevelScalar failed: 0x%x", hr)
	}

	return float64(prevLevel), nil
}

func restoreVolume(level float64) error {
	volume, cleanup, err := acquireEndpointVolume()
	if err != nil {
		return err
	}
	defer cleanup()

	var emptyGUID comGUID
	hr, _, _ := syscall.SyscallN(comVtableMethod(volume, 7),
		uintptr(volume),
		uintptr(math.Float32bits(float32(level))),
		uintptr(unsafe.Pointer(&emptyGUID)),
	)
	if hr != 0 {
		return fmt.Errorf("SetMasterVolumeLevelScalar failed: 0x%x", hr)
	}

	return nil
}
