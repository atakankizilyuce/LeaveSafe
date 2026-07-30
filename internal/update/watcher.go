package update

import (
	"context"
	"math/rand/v2"
	"time"
)

// DefaultInterval is how often to check when nothing says otherwise.
const DefaultInterval = 24 * time.Hour

// failureRetry is how long to wait after a failed check.
//
// A transient GitHub outage should not cost a whole day of not knowing, and a
// persistent one must not turn into a hammer. An hour is short enough that a
// blip is recovered from within the day and long enough that a sustained failure
// costs 24 requests rather than thousands.
const failureRetry = time.Hour

// jitterFraction bounds the random delay added before the first check, as a
// fraction of the interval.
//
// Without it every installation started by the same reboot, update or login
// script would query at the same moment. Sparkle and Omaha both do this.
const jitterFraction = 10

// jitterCap bounds that delay in absolute terms.
//
// A tenth of a daily interval is nearly two and a half hours, which would mean a
// copy just started says nothing about a waiting fix for most of the afternoon —
// worse, in the case that matters most, than the startup-only check this replaced.
// A few minutes spreads a reboot's worth of installations perfectly well, and the
// user who just launched the program still hears about a fix while they are
// sitting in front of it.
const jitterCap = 5 * time.Minute

// Watcher runs update checks on a schedule and reports what it finds.
//
// The schedule survives restarts through the Ledger: a copy caught in a crash
// loop must not query GitHub on every start, and a machine rebooted daily must
// not have its schedule reset every morning.
//
// Every field that touches time or the network is injectable, so the whole
// schedule can be tested in milliseconds.
type Watcher struct {
	// Interval is how often to check. Zero means DefaultInterval.
	Interval time.Duration

	// Settings reports whether checking is on and which channel to use. It is
	// consulted afresh each round rather than captured once, so a change made
	// from the phone takes effect without a restart. Nil means always on and the
	// stable channel.
	Settings func() (enabled bool, channel string)

	// Check performs one check against the given channel.
	Check func(ctx context.Context, channel string) (Result, error)

	// Ledger persists the schedule. Required.
	Ledger *Ledger

	// Report is called for a newer release that has not already been reported.
	Report func(Result)

	// OnError is called for a failed check. A failure is not the user's problem
	// — GitHub being unreachable says nothing about this machine — so callers
	// generally log it and no more.
	OnError func(error)

	// Now and Sleep exist so a test can drive the schedule. Sleep reports false
	// when the context ended, which is the signal to stop.
	Now   func() time.Time
	Sleep func(ctx context.Context, d time.Duration) bool

	// Jitter returns the extra delay to add before the first check. Nil means a
	// random fraction of the interval.
	Jitter func(interval time.Duration) time.Duration
}

// Run checks on schedule until ctx ends.
func (w Watcher) Run(ctx context.Context) {
	interval := w.Interval
	if interval <= 0 {
		interval = DefaultInterval
	}

	lastChannel := ""
	first := true

	for {
		enabled, channel := w.settings()
		channel = NormalizeChannel(channel)

		rec := w.Ledger.Load()

		// Switching channel invalidates what was already reported: someone who
		// has just opted into betas should hear about the newest beta even
		// though the stable result at that version was reported already.
		if lastChannel != "" && channel != lastChannel {
			rec.LastSeenLatest = ""
			_ = w.Ledger.Save(rec)
		}
		lastChannel = channel

		wait := w.until(rec, interval)
		if first {
			wait += w.jitter(interval)
			first = false
		}
		if !w.sleep(ctx, wait) {
			return
		}

		// Re-read rather than trusting the record from before the wait: an
		// on-demand check may have run while this goroutine was asleep.
		rec = w.Ledger.Load()

		if !enabled {
			// Keep the schedule ticking rather than spinning: checking may be
			// switched back on from the phone at any point.
			rec.LastCheck = w.now()
			_ = w.Ledger.Save(rec)
			continue
		}

		result, err := w.Check(ctx, channel)
		rec.LastCheck = w.now()
		if err != nil {
			_ = w.Ledger.Save(rec)
			if w.OnError != nil {
				w.OnError(err)
			}
			if !w.sleep(ctx, failureRetry) {
				return
			}
			continue
		}

		rec.LastSuccess = rec.LastCheck
		if result.Available && result.Latest != rec.LastSeenLatest {
			rec.LastSeenLatest = result.Latest
			if w.Report != nil {
				w.Report(result)
			}
		}
		_ = w.Ledger.Save(rec)
	}
}

// until reports how long to wait before the next check.
func (w Watcher) until(rec Record, interval time.Duration) time.Duration {
	if rec.LastCheck.IsZero() {
		return 0
	}
	elapsed := w.now().Sub(rec.LastCheck)
	switch {
	case elapsed < 0:
		// The recorded check is in the future, so the clock moved back. Waiting
		// out the difference could mean waiting weeks; wait one interval.
		return interval
	case elapsed >= interval:
		return 0
	default:
		return interval - elapsed
	}
}

func (w Watcher) settings() (bool, string) {
	if w.Settings == nil {
		return true, ChannelStable
	}
	return w.Settings()
}

func (w Watcher) now() time.Time {
	if w.Now == nil {
		return time.Now()
	}
	return w.Now()
}

func (w Watcher) jitter(interval time.Duration) time.Duration {
	if w.Jitter != nil {
		return w.Jitter(interval)
	}
	span := min(interval/jitterFraction, jitterCap)
	if span <= 0 {
		return 0
	}
	return rand.N(span) //nolint:gosec // scheduling jitter, not a secret
}

func (w Watcher) sleep(ctx context.Context, d time.Duration) bool {
	if w.Sleep != nil {
		return w.Sleep(ctx, d)
	}
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
