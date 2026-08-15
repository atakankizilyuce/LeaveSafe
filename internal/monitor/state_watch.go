package monitor

// stateWatch turns a stream of readings of one piece of hardware into the one
// question every polling sensor asks: has it just changed, and which way.
//
// It lives apart from any one platform for the same reason activityWatch does.
// The charger, the lid and the screen are each a thing that is one way or the
// other, read on a timer; the rule for turning a reading into an alert is the
// same wherever the reading came from, and there is no version of it that is
// Windows' own.
//
// The first reading is a baseline and never an alert. That is the whole of the
// difference between a sensor that reports what changed and one that reports
// what it assumed: a sensor that decided which way the hardware was pointing
// before it had looked raises an alarm on the machine of anybody who armed a
// laptop that was already docked with its lid shut.
type stateWatch[T comparable] struct {
	known bool
	last  T
}

// sample records one reading and reports whether it changed anything.
//
// False for the first reading, and false for every reading that says what the
// one before it said — so a sensor polling twice a second raises one alert when
// the charger comes out rather than one every two seconds for as long as it is
// out.
func (w *stateWatch[T]) sample(now T) bool {
	if !w.known {
		w.known = true
		w.last = now
		return false
	}
	if now == w.last {
		return false
	}
	w.last = now
	return true
}

// forget drops the baseline, so the next reading is taken as a new one.
//
// A sensor whose loop is restarted by the supervisor has been away for as long
// as the restart took, and the hardware may have moved in the meantime. What it
// must not do is report that movement as an event nobody was there to cause —
// the machine was not being watched while the loop was down, and an alarm about
// a gap in the watching is not the same thing as an alarm about the laptop.
func (w *stateWatch[T]) forget() {
	w.known = false
}
