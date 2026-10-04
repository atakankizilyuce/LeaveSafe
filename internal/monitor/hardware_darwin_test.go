//go:build darwin

package monitor

import (
	"errors"
	"testing"
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
