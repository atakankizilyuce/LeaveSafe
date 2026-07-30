package update

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestLedgerRoundTrip(t *testing.T) {
	l := NewLedger(t.TempDir())

	want := Record{
		LastCheck:      time.Now().UTC().Truncate(time.Second),
		LastSuccess:    time.Now().UTC().Truncate(time.Second),
		LastSeenLatest: "v1.3.0",
	}
	if err := l.Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got := l.Load()
	if !got.LastCheck.Equal(want.LastCheck) {
		t.Errorf("LastCheck = %v, want %v", got.LastCheck, want.LastCheck)
	}
	if !got.LastSuccess.Equal(want.LastSuccess) {
		t.Errorf("LastSuccess = %v, want %v", got.LastSuccess, want.LastSuccess)
	}
	if got.LastSeenLatest != want.LastSeenLatest {
		t.Errorf("LastSeenLatest = %q, want %q", got.LastSeenLatest, want.LastSeenLatest)
	}
}

// A missing file means nothing has ever been checked, which is the same thing a
// first run means.
func TestLedgerMissingFileIsNeverChecked(t *testing.T) {
	got := NewLedger(t.TempDir()).Load()
	if !got.LastCheck.IsZero() || got.LastSeenLatest != "" {
		t.Errorf("a missing ledger produced %+v", got)
	}
}

// A truncated file costs one extra query, which is not worth failing over.
func TestLedgerCorruptFileIsNeverChecked(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ledgerFile), []byte(`{"last_check":`), 0o600); err != nil {
		t.Fatalf("seed corrupt ledger: %v", err)
	}

	got := NewLedger(dir).Load()
	if !got.LastCheck.IsZero() {
		t.Errorf("a corrupt ledger produced %+v", got)
	}
}

func TestLedgerOverwrites(t *testing.T) {
	l := NewLedger(t.TempDir())

	if err := l.Save(Record{LastSeenLatest: "v1.0.0"}); err != nil {
		t.Fatalf("first Save: %v", err)
	}
	if err := l.Save(Record{LastSeenLatest: "v2.0.0"}); err != nil {
		t.Fatalf("second Save: %v", err)
	}

	if got := l.Load().LastSeenLatest; got != "v2.0.0" {
		t.Errorf("LastSeenLatest = %q, want v2.0.0", got)
	}
}

// The file is written into the config directory, which holds the pairing key and
// the event log. Nothing here should widen those permissions.
func TestLedgerFileIsOwnerOnly(t *testing.T) {
	if os.Getuid() == -1 {
		t.Skip("POSIX permissions are not meaningful on this platform")
	}

	l := NewLedger(t.TempDir())
	if err := l.Save(Record{LastSeenLatest: "v1.0.0"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(l.Path())
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("ledger mode = %04o, want 0600", perm)
	}
}

// Save is called from a background goroutine while Load can run on the command
// path, so the two must not race.
func TestLedgerConcurrentAccess(t *testing.T) {
	l := NewLedger(t.TempDir())

	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			if err := l.Save(Record{LastSeenLatest: "v1.0.0", LastCheck: time.Now()}); err != nil {
				t.Errorf("Save %d: %v", i, err)
			}
		})
		wg.Go(func() {
			_ = l.Load()
		})
	}
	wg.Wait()
}
