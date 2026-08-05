package location

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeProvider drives the tracker without touching the network.
type fakeProvider struct {
	mu        sync.Mutex
	source    Source
	available bool
	fix       *Fix
	err       error
	calls     int
}

func (p *fakeProvider) Source() Source { return p.source }
func (p *fakeProvider) Available() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.available
}

func (p *fakeProvider) Locate(context.Context) (*Fix, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if p.err != nil {
		return nil, p.err
	}
	if p.fix == nil {
		return nil, nil
	}
	cp := *p.fix
	return &cp, nil
}

func (p *fakeProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func at(lat, lon, accuracy float64, src Source, ts time.Time) *Fix {
	return &Fix{Latitude: lat, Longitude: lon, AccuracyM: accuracy, Source: src, Timestamp: ts}
}

func TestSelectFix(t *testing.T) {
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	const maxAge = 10 * time.Minute

	tests := []struct {
		name       string
		anchor     *Fix
		live       *Fix
		wantSource Source
		wantNil    bool
		wantMoved  bool
	}{
		{
			name:    "nothing known",
			wantNil: true,
		},
		{
			name:       "only the anchor",
			anchor:     at(41.0, 29.0, 8, SourcePhone, now),
			wantSource: SourcePhone,
		},
		{
			name:       "only a live fix",
			live:       at(41.0, 29.0, 25000, SourceIP, now),
			wantSource: SourceIP,
		},
		{
			name: "agreeing sources: the precise one wins",
			// An IP fix 3 km away with a 25 km radius is perfectly consistent
			// with the anchor, so it tells us nothing new and the 8 m anchor
			// remains the best statement about where the machine is.
			anchor:     at(41.0, 29.0, 8, SourcePhone, now),
			live:       at(41.027, 29.0, 25000, SourceIP, now),
			wantSource: SourcePhone,
		},
		{
			name: "live fix is more precise than the anchor",
			// A Wi-Fi fix 20 m away with a 30 m radius still agrees with the
			// anchor, but it is both fresher and tighter.
			anchor:     at(41.0, 29.0, 60, SourcePhone, now),
			live:       at(41.00018, 29.0, 30, SourceWiFi, now),
			wantSource: SourceWiFi,
		},
		{
			name: "the machine has been carried away",
			// 800 m apart with radii of 8 m and 30 m. The two cannot both be
			// right, and only one of them is about the present.
			anchor:     at(41.0, 29.0, 8, SourcePhone, now),
			live:       at(41.0072, 29.0, 30, SourceWiFi, now),
			wantSource: SourceWiFi,
			wantMoved:  true,
		},
		{
			name: "a coarse live fix cannot convict a precise anchor",
			// 3 km apart, but the IP fix admits to 25 km of error. That is not
			// evidence of movement, so the anchor stands.
			anchor:     at(41.0, 29.0, 8, SourcePhone, now),
			live:       at(41.027, 29.0, 25000, SourceIP, now),
			wantSource: SourcePhone,
			wantMoved:  false,
		},
		{
			name:       "a stale live fix is discarded",
			anchor:     at(41.0, 29.0, 8, SourcePhone, now),
			live:       at(41.5, 29.0, 30, SourceWiFi, now.Add(-30*time.Minute)),
			wantSource: SourcePhone,
		},
		{
			name: "the anchor survives its own age",
			// The anchor is explicitly a statement about arm time, so aging it
			// out would throw away the only reference for detecting movement.
			anchor:     at(41.0, 29.0, 8, SourcePhone, now.Add(-6*time.Hour)),
			wantSource: SourcePhone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, movedM, moved := selectFix(tt.anchor, tt.live, now, maxAge)

			if tt.wantNil {
				if got != nil {
					t.Fatalf("got %+v, want no fix", got)
				}
				return
			}
			if got == nil {
				// The return is redundant to the test — Fatal already ends it —
				// but it is what makes the dereference below unreachable to a
				// reader and to the analyzer, which does not always know that
				// Fatal has no way back.
				t.Fatal("got no fix, want one")
				return
			}
			if got.Source != tt.wantSource {
				t.Errorf("source = %q, want %q", got.Source, tt.wantSource)
			}
			if moved != tt.wantMoved {
				t.Errorf("moved = %v (%.0f m), want %v", moved, movedM, tt.wantMoved)
			}
		})
	}
}

func TestTracker_PicksMostPreciseProvider(t *testing.T) {
	now := time.Now()
	coarse := &fakeProvider{source: SourceIP, available: true, fix: at(41.0, 29.0, 25000, SourceIP, now)}
	precise := &fakeProvider{source: SourceWiFi, available: true, fix: at(41.0, 29.0, 30, SourceWiFi, now)}

	tr := NewTracker([]Provider{coarse, precise}, time.Minute)
	tr.refresh(context.Background())

	snap := tr.Snapshot()
	if snap.Fix == nil {
		t.Fatal("no fix after refresh")
	}
	if snap.Fix.Source != SourceWiFi {
		t.Errorf("source = %q, want the more precise %q", snap.Fix.Source, SourceWiFi)
	}
}

// TestTracker_SkipsUnavailableProviders keeps a provider that has already said
// it cannot work from being polled and failing once a minute forever.
func TestTracker_SkipsUnavailableProviders(t *testing.T) {
	dead := &fakeProvider{source: SourceWiFi, available: false}
	alive := &fakeProvider{source: SourceIP, available: true, fix: at(41.0, 29.0, 25000, SourceIP, time.Now())}

	tr := NewTracker([]Provider{dead, alive}, time.Minute)
	tr.refresh(context.Background())

	if dead.callCount() != 0 {
		t.Errorf("unavailable provider was polled %d times", dead.callCount())
	}
	if alive.callCount() != 1 {
		t.Errorf("available provider polled %d times, want 1", alive.callCount())
	}
}

func TestTracker_SurvivesProviderFailure(t *testing.T) {
	broken := &fakeProvider{source: SourceWiFi, available: true, err: errors.New("scan failed")}
	working := &fakeProvider{source: SourceIP, available: true, fix: at(41.0, 29.0, 25000, SourceIP, time.Now())}

	tr := NewTracker([]Provider{broken, working}, time.Minute)
	tr.refresh(context.Background())

	snap := tr.Snapshot()
	if snap.Fix == nil {
		t.Fatal("one provider failing lost the fix from the other")
	}
	if snap.Fix.Source != SourceIP {
		t.Errorf("source = %q, want %q", snap.Fix.Source, SourceIP)
	}
}

func TestTracker_AnchorIsReportedAndMeasuredAgainst(t *testing.T) {
	now := time.Now()
	// 800 m north of the anchor, tightly enough to be believed.
	live := &fakeProvider{source: SourceWiFi, available: true, fix: at(41.0072, 29.0, 30, SourceWiFi, now)}

	tr := NewTracker([]Provider{live}, time.Minute)
	tr.SetAnchor(at(41.0, 29.0, 8, SourcePhone, now))
	tr.refresh(context.Background())

	snap := tr.Snapshot()
	if snap.Anchor == nil {
		t.Fatal("anchor missing from snapshot")
	}
	if !snap.Moved {
		t.Error("moved = false, want true: the machine is 800 m from where it was armed")
	}
	if snap.MovedM < 700 || snap.MovedM > 900 {
		t.Errorf("moved_m = %.0f, want roughly 800", snap.MovedM)
	}
}

func TestTracker_UpdateCallbackFires(t *testing.T) {
	var (
		mu    sync.Mutex
		calls int
	)

	live := &fakeProvider{source: SourceIP, available: true, fix: at(41.0, 29.0, 25000, SourceIP, time.Now())}
	tr := NewTracker([]Provider{live}, time.Minute)
	tr.SetUpdateCallback(func(Snapshot) {
		mu.Lock()
		calls++
		mu.Unlock()
	})

	tr.refresh(context.Background())

	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Errorf("update callback fired %d times, want 1", calls)
	}
}

// TestTracker_StopKeepsLastFix is what lets the phone show a last known
// position after disarming rather than an empty panel.
func TestTracker_StopKeepsLastFix(t *testing.T) {
	live := &fakeProvider{source: SourceIP, available: true, fix: at(41.0, 29.0, 25000, SourceIP, time.Now())}
	tr := NewTracker([]Provider{live}, time.Minute)

	tr.refresh(context.Background())
	tr.Stop()

	if tr.Snapshot().Fix == nil {
		t.Error("stopping the tracker discarded the last known position")
	}
}

func TestNewTracker_FloorsThePollInterval(t *testing.T) {
	tr := NewTracker(nil, time.Second)
	if tr.poll < minPoll {
		t.Errorf("poll = %v, want at least %v", tr.poll, minPoll)
	}
}
