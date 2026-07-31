package monitor

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// failingSensor fails its first n starts, then blocks until it is canceled.
type failingSensor struct {
	name      string
	failures  int32
	starts    atomic.Int32
	available bool
}

func (f *failingSensor) Name() string        { return f.name }
func (f *failingSensor) DisplayName() string { return f.name }
func (f *failingSensor) Available() bool     { return f.available }
func (f *failingSensor) Stop() error         { return nil }

func (f *failingSensor) Start(ctx context.Context, _ chan<- Alert) error {
	if f.starts.Add(1) <= f.failures {
		return errors.New("driver went away")
	}
	<-ctx.Done()
	return ctx.Err()
}

// waitFor polls cond until it holds or the deadline passes.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	// Generous: safe restarts a failed loop after a two-second backoff.
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// A sensor whose driver returns an error used to be retired for the life of the
// process. The supervisor restarts only on panic, the closure caught the error
// and returned normally, and m.cancels kept its entry — so every later
// StartEnabled skipped the sensor as already running. One transient failure on a
// busy machine silently removed it, and nothing ever brought it back.
func TestAFailedSensorIsRestarted(t *testing.T) {
	s := &failingSensor{name: "power", failures: 2, available: true}
	m := NewManager()
	m.Register(s)
	m.Enable("power")

	m.StartEnabled()
	defer m.StopAll()

	waitFor(t, "the sensor to be restarted past its failures", func() bool {
		return s.starts.Load() > 2
	})
	waitFor(t, "the failure record to clear once it is watching again", func() bool {
		return m.Failure("power") == ""
	})
}

// While it is not watching, it must not be reported as though it were. An alarm
// that claims coverage it does not have is worse than one that claims none: the
// user reads the tally and walks away.
func TestAFailedSensorIsReportedAsFailed(t *testing.T) {
	// Fails often enough that the record is observable between restarts.
	s := &failingSensor{name: "usb", failures: 1 << 30, available: true}
	m := NewManager()
	m.Register(s)
	m.Enable("usb")

	m.StartEnabled()
	defer m.StopAll()

	waitFor(t, "the failure to be recorded", func() bool {
		return m.Failure("usb") != ""
	})
	if got := m.Failure("usb"); got != "driver went away" {
		t.Errorf("failure = %q, want the driver's own reason", got)
	}
	// Enabled and available are both still true — which is exactly why the
	// failure has to be carried separately for the panel to be honest.
	if !m.IsEnabled("usb") {
		t.Error("the sensor was disabled by its own failure; it should still be enabled")
	}
}

// Switching a sensor off is a choice, not a fault, and must not leave a failure
// showing on the panel.
func TestDisablingClearsTheFailure(t *testing.T) {
	s := &failingSensor{name: "lid", failures: 1 << 30, available: true}
	m := NewManager()
	m.Register(s)
	m.Enable("lid")

	m.StartEnabled()

	waitFor(t, "the failure to be recorded", func() bool { return m.Failure("lid") != "" })

	m.Disable("lid")
	if got := m.Failure("lid"); got != "" {
		t.Errorf("failure after Disable = %q, want it cleared", got)
	}
}

// A sensor that returns nil while it is still supposed to be watching has also
// stopped watching. Reading that as "finished its work" is what retired the
// loop, so it counts as a failure too.
func TestAQuietExitCountsAsAFailure(t *testing.T) {
	s := &quietSensor{}
	m := NewManager()
	m.Register(s)
	m.Enable("screen")

	m.StartEnabled()
	defer m.StopAll()

	waitFor(t, "the quiet exit to be restarted", func() bool { return s.starts.Load() > 1 })
}

// quietSensor returns nil immediately without being asked to stop.
type quietSensor struct{ starts atomic.Int32 }

func (q *quietSensor) Name() string        { return "screen" }
func (q *quietSensor) DisplayName() string { return "screen" }
func (q *quietSensor) Available() bool     { return true }
func (q *quietSensor) Stop() error         { return nil }
func (q *quietSensor) Start(context.Context, chan<- Alert) error {
	q.starts.Add(1)
	return nil
}
