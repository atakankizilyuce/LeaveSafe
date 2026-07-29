package rotate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeAll(t *testing.T, w *Writer, lines ...string) {
	t.Helper()
	for _, l := range lines {
		if _, err := w.Write([]byte(l)); err != nil {
			t.Fatalf("write %q: %v", l, err)
		}
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestRotatesOnceOverMaxBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	w, err := Open(path, Options{MaxBytes: 10, MaxFiles: 2})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer w.Close()

	writeAll(t, w, "aaaaa", "bbbbb", "ccccc")

	// "aaaaa"+"bbbbb" exactly fills 10 bytes; "ccccc" would exceed it, so the
	// first two end up in generation 1 and the third starts the live file.
	if got := readFile(t, path); got != "ccccc" {
		t.Errorf("live file = %q, want %q", got, "ccccc")
	}
	if got := readFile(t, path+".1"); got != "aaaaabbbbb" {
		t.Errorf("generation 1 = %q, want %q", got, "aaaaabbbbb")
	}
}

func TestKeepsOnlyMaxFilesGenerations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	w, err := Open(path, Options{MaxBytes: 4, MaxFiles: 2})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer w.Close()

	writeAll(t, w, "1111", "2222", "3333", "4444")

	if _, err := os.Stat(path + ".3"); !os.IsNotExist(err) {
		t.Error("a third generation survived a MaxFiles of 2")
	}
	// The oldest content must be the one that fell off the end.
	if got := readFile(t, path+".2"); got == "1111" {
		t.Error("the oldest generation was kept instead of being dropped")
	}
	if got := readFile(t, path); got != "4444" {
		t.Errorf("live file = %q, want %q", got, "4444")
	}
}

func TestNegativeMaxFilesTruncatesInsteadOfKeeping(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	w, err := Open(path, Options{MaxBytes: 4, MaxFiles: -1})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer w.Close()

	writeAll(t, w, "1111", "2222")

	if _, err := os.Stat(path + ".1"); !os.IsNotExist(err) {
		t.Error("a generation was kept despite MaxFiles being negative")
	}
	if got := readFile(t, path); got != "2222" {
		t.Errorf("live file = %q, want %q", got, "2222")
	}
}

func TestOversizedRecordIsWrittenWhole(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	w, err := Open(path, Options{MaxBytes: 8, MaxFiles: 1})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer w.Close()

	big := strings.Repeat("x", 40)
	writeAll(t, w, "seed", big)

	// Truncating a record would corrupt the JSONL line it belongs to, so the
	// whole thing is written even though it blows the size budget.
	if got := readFile(t, path); got != big {
		t.Errorf("live file holds %d bytes, want the whole %d-byte record", len(got), len(big))
	}
}

func TestReopenAppendsRatherThanTruncating(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")

	w, err := Open(path, Options{MaxBytes: 1000, MaxFiles: 2})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	writeAll(t, w, "first\n")
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	w2, err := Open(path, Options{MaxBytes: 1000, MaxFiles: 2})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer w2.Close()
	writeAll(t, w2, "second\n")

	if got := readFile(t, path); got != "first\nsecond\n" {
		t.Errorf("after reopen the file holds %q, want both records", got)
	}
}

func TestWriteAfterCloseFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	w, err := Open(path, Options{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := w.Write([]byte("x")); err == nil {
		t.Error("writing to a closed writer succeeded")
	}
}

func TestGenerationsListsNewestFirstAndSkipsMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")

	for _, name := range []string{"events.jsonl.1", "events.jsonl.3"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}

	got := Generations(path, 3)
	want := []string{path + ".1", path + ".3"}
	if len(got) != len(want) {
		t.Fatalf("Generations returned %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Generations[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestDefaultsAreApplied(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	w, err := Open(path, Options{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer w.Close()

	if w.opts.MaxBytes != DefaultMaxBytes {
		t.Errorf("MaxBytes = %d, want the default %d", w.opts.MaxBytes, DefaultMaxBytes)
	}
	if w.opts.MaxFiles != DefaultMaxFiles {
		t.Errorf("MaxFiles = %d, want the default %d", w.opts.MaxFiles, DefaultMaxFiles)
	}
}
