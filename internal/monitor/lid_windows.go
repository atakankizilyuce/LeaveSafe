//go:build windows

package monitor

import (
	"context"
	"time"
)

// LidSensor monitors the laptop lid state on Windows.
type LidSensor struct {
	lastOpen    bool
	initialized bool
}

func NewLidSensor() *LidSensor {
	return &LidSensor{}
}

func (s *LidSensor) Name() string        { return "lid" }
func (s *LidSensor) DisplayName() string { return "Lid State" }

func (s *LidSensor) Available() bool {
	// Check if this is a laptop by querying battery info.
	//
	// Bounded, because this runs while the sensors are registered and that
	// happens before the server binds: on a machine with no battery the query
	// can sit for half a minute, and every second of it is a second the phone
	// cannot reach the laptop. A probe that does not answer is not a laptop
	// lid as far as this is concerned.
	out, err := powershellOutput(context.Background(),
		"(Get-WmiObject -Class Win32_Battery).Count")
	if err != nil {
		return false
	}
	return parseHasBattery(string(out))
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
	out, err := powershellOutput(ctx,
		"(Get-WmiObject -Namespace root/WMI -Class MSAcpi_LidStatus).LidStatus")
	if err != nil {
		return true, err // Assume open if we can't determine
	}
	return parseLidStatusWMI(string(out)), nil
}
