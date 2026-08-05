package ws

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/leavesafe/leavesafe/internal/location"
)

// Location tracking runs only while armed: there is no reason to scan for Wi-Fi
// or hit a geolocation service while the user is sitting at the machine, and
// every reason not to. That makes arming the one moment it has to start — and
// restoring an armed state after a restart is arming as far as the laptop is
// concerned, so it has to start there too.

// countingProvider is a location source that records being asked. It is enough
// to prove the tracker's loop is running, which is what starting it means.
type countingProvider struct {
	asked atomic.Int32
}

func (p *countingProvider) Source() location.Source { return location.SourceWiFi }
func (p *countingProvider) Available() bool         { return true }

func (p *countingProvider) Locate(context.Context) (*location.Fix, error) {
	p.asked.Add(1)
	return &location.Fix{Latitude: 51.5, Longitude: -0.12, AccuracyM: 25}, nil
}

// trackedHub gives a hub a tracker with one observable source behind it, and
// hands back the source.
func trackedHub(t *testing.T) (*Hub, *countingProvider) {
	t.Helper()
	hub, _ := hubWithSensors(t, "power")
	provider := &countingProvider{}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	hub.SetLocationTracker(ctx, location.NewTracker([]location.Provider{provider}, time.Minute))
	return hub, provider
}

func TestArmingStartsLocationTracking(t *testing.T) {
	hub, provider := trackedHub(t)

	hub.Arm()

	waitUntil(t, "arming did not start location tracking", func() bool {
		return provider.asked.Load() > 0
	})
}

// A laptop that was armed when it was shut down comes back armed, and it has to
// come back tracking. Without this the machine says ARMED on the phone while
// the position panel quietly reports the last fix from before the restart.
func TestRestoringAnArmedStateStartsLocationTracking(t *testing.T) {
	hub, provider := trackedHub(t)

	hub.RestoreArmed(time.Now().Add(-time.Hour))

	if !hub.IsArmed() {
		t.Fatal("the armed state was not restored")
	}
	waitUntil(t, "the restored armed state did not start location tracking", func() bool {
		return provider.asked.Load() > 0
	})
}

// Nothing installs a tracker on a build with location switched off, and arming
// then has to be arming rather than a nil call.
func TestArmingWithNoTrackerInstalledIsHarmless(t *testing.T) {
	hub, _ := hubWithSensors(t, "power")

	hub.Arm()

	if !hub.IsArmed() {
		t.Error("the machine did not arm without a tracker")
	}
}

// Taking the tracker away has to take the starting with it, or arming would
// keep reaching for a tracker that is no longer there.
func TestRemovingTheTrackerStopsArmingFromStartingIt(t *testing.T) {
	hub, provider := trackedHub(t)
	hub.SetLocationTracker(context.Background(), nil)

	hub.Arm()

	time.Sleep(50 * time.Millisecond)
	if provider.asked.Load() != 0 {
		t.Error("arming started a tracker that had been removed")
	}
}
