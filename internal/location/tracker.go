package location

import (
	"context"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

const (
	// defaultPoll is how often live providers are asked for a new fix.
	defaultPoll = 60 * time.Second
	// minPoll floors the configured interval. Wi-Fi scans and geolocation
	// lookups are not free, and polling faster than this buys nothing for a
	// machine that is supposed to be sitting still.
	minPoll = 15 * time.Second
	// defaultMaxAge is how long a fix stays usable. Beyond it, reporting the
	// position as current would be a guess about what happened in between.
	defaultMaxAge = 10 * time.Minute
	// locateTimeout bounds a single provider call.
	locateTimeout = 20 * time.Second
)

// Snapshot is everything the phone needs to render the location panel
// honestly: where the machine is, where it was when armed, and how far those
// two are apart.
type Snapshot struct {
	// Available reports whether any provider could run at all.
	Available bool `json:"available"`
	// Fix is the best current estimate, or nil if there is none.
	Fix *Fix `json:"location,omitempty"`
	// Anchor is the position recorded when the system was armed, or nil.
	Anchor *Fix `json:"anchor,omitempty"`
	// MovedM is the distance from the anchor to the current fix.
	MovedM float64 `json:"moved_m"`
	// Moved reports that the machine is further from its anchor than the two
	// error radii can explain — it has genuinely been carried somewhere.
	Moved bool `json:"moved"`
}

// Tracker polls the configured providers while the system is armed and keeps
// the best available fix.
type Tracker struct {
	mu        sync.RWMutex
	providers []Provider
	poll      time.Duration
	maxAge    time.Duration

	anchor *Fix // from the phone, captured at arm time
	live   *Fix // from a polling provider

	onUpdate func(Snapshot)
	cancel   context.CancelFunc
	running  bool
}

// NewTracker creates a tracker over the given providers. Providers reporting
// themselves unavailable are dropped immediately rather than polled and failed
// once a minute forever.
func NewTracker(providers []Provider, poll time.Duration) *Tracker {
	if poll < minPoll {
		poll = defaultPoll
	}

	usable := make([]Provider, 0, len(providers))
	for _, p := range providers {
		if p.Available() {
			usable = append(usable, p)
			continue
		}
		log.WithField("source", p.Source()).Debug("Location provider unavailable, not polling it")
	}

	return &Tracker{
		providers: usable,
		poll:      poll,
		maxAge:    defaultMaxAge,
	}
}

// SetUpdateCallback registers a function called whenever the position changes.
func (t *Tracker) SetUpdateCallback(fn func(Snapshot)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.onUpdate = fn
}

// HasProviders reports whether any provider is usable on this machine.
func (t *Tracker) HasProviders() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.providers) > 0
}

// SetAnchor records the phone's own position. The phone is next to the laptop
// when the system is armed, which makes its GPS the most precise statement
// available about where the laptop is — until the laptop moves.
func (t *Tracker) SetAnchor(fix *Fix) {
	if fix == nil {
		return
	}
	fix.Source = SourcePhone
	if fix.Timestamp.IsZero() {
		fix.Timestamp = time.Now()
	}

	t.mu.Lock()
	t.anchor = fix
	t.mu.Unlock()

	log.WithField("accuracy_m", fix.AccuracyM).Info("Location anchor recorded from phone")
	t.notify()
}

// Snapshot returns the current position view.
func (t *Tracker) Snapshot() Snapshot {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.snapshotLocked()
}

func (t *Tracker) snapshotLocked() Snapshot {
	best, movedM, moved := selectFix(t.anchor, t.live, time.Now(), t.maxAge)
	return Snapshot{
		Available: len(t.providers) > 0 || t.anchor != nil,
		Fix:       best,
		Anchor:    t.anchor,
		MovedM:    movedM,
		Moved:     moved,
	}
}

// Start begins polling. It is safe to call when already running.
func (t *Tracker) Start(ctx context.Context) {
	t.mu.Lock()
	if t.running || len(t.providers) == 0 {
		t.mu.Unlock()
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	t.cancel = cancel
	t.running = true
	poll := t.poll
	t.mu.Unlock()

	go t.loop(runCtx, poll)
}

// Stop halts polling. The last fix is deliberately kept, so the phone can still
// show a last known position instead of an empty panel.
func (t *Tracker) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cancel != nil {
		t.cancel()
		t.cancel = nil
	}
	t.running = false
}

func (t *Tracker) loop(ctx context.Context, poll time.Duration) {
	t.refresh(ctx)

	ticker := time.NewTicker(poll)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.refresh(ctx)
		}
	}
}

// refresh asks every provider for a fix and keeps the most precise one.
func (t *Tracker) refresh(ctx context.Context) {
	t.mu.RLock()
	providers := make([]Provider, len(t.providers))
	copy(providers, t.providers)
	t.mu.RUnlock()

	var best *Fix
	for _, p := range providers {
		if ctx.Err() != nil {
			return
		}

		callCtx, cancel := context.WithTimeout(ctx, locateTimeout)
		fix, err := p.Locate(callCtx)
		cancel()

		if err != nil {
			log.WithField("source", p.Source()).Debugf("Location provider failed: %v", err)
			continue
		}
		if fix == nil {
			continue
		}
		if fix.Timestamp.IsZero() {
			fix.Timestamp = time.Now()
		}
		if best == nil || fix.AccuracyM < best.AccuracyM {
			best = fix
		}
	}

	if best == nil {
		return
	}

	t.mu.Lock()
	t.live = best
	t.mu.Unlock()

	log.WithFields(log.Fields{
		"source":     best.Source,
		"accuracy_m": best.AccuracyM,
	}).Debug("Location updated")

	t.notify()
}

func (t *Tracker) notify() {
	t.mu.RLock()
	cb := t.onUpdate
	snap := t.snapshotLocked()
	t.mu.RUnlock()

	if cb != nil {
		cb(snap)
	}
}

// selectFix decides which of the anchor and the live fix describes where the
// machine is now, and how far it has moved.
//
// Ranking purely by accuracy would be wrong. The phone anchor is the most
// precise source and also the one that silently goes stale: it says where the
// laptop was when it was armed, so the moment the laptop is carried off, its
// tight error circle is centered on the wrong place.
//
// So when a live fix lands further from the anchor than the two error radii can
// jointly explain, the two cannot be describing the same place. The anchor is
// then known to be wrong, and the less precise live fix wins on the grounds
// that it is at least about the present.
func selectFix(anchor, live *Fix, now time.Time, maxAge time.Duration) (best *Fix, movedM float64, moved bool) {
	anchor = freshOrNil(anchor, now, maxAge)
	live = freshOrNil(live, now, maxAge)

	switch {
	case anchor == nil && live == nil:
		return nil, 0, false
	case live == nil:
		return anchor, 0, false
	case anchor == nil:
		return live, 0, false
	}

	movedM = DistanceM(anchor, live)
	moved = movedM > anchor.AccuracyM+live.AccuracyM

	if moved {
		return live, movedM, true
	}
	if live.AccuracyM < anchor.AccuracyM {
		return live, movedM, false
	}
	return anchor, movedM, false
}

// freshOrNil drops a fix that is too old to describe the present. The anchor is
// exempt: it is explicitly a statement about arm time, and the caller renders
// its age. Discarding it would throw away the only reference point for
// detecting that the machine has moved.
func freshOrNil(fix *Fix, now time.Time, maxAge time.Duration) *Fix {
	if fix == nil {
		return nil
	}
	if fix.Source != SourcePhone && now.Sub(fix.Timestamp) > maxAge {
		return nil
	}
	return fix
}
