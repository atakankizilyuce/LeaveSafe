//go:build windows

package monitor

import (
	"runtime"
	"syscall"
	"testing"
	"time"
)

func TestMotionHRESULT(t *testing.T) {
	for _, hr := range []uintptr{0, 1} {
		if motionHRESULT(hr) != nil {
			t.Fatal("success rejected")
		}
	}
	if motionHRESULT(0x80004005) == nil {
		t.Fatal("failure accepted")
	}
}

// Exercise the actual WinRT ABI adapter against in-process COM callbacks.
// These tests require Windows, but no sensor hardware or elevated privileges.
func TestMotionNativeReadingAndCleanup(t *testing.T) {
	for _, failSlot := range []int{-1, 6, 7, 8, 9} {
		t.Run(string(rune('a'+failSlot+1)), func(t *testing.T) {
			checkMotionNativeReading(t, failSlot)
		})
	}
}

type nativeMotionFixture struct {
	readingTable, sensorTable       [14]uintptr
	reading, sensor                 *motionObject
	releasedReading, releasedSensor int
	interval                        uint32
}

func newNativeMotionFixture(failSlot int) *nativeMotionFixture {
	f := &nativeMotionFixture{interval: 999}
	f.reading = &motionObject{table: &f.readingTable}
	f.sensor = &motionObject{table: &f.sensorTable}
	f.readingTable[2] = syscall.NewCallback(func(_ uintptr) uintptr { f.releasedReading++; return 1 })
	f.sensorTable[2] = syscall.NewCallback(func(_ uintptr) uintptr { f.releasedSensor++; return 1 })
	f.sensorTable[6] = syscall.NewCallback(func(_ uintptr, out **motionObject) uintptr { *out = f.reading; return 0 })
	f.readingTable[6] = syscall.NewCallback(func(_ uintptr, out *int64) uintptr {
		if failSlot == 6 {
			return 0x80004005
		}
		*out = 116444736000000000 + 123456789
		return 0
	})
	for slot := 7; slot <= 9; slot++ {
		f.readingTable[slot] = nativeMotionValueGetter(slot, failSlot)
	}
	f.sensorTable[8] = syscall.NewCallback(func(_ uintptr, value uint32) uintptr { f.interval = value; return 0 })
	return f
}

func nativeMotionValueGetter(slot, failSlot int) uintptr {
	return syscall.NewCallback(func(_ uintptr, out *float64) uintptr {
		if failSlot == slot {
			return 0x80004005
		}
		*out = float64(slot) - 7.5
		return 0
	})
}

func checkMotionNativeReading(t *testing.T, failSlot int) {
	t.Helper()
	f := newNativeMotionFixture(failSlot)
	runtime.LockOSThread()
	hr, _, _ := motionInitialize.Call(1)
	if err := motionHRESULT(hr); err != nil {
		runtime.UnlockOSThread()
		t.Fatal(err)
	}
	r := &windowsMotionReader{sensor: f.sensor}
	defer r.close()
	got, err := r.read()
	if failSlot >= 0 && err == nil {
		t.Fatal("native getter error ignored")
	}
	if failSlot < 0 && (err != nil || got.x != -.5 || got.y != .5 || got.z != 1.5 || !got.at.Equal(time.Unix(12, 345678900))) {
		t.Fatalf("reading=%+v err=%v", got, err)
	}
	if f.releasedReading != 1 {
		t.Fatal("reading reference leaked")
	}
}

func TestMotionNativeCloseRestoresIntervalAndReleasesSensor(t *testing.T) {
	f := newNativeMotionFixture(-1)
	runtime.LockOSThread()
	hr, _, _ := motionInitialize.Call(1)
	if err := motionHRESULT(hr); err != nil {
		runtime.UnlockOSThread()
		t.Fatal(err)
	}
	r := &windowsMotionReader{sensor: f.sensor}
	r.close()
	if f.releasedSensor != 1 || f.interval != 0 {
		t.Fatalf("cleanup: sensor=%d interval=%d", f.releasedSensor, f.interval)
	}
	runtime.KeepAlive(f)
}

func TestMotionNativeMissingOrFailedReading(t *testing.T) {
	for _, hr := range []uintptr{0, 0x80004005} {
		var table [14]uintptr
		table[6] = syscall.NewCallback(func(_ uintptr, _ **motionObject) uintptr { return hr })
		r := &windowsMotionReader{sensor: &motionObject{table: &table}}
		if _, err := r.read(); err == nil {
			t.Fatal("missing reading reported as data")
		}
	}
}

func TestMotionPublicAPIProbe(t *testing.T) {
	// CI normally has no accelerometer. Both supported and unsupported hosts
	// must return normally, and release their COM apartment and sensor objects.
	s := NewMotionSensor()
	_ = s.Available()
}
