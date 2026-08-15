package monitor

import (
	"context"
	"time"
)

// poll is the loop every sensor that watches one piece of hardware runs.
//
// Written once because it had been written nine times: the charger, the lid and
// the display, on each of three operating systems, and the nine copies differed
// only in which reading they took and which alert they raised. Nine copies of a
// loop is nine places for one of them to stop clearing its baseline, or to
// treat a failed poll as an event, and no way to notice short of reading all of
// them side by side.
type poll struct {
	// every is how often the hardware is looked at.
	every time.Duration

	// read takes one look. It is given the sensor's own context, so disarming
	// interrupts a query rather than waiting on one that may not return.
	read func(context.Context) (bool, error)

	// alert says what a change is worth saying.
	alert func(bool) Alert

	// watch is what tells a change from a repetition. It belongs to the sensor
	// rather than to the loop, because a sensor outlives any one run of it.
	watch *stateWatch[bool]
}

// run watches until the sensor is stopped, or until it cannot watch.
func (p poll) run(ctx context.Context, alerts chan<- Alert) error {
	// Where the hardware is now is the baseline, and it is read rather than
	// assumed. Assuming put "Lid closed!" on the phone of anybody who armed a
	// laptop that was already shut on a dock.
	//
	// This may also be a restart: what the hardware did while the loop was down
	// happened on a machine nobody was watching, and is not an event now.
	p.watch.forget()

	now, err := p.read(ctx)
	if err != nil {
		// A first reading that cannot be taken is reported rather than worked
		// around. The supervisor records the failure and the panel shows the
		// gap, which is the whole difference between a sensor that is not
		// watching and one that says it is.
		return err
	}
	p.watch.sample(now)

	ticker := time.NewTicker(p.every)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			now, err := p.read(ctx)
			if err != nil {
				// One unreadable poll is not the hardware moving. The next one
				// decides.
				continue
			}
			if !p.watch.sample(now) {
				continue
			}
			if !sendAlert(ctx, alerts, p.alert(now)) {
				return nil
			}
		}
	}
}
