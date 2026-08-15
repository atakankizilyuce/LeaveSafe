//go:build windows

package monitor

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// LidSensor monitors the laptop lid state on Windows.
type LidSensor struct {
	watch stateWatch[bool]

	// read is how the lid is asked and every is how often. Both are filled in
	// by the constructor; a test replaces them to drive the loop without a
	// laptop, and without waiting two seconds for every reading.
	read  func(context.Context) (bool, error)
	every time.Duration

	// availableOnce guards the one WMI query behind Available, which several
	// goroutines reach: the status broadcast on the heartbeat, and the hub when
	// a phone pairs.
	availableOnce sync.Once
	available     bool
	// known says the query has finished, so Available can answer from memory.
	// Read by callers that must not block on it — see monitor.AvailableNow.
	known atomic.Bool
}

func NewLidSensor() *LidSensor {
	return &LidSensor{read: isLidOpenWindows, every: 2 * time.Second}
}

func (s *LidSensor) Name() string        { return "lid" }
func (s *LidSensor) DisplayName() string { return "Lid State" }

// Available reports whether this machine has a lid to watch, by asking WMI
// whether it has a battery.
//
// The answer is worked out once and kept. It cannot change while the program
// runs — a desktop does not grow a battery — and the question is asked far more
// often than it looks: every status broadcast calls this, which is every
// fifteen seconds for as long as the program is running. Without the cache each
// one started a PowerShell process to ask WMI the same settled question again.
// It can take twenty seconds in the worst case, which is why nothing a client
// waits on calls this directly — see monitor.AvailableNow.
func (s *LidSensor) Available() bool {
	s.availableOnce.Do(func() {
		out, err := powershellOutput(context.Background(), availabilityTimeout,
			"(Get-WmiObject -Class Win32_Battery).Count")
		s.available = err == nil && parseHasBattery(string(out))
		s.known.Store(true)
	})
	return s.available
}

// AvailabilityKnown implements monitor.AvailabilityProber: it reports whether
// the WMI query above has finished, so a caller on a request path can decline
// to wait for it.
func (s *LidSensor) AvailabilityKnown() bool { return s.known.Load() }

func (s *LidSensor) Start(ctx context.Context, alerts chan<- Alert) error {
	return poll{
		every: s.every,
		read:  s.read,
		alert: lidAlert,
		watch: &s.watch,
	}.run(ctx, alerts)
}

func (s *LidSensor) Stop() error { return nil }

// isLidOpenWindows reads the lid state. ctx is the sensor's own context, so
// disarming stops the poll rather than waiting on a query that may not return.
func isLidOpenWindows(ctx context.Context) (bool, error) {
	out, err := powershellOutput(ctx, pollTimeout,
		"(Get-WmiObject -Namespace root/WMI -Class MSAcpi_LidStatus).LidStatus")
	if err != nil {
		return true, err // Assume open if we can't determine
	}
	return parseLidStatusWMI(string(out)), nil
}
