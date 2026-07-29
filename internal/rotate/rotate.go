// Package rotate provides an io.Writer that keeps a log file from growing
// without bound.
//
// LeaveSafe is meant to run for months on a laptop that is never administered.
// Nothing prunes the files it writes, so a log that only ever appends is a slow
// disk leak on someone else's machine. This rotates on size and keeps a fixed
// number of older files, which puts a hard ceiling on the bytes the program is
// responsible for.
package rotate

import (
	"fmt"
	"os"
	"sync"
)

const (
	// DefaultMaxBytes is the size at which the live file is rotated.
	DefaultMaxBytes int64 = 2 << 20 // 2 MiB
	// DefaultMaxFiles is how many rotated generations are kept alongside it.
	// With the default size that caps a log family at roughly 6 MiB.
	DefaultMaxFiles = 2
	// fileMode keeps logs readable only by their owner: the event log records
	// when the machine was left alone, which is not something to hand to every
	// account on the system.
	fileMode os.FileMode = 0o600
)

// Options configures the rotation policy. A zero value in any field means "use
// the default".
type Options struct {
	// MaxBytes is the size the live file may reach before it is rotated.
	MaxBytes int64
	// MaxFiles is how many rotated generations to keep. Zero keeps the default;
	// a negative value keeps none, so the file is simply truncated.
	MaxFiles int
}

func (o Options) withDefaults() Options {
	if o.MaxBytes <= 0 {
		o.MaxBytes = DefaultMaxBytes
	}
	if o.MaxFiles == 0 {
		o.MaxFiles = DefaultMaxFiles
	}
	if o.MaxFiles < 0 {
		o.MaxFiles = 0
	}
	return o
}

// Writer is an io.Writer that appends to a file and rotates it once it grows
// past the configured size. It is safe for concurrent use.
type Writer struct {
	mu   sync.Mutex
	path string
	opts Options
	file *os.File
	size int64
}

// Open opens path for appending, creating it and rotating it as needed. The
// parent directory must already exist.
func Open(path string, opts Options) (*Writer, error) {
	w := &Writer{path: path, opts: opts.withDefaults()}
	if err := w.open(); err != nil {
		return nil, err
	}
	return w, nil
}

// open attaches to the file on disk and records its current size. Callers must
// hold w.mu, or hold no references to w at all.
func (w *Writer) open() error {
	// #nosec G304 -- path comes from the app's own config dir, never user input
	f, err := os.OpenFile(w.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, fileMode)
	if err != nil {
		return fmt.Errorf("open %s: %w", w.path, err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("stat %s: %w", w.path, err)
	}
	w.file = f
	w.size = info.Size()
	return nil
}

// Path returns the path of the live file.
func (w *Writer) Path() string { return w.path }

// Write appends p, rotating first if it would push the file past MaxBytes.
//
// A single write is never split across two files. A record that is itself
// larger than MaxBytes is written whole to a freshly rotated file rather than
// truncated, because half a JSON line is worse than a file slightly over its
// budget.
func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		return 0, os.ErrClosed
	}
	if w.size > 0 && w.size+int64(len(p)) > w.opts.MaxBytes {
		if err := w.rotateLocked(); err != nil {
			// Rotation failing is not a reason to lose the record. Keep
			// appending to the file we already have: an oversized log beats a
			// missing alarm history.
			return w.writeLocked(p)
		}
	}
	return w.writeLocked(p)
}

func (w *Writer) writeLocked(p []byte) (int, error) {
	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

// rotateLocked closes the live file, shifts the generations along, and opens a
// fresh one. Callers must hold w.mu.
func (w *Writer) rotateLocked() error {
	if err := w.file.Close(); err != nil {
		return err
	}
	w.file = nil

	if w.opts.MaxFiles == 0 {
		// No generations kept: start the live file over.
		if err := os.Remove(w.path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return w.open()
	}

	// The oldest generation falls off the end, then each remaining one shifts
	// up by one, then the live file becomes generation 1.
	oldest := generationPath(w.path, w.opts.MaxFiles)
	if err := os.Remove(oldest); err != nil && !os.IsNotExist(err) {
		return err
	}
	for i := w.opts.MaxFiles - 1; i >= 1; i-- {
		from, to := generationPath(w.path, i), generationPath(w.path, i+1)
		if err := os.Rename(from, to); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := os.Rename(w.path, generationPath(w.path, 1)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return w.open()
}

// Rotate forces a rotation regardless of the current size.
func (w *Writer) Rotate() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return os.ErrClosed
	}
	return w.rotateLocked()
}

// Close closes the live file. Writes after Close return os.ErrClosed.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

// generationPath returns the name of the nth rotated generation of path, where
// generation 1 is the most recently rotated.
func generationPath(path string, n int) string {
	return fmt.Sprintf("%s.%d", path, n)
}

// Generations returns the rotated files for path, newest first, skipping any
// that do not exist. Readers use it to walk backwards through history when the
// live file alone does not hold enough records.
func Generations(path string, maxFiles int) []string {
	if maxFiles <= 0 {
		maxFiles = DefaultMaxFiles
	}
	var out []string
	for i := 1; i <= maxFiles; i++ {
		p := generationPath(path, i)
		if _, err := os.Stat(p); err == nil {
			out = append(out, p)
		}
	}
	return out
}
