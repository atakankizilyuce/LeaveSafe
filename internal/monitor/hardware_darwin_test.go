//go:build darwin

package monitor

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestDisplayRead(t *testing.T) {
	for _, state := range []int32{0, 1} {
		on, err := readDisplayOn(func() uint32 { return 1 }, func(id uint32) int32 {
			if id != 1 {
				t.Fatalf("display = %d", id)
			}
			return state
		})
		if err != nil || on != (state == 0) {
			t.Fatalf("state %d: on=%v err=%v", state, on, err)
		}
	}
	if _, err := readDisplayOn(func() uint32 { return 0 }, func(uint32) int32 { t.Fatal("invalid display queried"); return 0 }); err == nil {
		t.Fatal("no display was reported as a successful reading")
	}
	if _, err := readDisplayOn(nil, nil); err == nil {
		t.Fatal("missing framework was reported as a successful reading")
	}
	if _, err := readDisplayOn(func() uint32 { return 1 }, func(uint32) int32 { return -1 }); err == nil {
		t.Fatal("failed display query was accepted as a sleep measurement")
	}
}

func TestUSBProfilerFallback(t *testing.T) {
	t.Run("legacy data type remains supported", func(t *testing.T) {
		_, _, err := usbSnapshotDarwin(func(kind string) ([]byte, error) {
			if kind != "SPUSBDataType" {
				t.Fatalf("unexpected fallback: %s", kind)
			}
			return []byte("USB:\n    USB Bus:\n"), nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
	var queries []string
	hash, names, err := usbSnapshotDarwin(func(kind string) ([]byte, error) {
		queries = append(queries, kind)
		if kind == "SPUSBDataType" {
			return nil, nil
		}
		return []byte("USB:\n    USB 3.1 Bus:\n        Test Device:\n"), nil
	})
	if err != nil || hash == "" || len(names) != 1 || names[0] != "Test Device" || len(queries) != 2 {
		t.Fatalf("hash=%s names=%v queries=%v err=%v", hash, names, queries, err)
	}
	if _, _, err := usbSnapshotDarwin(func(string) ([]byte, error) { return nil, nil }); err == nil {
		t.Fatal("empty profiler output was accepted as a hardware snapshot")
	}
	denied := errors.New("permission denied")
	if _, _, err := usbSnapshotDarwin(func(string) ([]byte, error) { return nil, denied }); !errors.Is(err, denied) {
		t.Fatalf("profiler failure was hidden: %v", err)
	}
}

func TestUSBFallbackFailure(t *testing.T) {
	denied := errors.New("profiler unavailable")
	_, _, err := usbSnapshotDarwin(func(kind string) ([]byte, error) {
		if kind == "SPUSBDataType" {
			return nil, nil
		}
		return nil, denied
	})
	if !errors.Is(err, denied) {
		t.Fatalf("fallback failure hidden: %v", err)
	}
}

func TestUSBWatchingChangesAndCancellation(t *testing.T) {
	s := NewUSBSensor()
	s.every = time.Millisecond
	var reads atomic.Int32
	s.read = func() (string, []string, error) {
		switch reads.Add(1) {
		case 1:
			return "baseline", []string{"keyboard"}, nil
		case 2:
			return "", nil, errors.New("temporary profiler failure")
		default:
			return "changed", []string{"keyboard", "mouse"}, nil
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	alerts := make(chan Alert, 8)
	done := make(chan error, 1)
	go func() { done <- s.Start(ctx, alerts) }()
	select {
	case alert := <-alerts:
		if alert.Sensor != "usb" || alert.Level != AlertCritical {
			t.Fatal(alert)
		}
	case <-time.After(time.Second):
		t.Fatal("USB change was not reported")
	}
	// Repeated identical snapshots must not generate repeated alarms.
	for reads.Load() < 6 {
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 0 || s.lastHash != "changed" || len(s.lastDeviceNames) != 2 {
		t.Fatal("duplicate USB alert or stale snapshot")
	}
}

func TestUSBStopUnblocksAlertAndReportsBaselineFailure(t *testing.T) {
	s := NewUSBSensor()
	s.every = time.Millisecond
	denied := errors.New("profiler denied")
	s.read = func() (string, []string, error) { return "", nil, denied }
	if err := s.Start(context.Background(), make(chan Alert)); !errors.Is(err, denied) {
		t.Fatal(err)
	}
	var reads atomic.Int32
	pending := make(chan struct{})
	s.read = func() (string, []string, error) {
		if reads.Add(1) == 1 {
			return "before", nil, nil
		}
		close(pending)
		return "after", nil, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.Start(ctx, make(chan Alert)) }()
	<-pending
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("disarming blocked on USB alert delivery")
	}
}

func TestDisplayAvailabilityAndReadAdapter(t *testing.T) {
	savedMain, savedAsleep := mainDisplayID, displayIsAsleep
	defer func() { mainDisplayID, displayIsAsleep = savedMain, savedAsleep }()
	mainDisplayID, displayIsAsleep = nil, nil
	if NewScreenSensor().Available() {
		t.Fatal("missing display services are available")
	}
	mainDisplayID = func() uint32 { return 1 }
	displayIsAsleep = func(uint32) int32 { return 0 }
	if !NewScreenSensor().Available() {
		t.Fatal("display services unavailable")
	}
	on, err := isScreenOnDarwin()
	if err != nil || !on {
		t.Fatalf("on=%v err=%v", on, err)
	}
}
