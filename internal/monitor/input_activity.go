package monitor

// activityWatch turns a stream of idle readings into the one question the input
// sensor asks: has someone been at this machine long enough to say so.
//
// It lives apart from any one platform's sensor because none of it is about a
// platform. Every backend answers the same question — how many seconds since
// the last keypress — and the rule for turning that into an alert is the same
// wherever the number came from.
//
// It reports once and then stays quiet until the machine has been left alone
// again for five seconds, so a person working at the laptop raises one alert
// rather than one every second.
type activityWatch struct {
	threshold int
	busy      int
	quiet     int
	alerted   bool
}

// sample records one idle reading and reports whether it completes an alert.
//
// A negative reading means ioreg could not be asked at all, which is not
// evidence of anyone touching the machine — so it counts as quiet rather than
// as activity.
func (w *activityWatch) sample(idleSeconds float64) bool {
	if idleSeconds < 0 || idleSeconds >= 2 {
		w.busy = 0
		w.quiet++
		if w.alerted && w.quiet >= 5 {
			w.alerted = false
		}
		return false
	}

	w.quiet = 0
	if w.alerted {
		return false
	}
	w.busy++
	if w.busy < w.threshold {
		return false
	}
	w.alerted = true
	w.busy = 0
	return true
}
