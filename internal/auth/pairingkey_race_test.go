package auth

import (
	"sync"
	"testing"
)

// The pairing key is read from several goroutines at once — the dashboard's
// five-second repaint, the QR renderer, the writer that persists it for a
// headless restart — while `rotate-key` replaces it from the console's. The
// readers used to take it without the lock, which is a data race on the one
// value the whole pairing story rests on, and precisely the value a rotation
// exists to change.
//
// Under -race this fails outright on the old code. Without it, the assertions
// still hold: whatever a reader saw has to be a whole 16-digit key, not half of
// one and half of another.
func TestReadingThePairingKeyWhileItRotates(t *testing.T) {
	mgr, err := NewManager()
	if err != nil {
		t.Fatalf("auth manager: %v", err)
	}

	const rounds = 200
	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		for range rounds {
			if _, err := mgr.Regenerate(); err != nil {
				t.Errorf("regenerate: %v", err)
				return
			}
		}
	}()

	check := func(read func() string, want int) {
		defer wg.Done()
		for range rounds {
			if got := read(); len(got) != want {
				t.Errorf("read a key of length %d, want %d: %q", len(got), want, got)
				return
			}
		}
	}

	go check(mgr.RawPairingKey, 16)
	go check(mgr.PairingKey, 19) // 16 digits and three dashes

	wg.Wait()
}
