package logsink

import (
	"bytes"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// blocked is an io.Writer that never returns, standing in for the thing this
// package exists for: a pipe whose other end nobody is reading.
type blocked struct {
	entered chan struct{}
	once    sync.Once
}

func (b *blocked) Write(p []byte) (int, error) {
	b.once.Do(func() { close(b.entered) })
	select {}
}

// recorder is an io.Writer a test can read back, guarded because the sink
// writes from a goroutine of its own.
type recorder struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (r *recorder) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.Write(p)
}

func (r *recorder) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.String()
}

// waitFor polls until want reports true, or fails the test. Polling rather
// than a fixed sleep: what is being waited for is another goroutine getting
// its turn, and how long that takes is the machine's business.
func waitFor(t *testing.T, why string, want func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !want() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", why)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestAWriterThatNeverReturnsDoesNotStopTheCaller(t *testing.T) {
	// The whole point. An armed machine stopped watching because the write
	// behind its logger blocked: logrus holds one mutex across that write, so
	// the first blocked line stopped every later line in the process, and the
	// first statement in each sensor loop is a line.
	out := &blocked{entered: make(chan struct{})}
	w := New(out, 4)
	defer func() { _ = w.Close() }()

	if _, err := w.Write([]byte("the first line, which blocks\n")); err != nil {
		t.Fatalf("first write: %v", err)
	}
	<-out.entered

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 100 {
			_, _ = w.Write([]byte("and a hundred more behind it\n"))
		}
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("writing behind a blocked sink blocked the caller")
	}
}

func TestLinesReachTheWriterInOrder(t *testing.T) {
	out := &recorder{}
	w := New(out, 16)
	defer func() { _ = w.Close() }()

	for _, line := range []string{"one\n", "two\n", "three\n"} {
		if _, err := w.Write([]byte(line)); err != nil {
			t.Fatalf("write %q: %v", line, err)
		}
	}

	waitFor(t, "the three lines", func() bool {
		return out.String() == "one\ntwo\nthree\n"
	})
}

func TestTheBytesAreCopiedBeforeTheyAreQueued(t *testing.T) {
	// logrus formats into a buffer it reuses, so the slice handed to Write is
	// the caller's and is overwritten the moment Write returns. A sink that
	// queued the slice itself would write whatever the next line happened to
	// put there.
	out := &recorder{}
	w := New(out, 16)
	defer func() { _ = w.Close() }()

	shared := []byte("the original line\n")
	if _, err := w.Write(shared); err != nil {
		t.Fatalf("write: %v", err)
	}
	copy(shared, []byte("OVERWRITTEN......\n"))

	waitFor(t, "the original line", func() bool {
		return out.String() == "the original line\n"
	})
}

func TestWhatWasDroppedIsSaidRatherThanLost(t *testing.T) {
	// Dropping is the price of never blocking, and a silent drop would be the
	// same kind of lie this whole product is built against: a log that looks
	// complete and is not.
	release := make(chan struct{})
	out := &gated{release: release}
	w := New(out, 2)
	defer func() { _ = w.Close() }()

	// The first goes to the writer, which is waiting on release. The next two
	// fill the queue behind it; everything after that has nowhere to go.
	for i := range 40 {
		if _, err := w.Write([]byte("a line\n")); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}

	close(release)

	waitFor(t, "the count of what was dropped", func() bool {
		return strings.Contains(out.String(), "log lines were dropped")
	})
}

func TestClosingTwiceIsNotAPanic(t *testing.T) {
	// Shutdown paths run more than once often enough that this is worth
	// stating rather than assuming.
	w := New(&recorder{}, 4)
	if err := w.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func TestWriteAfterCloseIsNotAnError(t *testing.T) {
	// The logger is process-wide and outlives whatever closed the sink. A
	// write that arrives late has nowhere to go, and saying so would only
	// print a complaint through the logger that is trying to print.
	w := New(&recorder{}, 4)
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	n, err := w.Write([]byte("after the end\n"))
	if err != nil {
		t.Fatalf("write after close: %v", err)
	}
	if n != len("after the end\n") {
		t.Fatalf("write after close reported %d bytes, want %d", n, len("after the end\n"))
	}
}

func TestAWriterThatFailsIsNotRetriedForever(t *testing.T) {
	// A sink whose writer errors has nowhere to report it — reporting it
	// through the logger would go straight back into this sink. It is dropped,
	// and the caller is not told, because the caller is a log line.
	out := &failing{}
	w := New(out, 4)
	defer func() { _ = w.Close() }()

	if _, err := w.Write([]byte("into the void\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	waitFor(t, "the failing writer to be tried", func() bool { return out.tried() })
}

// gated is an io.Writer that holds its first write until release is closed,
// so a test can fill the queue behind it.
type gated struct {
	release chan struct{}
	once    sync.Once
	mu      sync.Mutex
	buf     bytes.Buffer
}

func (g *gated) Write(p []byte) (int, error) {
	g.once.Do(func() { <-g.release })
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.buf.Write(p)
}

func (g *gated) String() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.buf.String()
}

// failing is an io.Writer that refuses everything.
type failing struct {
	mu  sync.Mutex
	saw bool
}

func (f *failing) Write(_ []byte) (int, error) {
	f.mu.Lock()
	f.saw = true
	f.mu.Unlock()
	return 0, errors.New("nowhere to write")
}

func (f *failing) tried() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.saw
}
