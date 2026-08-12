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

// Starting the program looks now, even though the daily interval has hours left
// on it.
//
// The interval is right for a copy that stays up for weeks and wrong for the
// moment somebody starts one: they are sitting in front of the screen, a release
// they want may have been cut an hour ago, and waiting out the rest of
// yesterday's interval means the one person looking is the one person not told.
func TestAStartLooksWithoutWaitingOutTheInterval(t *testing.T) {
	h := newHarness(t, 1)
	ledger := NewLedger(t.TempDir())
	if err := ledger.Save(Record{LastCheck: h.now.Add(-1 * time.Hour)}); err != nil {
		t.Fatalf("seed ledger: %v", err)
	}

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
		t.Fatal("a start an hour after the last check did not look")
	}
	if len(h.slept) == 0 {
		t.Fatal("no wait was recorded")
	}
	if h.slept[0] != 0 {
		t.Errorf("first wait = %v, want none", h.slept[0])
	}
}

// The one thing that still holds a start back, and the case the daily interval
// was really protecting against: a copy restarted every few seconds by a service
// manager would spend a rate limit in a minute.
func TestAStartInsideTheFloorWaitsRatherThanLooking(t *testing.T) {
	h := newHarness(t, 1)
	ledger := NewLedger(t.TempDir())
	if err := ledger.Save(Record{LastCheck: h.now.Add(-1 * time.Minute)}); err != nil {
		t.Fatalf("seed ledger: %v", err)
	}

	Watcher{
		Interval:     24 * time.Hour,
		StartupFloor: 15 * time.Minute,
		Ledger:       ledger,
		Now:          h.clock,
		Sleep:        h.sleep,
		Jitter:       func(time.Duration) time.Duration { return 0 },
		Check:        func(context.Context, string) (Result, error) { return Result{}, nil },
	}.Run(context.Background())

	if len(h.slept) == 0 {
		t.Fatal("no wait was recorded")
	}
	if want := 14 * time.Minute; h.slept[0] != want {
		t.Errorf("first wait = %v, want %v — a crash loop would query on every restart",
			h.slept[0], want)
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

	// A start waits the floor at most, whatever the recorded time says. The
	// thing being guarded against is a wait of weeks, and fifteen minutes is
	// not it.
	if floor := DefaultStartupFloor; h.slept[0] > floor {
		t.Errorf("first wait = %v, want no more than the %v floor", h.slept[0], floor)
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
	h := newHarness(t, 2)
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

	// The second wait, because the first one belongs to the start and is
	// measured against the floor rather than the interval.
	if len(h.slept) < 2 {
		t.Fatalf("only %d waits were recorded", len(h.slept))
	}
	if h.slept[1] != DefaultInterval {
		t.Errorf("wait = %v, want %v", h.slept[1], DefaultInterval)
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

// A tenth of a daily interval is nearly two and a half hours. Left uncapped, a
// copy someone just started would sit on a waiting fix for most of an afternoon —
// which is worse than the startup-only check this replaced, in exactly the case
// the feature exists for.
func TestWatcherFirstCheckIsPrompt(t *testing.T) {
	w := Watcher{Interval: 24 * time.Hour}
	for range 200 {
		if got := w.jitter(24 * time.Hour); got > jitterCap {
			t.Fatalf("jitter = %v, past the %v cap", got, jitterCap)
		}
	}

	// A short interval keeps its proportional jitter rather than the cap, so a
	// six-hour interval is not delayed by a fixed five minutes either way.
	if got := w.jitter(10 * time.Minute); got >= time.Minute {
		t.Errorf("jitter for a 10m interval = %v, want under a minute", got)
	}
}

// Shutting down while a failed check is backing off ends the schedule. Waiting
// the retry out and then checking again would mean a laptop being shut down
// still reaching for GitHub on its way out.
func TestWatcherStopsWhenShutDownDuringAFailureBackoff(t *testing.T) {
	// One round: the scheduled wait succeeds, then the failure's retry sleep is
	// the one that reports the context gone.
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
			return Result{}, errors.New("github is down")
		},
	}.Run(context.Background())

	if checks != 1 {
		t.Errorf("checked %d times, want the one attempt before the shutdown", checks)
	}
}
