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

// activityQuietSeconds is the idle reading at or above which the machine counts
// as left alone. Below it, somebody is at the keyboard.
//
// Named because Windows has no idle clock to read — it answers with the tick of
// the last input instead — and the number that turns its answer into one of
// these has to be this number rather than a second copy of it.
const activityQuietSeconds = 2.0

// sample records one idle reading and reports whether it completes an alert.
//
// A negative reading means ioreg could not be asked at all, which is not
// evidence of anyone touching the machine — so it counts as quiet rather than
// as activity.
func (w *activityWatch) sample(idleSeconds float64) bool {
	if idleSeconds < 0 || idleSeconds >= activityQuietSeconds {
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
