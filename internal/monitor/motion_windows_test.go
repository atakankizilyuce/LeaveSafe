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
			var readingTable, sensorTable [14]uintptr
			reading := &motionObject{table: &readingTable}
			sensor := &motionObject{table: &sensorTable}
			releasedReading, releasedSensor := 0, 0
			readingTable[2] = syscall.NewCallback(func(_ uintptr) uintptr { releasedReading++; return 1 })
			sensorTable[2] = syscall.NewCallback(func(_ uintptr) uintptr { releasedSensor++; return 1 })
			sensorTable[6] = syscall.NewCallback(func(_ uintptr, out **motionObject) uintptr { *out = reading; return 0 })
			readingTable[6] = syscall.NewCallback(func(_ uintptr, out *int64) uintptr {
				if failSlot == 6 {
					return 0x80004005
				}
				*out = 116444736000000000 + 123456789
				return 0
			})
			for slot := 7; slot <= 9; slot++ {
				index := slot
				readingTable[slot] = syscall.NewCallback(func(_ uintptr, out *float64) uintptr {
					if failSlot == index {
						return 0x80004005
					}
					*out = float64(index) - 7.5
					return 0
				})
			}
			interval := uint32(999)
			sensorTable[8] = syscall.NewCallback(func(_ uintptr, value uint32) uintptr { interval = value; return 0 })
			runtime.LockOSThread()
			hr, _, _ := motionInitialize.Call(1)
			if err := motionHRESULT(hr); err != nil {
				runtime.UnlockOSThread()
				t.Fatal(err)
			}
			r := &windowsMotionReader{sensor: sensor}
			closed := false
			defer func() {
				if !closed {
					r.close()
				}
			}()
			got, err := r.read()
			if failSlot >= 0 {
				if err == nil {
					t.Fatal("native getter error ignored")
				}
			} else if err != nil || got.x != -.5 || got.y != .5 || got.z != 1.5 || !got.at.Equal(time.Unix(12, 345678900)) {
				t.Fatalf("reading=%+v err=%v", got, err)
			}
			r.close()
			closed = true
			if releasedReading != 1 || releasedSensor != 1 || interval != 0 {
				t.Fatalf("cleanup: reading=%d sensor=%d interval=%d", releasedReading, releasedSensor, interval)
			}
			runtime.KeepAlive(reading)
		})
	}
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
