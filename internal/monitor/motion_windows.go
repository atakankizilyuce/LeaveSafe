//go:build windows

package monitor

import (
	"fmt"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// These are the public WinRT ABI interfaces from Windows.Devices.Sensors.
// No driver installation, location permission, private API or elevation.
var (
	motionRuntime      = windows.NewLazySystemDLL("combase.dll")
	motionInitialize   = motionRuntime.NewProc("RoInitialize")
	motionUninitialize = motionRuntime.NewProc("RoUninitialize")
	motionCreateString = motionRuntime.NewProc("WindowsCreateString")
	motionDeleteString = motionRuntime.NewProc("WindowsDeleteString")
	motionFactory      = motionRuntime.NewProc("RoGetActivationFactory")
	motionStaticsID    = windows.GUID{Data1: 0xa5e28b74, Data2: 0x5a87, Data3: 0x4a2d, Data4: [8]byte{0xbe, 0xcc, 0x0f, 0x90, 0x6e, 0xa0, 0x61, 0xdd}}
)

// IInspectable has six entries. The subsequent slots below follow the SDK's
// IAccelerometerStatics, IAccelerometer and IAccelerometerReading declarations.
type motionObject struct{ table *[14]uintptr }

func (o *motionObject) readValue(slot int, result unsafe.Pointer) error {
	hr, _, _ := syscall.SyscallN(o.table[slot], uintptr(unsafe.Pointer(o)), uintptr(result))
	return motionHRESULT(hr)
}

func (o *motionObject) setInterval(value uint32) error {
	hr, _, _ := syscall.SyscallN(o.table[8], uintptr(unsafe.Pointer(o)), uintptr(value))
	return motionHRESULT(hr)
}

func (o *motionObject) release() {
	if o != nil {
		syscall.SyscallN(o.table[2], uintptr(unsafe.Pointer(o)))
	}
}

func motionHRESULT(hr uintptr) error {
	if int32(hr) < 0 {
		return fmt.Errorf("Windows motion API failed (0x%08x)", uint32(hr))
	}
	return nil
}

type windowsMotionReader struct{ sensor *motionObject }

func openMotionReader() (motionReader, error) {
	runtime.LockOSThread()
	hr, _, _ := motionInitialize.Call(1) // RO_INIT_MULTITHREADED
	if err := motionHRESULT(hr); err != nil {
		runtime.UnlockOSThread()
		return nil, err
	}
	sensor, err := defaultAccelerometer()
	if err != nil {
		motionUninitialize.Call()
		runtime.UnlockOSThread()
		return nil, err
	}
	reader := &windowsMotionReader{sensor: sensor}
	var minimum uint32
	if err = sensor.readValue(7, unsafe.Pointer(&minimum)); err == nil {
		if minimum < 50 {
			minimum = 50
		}
		err = sensor.setInterval(minimum)
	}
	if err != nil {
		reader.close()
		return nil, err
	}
	return reader, nil
}

func defaultAccelerometer() (*motionObject, error) {
	name, err := windows.UTF16FromString("Windows.Devices.Sensors.Accelerometer")
	if err != nil {
		return nil, err
	}
	var class uintptr
	hr, _, _ := motionCreateString.Call(uintptr(unsafe.Pointer(&name[0])), uintptr(len(name)-1), uintptr(unsafe.Pointer(&class)))
	if err = motionHRESULT(hr); err != nil {
		return nil, err
	}
	defer motionDeleteString.Call(class)
	var factory *motionObject
	hr, _, _ = motionFactory.Call(class, uintptr(unsafe.Pointer(&motionStaticsID)), uintptr(unsafe.Pointer(&factory)))
	if err = motionHRESULT(hr); err != nil {
		return nil, err
	}
	if factory == nil {
		return nil, errMotionUnavailable
	}
	defer factory.release()
	var sensor *motionObject
	if err = factory.readValue(6, unsafe.Pointer(&sensor)); err != nil {
		return nil, err
	}
	if sensor == nil {
		return nil, errMotionUnavailable
	}
	return sensor, nil
}

func (r *windowsMotionReader) read() (motionReading, error) {
	var result motionReading
	var reading *motionObject
	if err := r.sensor.readValue(6, unsafe.Pointer(&reading)); err != nil {
		return result, err
	}
	if reading == nil {
		return result, errMotionUnavailable
	}
	defer reading.release()
	var ticks int64
	if err := reading.readValue(6, unsafe.Pointer(&ticks)); err != nil {
		return result, err
	}
	// WinRT DateTime is 100 ns ticks since 1601, not Unix nanoseconds.
	result.at = time.Unix(ticks/10000000-11644473600, (ticks%10000000)*100)
	for i, value := range []*float64{&result.x, &result.y, &result.z} {
		if err := reading.readValue(7+i, unsafe.Pointer(value)); err != nil {
			return motionReading{}, err
		}
	}
	return result, nil
}

func (r *windowsMotionReader) close() {
	_ = r.sensor.setInterval(0) // Return this client's report interval to default.
	r.sensor.release()
	motionUninitialize.Call()
	runtime.UnlockOSThread()
}
