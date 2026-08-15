package monitor

import (
	"context"
	"testing"
	"time"
)

// The only question that matters about a sensor: does it raise the alarm at the
// moment the hardware moves, in the words the phone will show, and does it stay
// quiet the rest of the time.
//
// These run on all three platforms because the answer has to be the same on all
// three. The rule used to be written out once per operating system, so the
// charger could have said something different depending on which laptop it came
// out of and no test would have noticed.

// ── the charger ────────────────────────────────────────────────────────────

// onCharger builds a power sensor reading from a script.
func onCharger(t *testing.T, script *readings[bool]) (*PowerSensor, chan Alert) {
	t.Helper()
	power := NewPowerSensor()
	power.every = pollFast
	power.read = script.next
	alerts, _, _ := watching(t, power)
	return power, alerts
}

func TestTheChargerComingOutIsTheAlarm(t *testing.T) {
	_, alerts := onCharger(t, scripted(true, false))

	alert := expectAlert(t, alerts)
	if alert.Level != AlertCritical {
		t.Errorf("the charger coming out was a %s, not an alarm", alert.Level)
	}
	if alert.Sensor != "power" || alert.Message != "Charger disconnected!" {
		t.Errorf("got %+v", alert)
	}
}

func TestPluggingBackInIsWorthKnowingAndIsNotAnAlarm(t *testing.T) {
	_, alerts := onCharger(t, scripted(true, false, true))

	if out := expectAlert(t, alerts); out.Level != AlertCritical {
		t.Fatalf("expected the disconnection first, got %+v", out)
	}
	back := expectAlert(t, alerts)
	if back.Level != AlertWarning {
		t.Errorf("reconnecting the charger was reported as %s", back.Level)
	}
	if back.Message != "Charger reconnected" {
		t.Errorf("got %q", back.Message)
	}
}

func TestAChargerThatStaysWhereItIsIsNotAnEvent(t *testing.T) {
	// The loop runs every two seconds for as long as the machine is armed. A
	// sensor that reported the state rather than the change would put an alert
	// on the phone every two seconds all night.
	_, alerts := onCharger(t, scripted(false))

	expectQuiet(t, alerts)
}

func TestArmingWithTheChargerAlreadyOutIsNotADisconnection(t *testing.T) {
	// Somebody who unplugs the laptop, walks to a meeting and arms it there has
	// not just had it taken.
	_, alerts := onCharger(t, scripted(false, false, false))

	expectQuiet(t, alerts)
}

func TestAPollThatFailsIsNotAnEventAndDoesNotEndTheWatch(t *testing.T) {
	// The reading can fail transiently — a PowerShell call that timed out, a
	// sysfs file that was busy. Ending the loop would hand the supervisor a
	// restart, and treating it as a reading would invent an event.
	_, alerts := onCharger(t, scriptedSteps(
		answers(true),
		refuses[bool](),
		refuses[bool](),
		answers(false),
	))

	if alert := expectAlert(t, alerts); alert.Message != "Charger disconnected!" {
		t.Errorf("after two failed polls the sensor said %q", alert.Message)
	}
}

func TestASensorThatCannotTakeItsFirstReadingSaysSo(t *testing.T) {
	// Returning nil would have the supervisor retire the loop as finished, and
	// the panel would go on reporting a sensor that is looking at nothing. The
	// failure is what puts the gap in cover on the screen.
	power := NewPowerSensor()
	power.every = pollFast
	power.read = scriptedSteps(refuses[bool]()).next

	_, _, done := watching(t, power)

	if err := expectStops(t, done); err == nil {
		t.Error("a sensor that could not read the charger returned as though it had finished its work")
	}
}

// ── the lid ────────────────────────────────────────────────────────────────

func onLid(t *testing.T, script *readings[bool]) chan Alert {
	t.Helper()
	lid := NewLidSensor()
	lid.every = pollFast
	lid.read = script.asked
	alerts, _, _ := watching(t, lid)
	return alerts
}

func TestClosingTheLidIsTheAlarm(t *testing.T) {
	alerts := onLid(t, scripted(open, closed))

	alert := expectAlert(t, alerts)
	if alert.Level != AlertCritical || alert.Message != "Lid closed!" {
		t.Errorf("got %+v", alert)
	}
}

func TestOpeningTheLidAgainIsReportedAndIsNotAnAlarm(t *testing.T) {
	alerts := onLid(t, scripted(open, closed, open))

	expectAlert(t, alerts)
	back := expectAlert(t, alerts)
	if back.Level != AlertWarning || back.Message != "Lid opened" {
		t.Errorf("got %+v", back)
	}
}

func TestArmingALaptopThatIsAlreadyShutRaisesNothing(t *testing.T) {
	// A laptop on a dock with the lid down is the ordinary way to leave a
	// machine at a desk. Assuming the lid was open before looking is what put
	// "Lid closed!" on the phone two seconds after arming.
	alerts := onLid(t, scripted(closed, closed, closed))

	expectQuiet(t, alerts)
}

func TestALidThatCannotBeReadIsNotALidThatMoved(t *testing.T) {
	// The lid is read by starting a process, which a busy machine can fail to
	// do. That is not the laptop being shut.
	alerts := onLid(t, scriptedSteps(
		answers(open),
		refuses[bool](),
		refuses[bool](),
		refuses[bool](),
	))

	expectQuiet(t, alerts)
}

// ── the screen ─────────────────────────────────────────────────────────────

func onScreen(t *testing.T, script *readings[bool]) chan Alert {
	t.Helper()
	screen := NewScreenSensor()
	screen.every = pollFast
	screen.read = script.asked
	alerts, _, _ := watching(t, screen)
	return alerts
}

func TestTheScreenGoingOffIsAWarningRatherThanAnAlarm(t *testing.T) {
	// A screen goes off on its own after a few minutes on every machine this
	// product is for. An alarm here would fire on all of them.
	alerts := onScreen(t, scripted(true, false))

	alert := expectAlert(t, alerts)
	if alert.Level != AlertWarning || alert.Message != "Screen turned off!" {
		t.Errorf("got %+v", alert)
	}
}

func TestTheScreenComingBackIsReportedToo(t *testing.T) {
	alerts := onScreen(t, scripted(true, false, true))

	expectAlert(t, alerts)
	if back := expectAlert(t, alerts); back.Message != "Screen turned on" {
		t.Errorf("got %+v", back)
	}
}

func TestArmingWithTheScreenAlreadyOffRaisesNothing(t *testing.T) {
	alerts := onScreen(t, scripted(false, false, false))

	expectQuiet(t, alerts)
}

// ── all of them ────────────────────────────────────────────────────────────

// polling is every sensor that watches one piece of hardware on a timer, built
// so that a test can drive it.
func polling(t *testing.T) map[string]Sensor {
	t.Helper()

	power := NewPowerSensor()
	power.every = pollFast
	power.read = scripted(true).next

	lid := NewLidSensor()
	lid.every = pollFast
	lid.read = scripted(open).asked

	screen := NewScreenSensor()
	screen.every = pollFast
	screen.read = scripted(true).asked

	return map[string]Sensor{"power": power, "lid": lid, "screen": screen}
}

func TestDisarmingStopsASensorAndIsNotAFailure(t *testing.T) {
	// A sensor that reported cancellation as a failure would have the
	// supervisor restart it, and the machine would go on being watched after
	// the user disarmed it.
	for name, sensor := range polling(t) {
		t.Run(name, func(t *testing.T) {
			_, stop, done := watching(t, sensor)

			time.Sleep(10 * time.Millisecond)
			stop()

			if err := expectStops(t, done); err != nil {
				t.Errorf("disarming was reported as a failure: %v", err)
			}
		})
	}
}

func TestEverySensorKnowsWhatItIsCalled(t *testing.T) {
	// The name is the handle the phone switches a sensor on and off by, and it
	// is written into the protocol — see internal/ws. A sensor that renamed
	// itself on one platform would be one the phone could not turn off there.
	//
	// Availability is deliberately not asked: on Windows the lid answers by
	// starting PowerShell, and this is a test about names.
	named := map[string]Sensor{
		"power":   NewPowerSensor(),
		"lid":     NewLidSensor(),
		"usb":     NewUSBSensor(),
		"screen":  NewScreenSensor(),
		"network": NewNetworkSensor(),
		"input":   NewInputSensor(),
	}

	for name, sensor := range named {
		if sensor.Name() != name {
			t.Errorf("the %s sensor calls itself %q", name, sensor.Name())
		}
		if sensor.DisplayName() == "" {
			t.Errorf("the %s sensor has nothing to show on a tile", name)
		}
		if err := sensor.Stop(); err != nil {
			t.Errorf("the %s sensor would not stop: %v", name, err)
		}
	}
}

func TestASensorNobodyIsListeningToCanStillBeDisarmed(t *testing.T) {
	// The alert channel is buffered, not unbounded. A hub that stopped draining
	// it used to leave the sensor parked in a send that no disarm could
	// interrupt — a machine that could not be told to stop watching.
	power := NewPowerSensor()
	power.every = pollFast
	power.read = scripted(true, false, true, false, true).next

	full := make(chan Alert) // nobody reads this
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- power.Start(ctx, full) }()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the sensor could not be stopped while an alert had nowhere to go")
	}
}
