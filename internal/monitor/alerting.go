package monitor

import "context"

// What the phone shows when a sensor fires.
//
// One place per sensor rather than one per platform. These sentences were
// written out three times over — once in each of the windows, linux and darwin
// files — which is three chances for the charger to say something different
// depending on which laptop it came out of, and no way to tell that it had
// happened short of reading all three.

// chargerAlert says what the charger having moved is worth saying.
//
// Unplugged is the alarm this sensor exists for. Plugged back in is worth
// knowing and is not an intrusion.
func chargerAlert(onAC bool) Alert {
	if !onAC {
		return Alert{
			Sensor:  "power",
			Level:   AlertCritical,
			Message: "Charger disconnected!",
		}
	}
	return Alert{
		Sensor:  "power",
		Level:   AlertWarning,
		Message: "Charger reconnected",
	}
}

// lidAlert says what the lid having moved is worth saying.
//
// Closing is the alarm: it is what somebody does to a laptop they are about to
// pick up. Opening is worth knowing and is not, by itself, an intrusion.
func lidAlert(open bool) Alert {
	if !open {
		return Alert{
			Sensor:  "lid",
			Level:   AlertCritical,
			Message: "Lid closed!",
		}
	}
	return Alert{
		Sensor:  "lid",
		Level:   AlertWarning,
		Message: "Lid opened",
	}
}

// screenAlert says what the display having changed is worth saying.
//
// Both directions are a warning rather than an alarm. A screen goes off on its
// own after a few minutes on every machine this product is for, so treating it
// as an intrusion would mean an alarm on all of them.
func screenAlert(on bool) Alert {
	if !on {
		return Alert{
			Sensor:  "screen",
			Level:   AlertWarning,
			Message: "Screen turned off!",
		}
	}
	return Alert{
		Sensor:  "screen",
		Level:   AlertWarning,
		Message: "Screen turned on",
	}
}

// sendAlert hands an alert to whoever is listening, and reports whether the
// sensor should carry on.
//
// The send is guarded by the context because it can block. The alert channel is
// buffered rather than unbounded, so a hub that stopped draining it would leave
// a sensor parked in a send that no disarm could interrupt — and a sensor that
// cannot be stopped is a worse fault than one that missed an event.
func sendAlert(ctx context.Context, alerts chan<- Alert, alert Alert) bool {
	select {
	case alerts <- alert:
		return true
	case <-ctx.Done():
		return false
	}
}
