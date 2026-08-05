package safe

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
)

// The package logs every recovered panic at error level, which would bury the
// test output in stack traces for panics the tests raise on purpose.
func TestMain(m *testing.M) {
	log.SetOutput(io.Discard)
	m.Run()
}

// restartBackoffForTest swaps the supervision backoff so a restart test does
// not have to wait the production delay, and returns the previous value.
//
// Only ever called from the test goroutine, before Supervise and again in
// cleanup. Supervise reads the value on the caller's goroutine for exactly this
// reason: reading it from the supervised goroutine would race with the restore.
func restartBackoffForTest(d time.Duration) time.Duration {
	old := restartBackoff
	restartBackoff = d
	return old
}

func TestDoReportsPanicAndKeepsCallerAlive(t *testing.T) {
	var gotName string
	var gotValue any
	var gotStack []byte
	SetPanicHandler(func(name string, value any, stack []byte) {
		gotName, gotValue, gotStack = name, value, stack
	})
	t.Cleanup(func() { SetPanicHandler(nil) })

	ok := Do("unit", func() { panic(errors.New("boom")) })

	if ok {
		t.Error("Do reported success for a function that panicked")
	}
	if gotName != "unit" {
		t.Errorf("handler got name %q, want %q", gotName, "unit")
	}
	if err, isErr := gotValue.(error); !isErr || err.Error() != "boom" {
		t.Errorf("handler got value %v, want the boom error", gotValue)
	}
	if len(gotStack) == 0 {
		t.Error("handler got an empty stack trace")
	}
}

func TestDoReportsSuccess(t *testing.T) {
	ran := false
	if !Do("unit", func() { ran = true }) {
		t.Error("Do reported failure for a function that returned normally")
	}
	if !ran {
		t.Error("Do did not run the function")
	}
}

func TestGoRecoversWithoutRestarting(t *testing.T) {
	var calls atomic.Int32
	done := make(chan struct{})
	SetPanicHandler(func(string, any, []byte) { close(done) })
	t.Cleanup(func() { SetPanicHandler(nil) })

	Go("unit", func() {
		calls.Add(1)
		panic("boom")
	})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the panic was never reported")
	}

	// Go is one-shot: a panic must not turn into a restart loop.
	time.Sleep(50 * time.Millisecond)
	if n := calls.Load(); n != 1 {
		t.Errorf("function ran %d times, want 1", n)
	}
}

func TestSuperviseRestartsAfterPanic(t *testing.T) {
	old := restartBackoffForTest(10 * time.Millisecond)
	t.Cleanup(func() { restartBackoffForTest(old) })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	starts := 0
	settled := make(chan struct{})

	Supervise(ctx, "unit", func(context.Context) {
		mu.Lock()
		starts++
		n := starts
		mu.Unlock()
		if n < 3 {
			panic("boom")
		}
		close(settled)
		<-ctx.Done()
	})

	select {
	case <-settled:
	case <-time.After(5 * time.Second):
		mu.Lock()
		n := starts
		mu.Unlock()
		t.Fatalf("supervised loop never got past its panics: %d starts", n)
	}
}

func TestSuperviseStopsOnNormalReturn(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var calls atomic.Int32
	Supervise(ctx, "unit", func(context.Context) { calls.Add(1) })

	// A loop that finishes its work is done; restarting it would run the work
	// twice.
	time.Sleep(100 * time.Millisecond)
	if n := calls.Load(); n != 1 {
		t.Errorf("function ran %d times after returning normally, want 1", n)
	}
}

func TestSuperviseStopsWhenContextIsCanceled(t *testing.T) {
	old := restartBackoffForTest(10 * time.Millisecond)
	t.Cleanup(func() { restartBackoffForTest(old) })

	ctx, cancel := context.WithCancel(context.Background())
	var calls atomic.Int32

	Supervise(ctx, "unit", func(context.Context) {
		calls.Add(1)
		panic("boom")
	})

	time.Sleep(50 * time.Millisecond)
	cancel()
	time.Sleep(50 * time.Millisecond)
	before := calls.Load()
	time.Sleep(100 * time.Millisecond)

	if after := calls.Load(); after != before {
		t.Errorf("loop restarted %d more times after cancellation", after-before)
	}
}

func TestPanicInHandlerDoesNotEscape(t *testing.T) {
	SetPanicHandler(func(string, any, []byte) { panic("handler boom") })
	t.Cleanup(func() { SetPanicHandler(nil) })

	// A panic escaping report would crash the test binary rather than fail it.
	if Do("unit", func() { panic("boom") }) {
		t.Error("Do reported success for a function that panicked")
	}
}
