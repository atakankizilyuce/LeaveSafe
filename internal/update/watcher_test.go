package update

import (
	"context"
	"errors"
	"testing"
	"time"
)

// harness drives a Watcher without waiting: sleeps are recorded and returned
// from immediately, and the clock advances by whatever was slept.
type harness struct {
	t        *testing.T
	now      time.Time
	slept    []time.Duration
	rounds   int
	maxRound int
}

func newHarness(t *testing.T, maxRound int) *harness {
	t.Helper()
	return &harness{
		t:        t,
		now:      time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC),
		maxRound: maxRound,
	}
}

func (h *harness) sleep(_ context.Context, d time.Duration) bool {
	h.slept = append(h.slept, d)
	h.now = h.now.Add(d)
	h.rounds++
	if h.rounds > h.maxRound {
		// Stop the loop the way a canceled context would.
		return false
	}
	return true
}

func (h *harness) clock() time.Time { return h.now }

// A first run has never checked, so it checks straight away.
func TestWatcherChecksImmediatelyOnAFirstRun(t *testing.T) {
	h := newHarness(t, 1)
	ledger := NewLedger(t.TempDir())
	var checks int

	Watcher{
		Interval: 24 * time.Hour,
		Ledger:   ledger,
		Now:      h.clock,
		Sleep:    h.sleep,
		Jitter:   func(time.Duration) time.Duration { return 0 },
		Check: func(context.Context, string) (Result, error) {
			checks++
			return Result{}, nil
		},
	}.Run(context.Background())

	if checks == 0 {
		t.Fatal("a first run did not check")
	}
	if len(h.slept) == 0 || h.slept[0] != 0 {
		t.Errorf("first wait was %v, want 0", h.slept)
	}
}

// A restart minutes after the last check must not query again: a copy in a crash
// loop would otherwise burn through the rate limit.
func TestWatcherWaitsOutTheIntervalAfterARestart(t *testing.T) {
	h := newHarness(t, 1)
	ledger := NewLedger(t.TempDir())
	if err := ledger.Save(Record{LastCheck: h.now.Add(-1 * time.Hour)}); err != nil {
		t.Fatalf("seed ledger: %v", err)
	}

	Watcher{
		Interval: 24 * time.Hour,
		Ledger:   ledger,
		Now:      h.clock,
		Sleep:    h.sleep,
		Jitter:   func(time.Duration) time.Duration { return 0 },
		Check:    func(context.Context, string) (Result, error) { return Result{}, nil },
	}.Run(context.Background())

	if len(h.slept) == 0 {
		t.Fatal("no wait was recorded")
	}
	if want := 23 * time.Hour; h.slept[0] != want {
		t.Errorf("first wait = %v, want %v", h.slept[0], want)
	}
}

// A clock that moved backwards must not produce a wait of weeks.
func TestWatcherHandlesAClockThatMovedBack(t *testing.T) {
	h := newHarness(t, 1)
	ledger := NewLedger(t.TempDir())
	if err := ledger.Save(Record{LastCheck: h.now.Add(30 * 24 * time.Hour)}); err != nil {
		t.Fatalf("seed ledger: %v", err)
	}

	Watcher{
		Interval: 24 * time.Hour,
		Ledger:   ledger,
		Now:      h.clock,
		Sleep:    h.sleep,
		Jitter:   func(time.Duration) time.Duration { return 0 },
		Check:    func(context.Context, string) (Result, error) { return Result{}, nil },
	}.Run(context.Background())

	if want := 24 * time.Hour; h.slept[0] != want {
		t.Errorf("first wait = %v, want one interval (%v)", h.slept[0], want)
	}
}

// A failure backs off by an hour rather than a full interval, and does not
// advance LastSuccess.
func TestWatcherBacksOffAfterAFailure(t *testing.T) {
	h := newHarness(t, 2)
	ledger := NewLedger(t.TempDir())
	var reported int

	Watcher{
		Interval: 24 * time.Hour,
		Ledger:   ledger,
		Now:      h.clock,
		Sleep:    h.sleep,
		Jitter:   func(time.Duration) time.Duration { return 0 },
		Check: func(context.Context, string) (Result, error) {
			return Result{}, errors.New("github is down")
		},
		Report:  func(Result) { reported++ },
		OnError: func(error) {},
	}.Run(context.Background())

	if len(h.slept) < 2 {
		t.Fatalf("waits = %v, want a backoff after the failure", h.slept)
	}
	if h.slept[1] != failureRetry {
		t.Errorf("backoff = %v, want %v", h.slept[1], failureRetry)
	}
	if reported != 0 {
		t.Error("a failed check reported an update")
	}

	rec := ledger.Load()
	if rec.LastCheck.IsZero() {
		t.Error("a failed check did not record the attempt")
	}
	if !rec.LastSuccess.IsZero() {
		t.Error("a failed check recorded a success")
	}
}

// Being told once per release is information; once per interval is nagging.
func TestWatcherReportsEachVersionOnce(t *testing.T) {
	h := newHarness(t, 3)
	ledger := NewLedger(t.TempDir())
	var reported []string

	Watcher{
		Interval: 24 * time.Hour,
		Ledger:   ledger,
		Now:      h.clock,
		Sleep:    h.sleep,
		Jitter:   func(time.Duration) time.Duration { return 0 },
		Check: func(context.Context, string) (Result, error) {
			return Result{Available: true, Latest: "v1.3.0"}, nil
		},
		Report: func(r Result) { reported = append(reported, r.Latest) },
	}.Run(context.Background())

	if len(reported) != 1 {
		t.Errorf("reported %v, want v1.3.0 exactly once", reported)
	}
}

// A newer release defeats the suppression.
func TestWatcherReportsANewVersionAfterAnOldOne(t *testing.T) {
	h := newHarness(t, 3)
	ledger := NewLedger(t.TempDir())
	var reported []string
	latest := "v1.3.0"

	Watcher{
		Interval: 24 * time.Hour,
		Ledger:   ledger,
		Now:      h.clock,
		Sleep:    h.sleep,
		Jitter:   func(time.Duration) time.Duration { return 0 },
		Check: func(context.Context, string) (Result, error) {
			r := Result{Available: true, Latest: latest}
			latest = "v1.4.0"
			return r, nil
		},
		Report: func(r Result) { reported = append(reported, r.Latest) },
	}.Run(context.Background())

	if len(reported) != 2 || reported[0] != "v1.3.0" || reported[1] != "v1.4.0" {
		t.Errorf("reported %v, want [v1.3.0 v1.4.0]", reported)
	}
}

// Switching to beta must not be silenced by a stable result already reported at
// the same version.
func TestWatcherChannelChangeClearsWhatWasReported(t *testing.T) {
	h := newHarness(t, 3)
	ledger := NewLedger(t.TempDir())
	var reported []string

	channel := ChannelStable
	round := 0

	Watcher{
		Interval: 24 * time.Hour,
		Ledger:   ledger,
		Now:      h.clock,
		Sleep:    h.sleep,
		Jitter:   func(time.Duration) time.Duration { return 0 },
		Settings: func() (bool, string) { return true, channel },
		Check: func(_ context.Context, ch string) (Result, error) {
			round++
			if round == 1 {
				channel = ChannelBeta
			}
			if ch == ChannelBeta {
				return Result{Available: true, Latest: "v1.3.0-beta"}, nil
			}
			return Result{Available: true, Latest: "v1.3.0"}, nil
		},
		Report: func(r Result) { reported = append(reported, r.Latest) },
	}.Run(context.Background())

	if len(reported) < 2 {
		t.Fatalf("reported %v, want the stable result then the beta one", reported)
	}
	if reported[0] != "v1.3.0" || reported[1] != "v1.3.0-beta" {
		t.Errorf("reported %v, want [v1.3.0 v1.3.0-beta]", reported)
	}
}

// Switching checking off from the phone stops the queries without stopping the
// loop, so switching it back on works too.
func TestWatcherHonoursCheckingBeingSwitchedOff(t *testing.T) {
	h := newHarness(t, 2)
	ledger := NewLedger(t.TempDir())
	var checks int

	Watcher{
		Interval: 24 * time.Hour,
		Ledger:   ledger,
		Now:      h.clock,
		Sleep:    h.sleep,
		Jitter:   func(time.Duration) time.Duration { return 0 },
		Settings: func() (bool, string) { return false, ChannelStable },
		Check: func(context.Context, string) (Result, error) {
			checks++
			return Result{Available: true, Latest: "v9.9.9"}, nil
		},
		Report: func(Result) { t.Error("a disabled check reported an update") },
	}.Run(context.Background())

	if checks != 0 {
		t.Errorf("the endpoint was queried %d times with checking off", checks)
	}
	if ledger.Load().LastCheck.IsZero() {
		t.Error("the schedule stopped advancing, so switching back on would query at once")
	}
}

// A canceled context ends the loop rather than leaking the goroutine.
func TestWatcherStopsOnContextCancel(t *testing.T) {
	ledger := NewLedger(t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		Watcher{
			Interval: time.Millisecond,
			Ledger:   ledger,
			Check:    func(context.Context, string) (Result, error) { return Result{}, nil },
		}.Run(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after the context was canceled")
	}
}

func TestWatcherDefaultsToADailyInterval(t *testing.T) {
	h := newHarness(t, 1)
	ledger := NewLedger(t.TempDir())
	if err := ledger.Save(Record{LastCheck: h.now}); err != nil {
		t.Fatalf("seed ledger: %v", err)
	}

	Watcher{
		Ledger: ledger,
		Now:    h.clock,
		Sleep:  h.sleep,
		Jitter: func(time.Duration) time.Duration { return 0 },
		Check:  func(context.Context, string) (Result, error) { return Result{}, nil },
	}.Run(context.Background())

	if h.slept[0] != DefaultInterval {
		t.Errorf("wait = %v, want %v", h.slept[0], DefaultInterval)
	}
}

// The jitter keeps every installation started by the same script from querying
// at the same instant, and must stay inside its share of the interval.
func TestWatcherJitterIsBounded(t *testing.T) {
	w := Watcher{Interval: 24 * time.Hour}
	for range 100 {
		got := w.jitter(24 * time.Hour)
		if got < 0 || got >= (24*time.Hour)/jitterFraction {
			t.Fatalf("jitter = %v, outside [0, %v)", got, (24*time.Hour)/jitterFraction)
		}
	}
	if got := w.jitter(time.Nanosecond); got != 0 {
		t.Errorf("jitter for a tiny interval = %v, want 0", got)
	}
}
