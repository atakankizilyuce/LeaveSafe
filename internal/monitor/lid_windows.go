//go:build windows

package monitor

import (
	"context"
	"sync"
	"time"
)

// LidSensor monitors the laptop lid state on Windows.
type LidSensor struct {
	lastOpen    bool
	initialized bool

	// availableOnce guards the one WMI query behind Available, which several
	// goroutines reach: the status broadcast on the heartbeat, and the hub when
	// a phone pairs.
	availableOnce sync.Once
	available     bool
}

func NewLidSensor() *LidSensor {
	return &LidSensor{}
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
func (s *LidSensor) Available() bool {
	s.availableOnce.Do(func() {
		out, err := powershellOutput(context.Background(), availabilityTimeout,
			"(Get-WmiObject -Class Win32_Battery).Count")
		s.available = err == nil && parseHasBattery(string(out))
	})
	return s.available
}

func (s *LidSensor) Start(ctx context.Context, alerts chan<- Alert) error {
	s.lastOpen = true
	s.initialized = true

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			open, err := isLidOpenWindows(ctx)
			if err != nil {
				continue
			}
			if open != s.lastOpen {
				if !open {
					alerts <- Alert{
						Sensor:  "lid",
						Level:   AlertCritical,
						Message: "Lid closed!",
					}
				} else {
					alerts <- Alert{
						Sensor:  "lid",
						Level:   AlertWarning,
						Message: "Lid opened",
					}
				}
				s.lastOpen = open
			}
		}
	}
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
