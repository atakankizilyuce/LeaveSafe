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

// COM vtable slots. A vtable is an unnamed array of function pointers, so the
// compiler cannot tell one method from another: an index that is off by a few
// slots calls a *different* method with the arguments meant for this one, and
// the only symptom is whatever that method happens to do. Naming the slots and
// writing the full interface layout down is what keeps that from happening
// silently.
const (
	// IMMDeviceEnumerator, after the three IUnknown slots:
	//   3 EnumAudioEndpoints
	vtGetDefaultAudioEndpoint = 4

	// IMMDevice, after the three IUnknown slots:
	vtActivate = 3

	// IAudioEndpointVolume, after the three IUnknown slots:
	//    3 RegisterControlChangeNotify
	//    4 UnregisterControlChangeNotify
	//    5 GetChannelCount
	//    6 SetMasterVolumeLevel        (level in dB)
	vtSetMasterVolumeLevelScalar = 7 // (level 0..1, event context)
	//    8 GetMasterVolumeLevel        (level in dB)
	vtGetMasterVolumeLevelScalar = 9 // (out level 0..1)
	//   10 SetChannelVolumeLevel       (channel, level in dB, event context)
	//   11 SetChannelVolumeLevelScalar (channel, level 0..1, event context)
	//   12 GetChannelVolumeLevel
	//   13 GetChannelVolumeLevelScalar
	vtSetMute = 14 // (mute bool, event context)
	//   15 GetMute
	//   16 GetVolumeStepInfo
	//   17 VolumeStepUp
	//   18 VolumeStepDown
	//   19 QueryHardwareSupport
	//   20 GetVolumeRange
)

var ptrSize = unsafe.Sizeof(uintptr(0))

func comVtableMethod(obj unsafe.Pointer, index int) uintptr {
	vtable := *(*unsafe.Pointer)(obj)
	return *(*uintptr)(unsafe.Add(vtable, index*int(ptrSize)))
}

func comRelease(obj unsafe.Pointer) {
	if obj != nil {
		// IUnknown::Release returns the new reference count, not an error.
		_, _, _ = syscall.SyscallN(comVtableMethod(obj, 2), uintptr(obj))
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
		_, _, _ = procCoUninitialize.Call()
		return nil, nil, fmt.Errorf("CoCreateInstance failed: 0x%x", hr)
	}

	var device unsafe.Pointer
	hr, _, _ = syscall.SyscallN(comVtableMethod(enumerator, vtGetDefaultAudioEndpoint),
		uintptr(enumerator),
		uintptr(eRender), uintptr(eConsole),
		uintptr(unsafe.Pointer(&device)),
	)
	if hr != 0 {
		comRelease(enumerator)
		_, _, _ = procCoUninitialize.Call()
		return nil, nil, fmt.Errorf("GetDefaultAudioEndpoint failed: 0x%x", hr)
	}

	var volume unsafe.Pointer
	hr, _, _ = syscall.SyscallN(comVtableMethod(device, vtActivate),
		uintptr(device),
		uintptr(unsafe.Pointer(&iidIAudioEndpointVolume)),
		clsctxAll, 0,
		uintptr(unsafe.Pointer(&volume)),
	)
	if hr != 0 {
		comRelease(device)
		comRelease(enumerator)
		_, _, _ = procCoUninitialize.Call()
		return nil, nil, fmt.Errorf("IMMDevice.Activate failed: 0x%x", hr)
	}

	return volume, func() {
		comRelease(volume)
		comRelease(device)
		comRelease(enumerator)
		_, _, _ = procCoUninitialize.Call()
	}, nil
}

func maxVolume() (float64, error) {
	volume, cleanup, err := acquireEndpointVolume()
	if err != nil {
		return 0, err
	}
	defer cleanup()

	var prevLevel float32
	hr, _, _ := syscall.SyscallN(comVtableMethod(volume, vtGetMasterVolumeLevelScalar),
		uintptr(volume), uintptr(unsafe.Pointer(&prevLevel)))
	if hr != 0 {
		prevLevel = 0
	}

	maxLevel := float32(1.0)
	var emptyGUID comGUID
	hr, _, _ = syscall.SyscallN(comVtableMethod(volume, vtSetMasterVolumeLevelScalar),
		uintptr(volume),
		uintptr(math.Float32bits(maxLevel)),
		uintptr(unsafe.Pointer(&emptyGUID)),
	)
	if hr != 0 {
		return float64(prevLevel), fmt.Errorf("SetMasterVolumeLevelScalar failed: 0x%x", hr)
	}

	// Unmuting is best effort: the volume level is already at maximum. Only the
	// master mute is touched — never the per-channel volumes, which is where the
	// user's left/right balance lives.
	_, _, _ = syscall.SyscallN(comVtableMethod(volume, vtSetMute),
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
	hr, _, _ := syscall.SyscallN(comVtableMethod(volume, vtGetMasterVolumeLevelScalar),
		uintptr(volume), uintptr(unsafe.Pointer(&prevLevel)))
	if hr != 0 {
		prevLevel = 0
	}

	target := float32(clampLevel(level))
	var emptyGUID comGUID
	hr, _, _ = syscall.SyscallN(comVtableMethod(volume, vtSetMasterVolumeLevelScalar),
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
	hr, _, _ := syscall.SyscallN(comVtableMethod(volume, vtSetMasterVolumeLevelScalar),
		uintptr(volume),
		uintptr(math.Float32bits(float32(clampLevel(level)))),
		uintptr(unsafe.Pointer(&emptyGUID)),
	)
	if hr != 0 {
		return fmt.Errorf("SetMasterVolumeLevelScalar failed: 0x%x", hr)
	}

	return nil
}
