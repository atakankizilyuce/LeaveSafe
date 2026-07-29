package state

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestLoadOnFirstRunReportsDisarmed(t *testing.T) {
	s := NewStore(t.TempDir(), "test")

	st, err := s.Load()
	if err != nil {
		t.Fatalf("Load with no file: %v", err)
	}
	if st.Armed {
		t.Error("a missing state file was read as armed")
	}
}

func TestSaveThenLoadRoundTrip(t *testing.T) {
	s := NewStore(t.TempDir(), "v1.2.3")
	before := time.Now().Add(-time.Second)

	if err := s.Save(true); err != nil {
		t.Fatalf("Save: %v", err)
	}

	st, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !st.Armed {
		t.Error("saved armed=true, loaded armed=false")
	}
	if st.Version != "v1.2.3" {
		t.Errorf("version = %q, want v1.2.3", st.Version)
	}
	if st.ChangedAt.Before(before) {
		t.Errorf("ChangedAt = %v, which predates the save", st.ChangedAt)
	}
}

func TestSaveOverwritesPreviousState(t *testing.T) {
	s := NewStore(t.TempDir(), "test")

	if err := s.Save(true); err != nil {
		t.Fatalf("Save(true): %v", err)
	}
	if err := s.Save(false); err != nil {
		t.Fatalf("Save(false): %v", err)
	}

	st, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if st.Armed {
		t.Error("state still reads as armed after being saved disarmed")
	}
}

func TestSaveLeavesNoTemporaryFilesBehind(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir, "test")

	for range 3 {
		if err := s.Save(true); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != fileName {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("directory holds %v, want just %s", names, fileName)
	}
}

func TestStateFileIsOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits do not apply on Windows")
	}
	dir := t.TempDir()
	s := NewStore(dir, "test")
	if err := s.Save(true); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(s.Path())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	// When the machine was left alone is not something to hand to every account
	// on the system.
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("state file mode = %v, want 0600", perm)
	}
}

func TestLoadReportsACorruptFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, fileName), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	s := NewStore(dir, "test")

	st, err := s.Load()
	if err == nil {
		t.Error("a corrupt state file loaded without an error")
	}
	if st.Armed {
		t.Error("a corrupt state file was read as armed")
	}
}

func TestClearRemovesTheFile(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir, "test")
	if err := s.Save(true); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := s.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if _, err := os.Stat(s.Path()); !os.IsNotExist(err) {
		t.Error("the state file survived Clear")
	}
	// Clearing an already-clear store is not an error.
	if err := s.Clear(); err != nil {
		t.Errorf("second Clear: %v", err)
	}
}

func TestSaveCreatesTheDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does", "not", "exist")
	s := NewStore(dir, "test")

	if err := s.Save(true); err != nil {
		t.Fatalf("Save into a missing directory: %v", err)
	}
	if _, err := os.Stat(s.Path()); err != nil {
		t.Errorf("state file was not created: %v", err)
	}
}
