//go:build windows

package monitor

import (
	"sync"
	"testing"
	"time"
)

// Windows has no idle clock to read. It answers with the tick of the last
// input, so the sensor asks "has that moved since I last looked" and hands the
// answer to the same rule the other two platforms use — which is the point:
// when this product decides somebody is at the machine must not depend on which
// machine it is.

// ticking is an idle clock that moves every time it is read, which is what
// somebody working at the machine looks like.
func ticking() func() uint32 {
	var mu sync.Mutex
	var at uint32
	return func() uint32 {
		mu.Lock()
		defer mu.Unlock()
		at++
		return at
	}
}

func atInput(t *testing.T, threshold int, read func() uint32, grace time.Duration) chan Alert {
	t.Helper()
	input := NewInputSensorWithThreshold(threshold)
	input.every = pollFast
	input.grace = grace
	input.read = read
	alerts, _, _ := watching(t, input)
	return alerts
}

func TestSomebodyAtTheMachineRaisesOneAlarm(t *testing.T) {
	alerts := atInput(t, 3, ticking(), 0)

	alert := expectAlert(t, alerts)
	if alert.Level != AlertCritical || alert.Sensor != "input" {
		t.Errorf("got %+v", alert)
	}
	// One alert, not one a second, for as long as they keep working.
	expectQuiet(t, alerts)
}

func TestAMachineNobodyIsTouchingSaysNothing(t *testing.T) {
	still := func() uint32 { return 7 }

	alerts := atInput(t, 3, still, 0)

	expectQuiet(t, alerts)
}

func TestOnePassingNudgeIsNotSomebodyAtTheMachine(t *testing.T) {
	// The threshold is what tells a person working from a mouse knocked by a
	// closing door.
	var reads int
	var mu sync.Mutex
	once := func() uint32 {
		mu.Lock()
		defer mu.Unlock()
		reads++
		if reads == 2 {
			return 99 // one poll where the clock moved
		}
		return 7
	}

	alerts := atInput(t, 3, once, 0)

	expectQuiet(t, alerts)
}

func TestTheHandThatPressedArmIsNotAnIntruder(t *testing.T) {
	// The grace period is the whole reason this sensor does not fire on the
	// person who armed it.
	alerts := atInput(t, 1, ticking(), time.Hour)

	expectQuiet(t, alerts)
}

func TestDisarmingDuringTheGracePeriodStopsTheSensor(t *testing.T) {
	input := NewInputSensorWithThreshold(1)
	input.every = pollFast
	input.grace = time.Hour
	input.read = ticking()

	_, stop, done := watching(t, input)
	stop()

	if err := expectStops(t, done); err != nil {
		t.Errorf("being disarmed was reported as a failure: %v", err)
	}
}

func TestAThresholdBelowOneIsStillAThreshold(t *testing.T) {
	// Asked for none, the sensor would alert on the first reading of a machine
	// nobody had touched.
	if got := NewInputSensorWithThreshold(0).threshold; got != 1 {
		t.Errorf("a threshold of 0 became %d", got)
	}
	if got := NewInputSensorWithThreshold(-5).threshold; got != 1 {
		t.Errorf("a negative threshold became %d", got)
	}
}

// ── what Windows answers with ──────────────────────────────────────────────

func TestAChargerReadingWindowsWillNotAnswerIsNotADisconnection(t *testing.T) {
	// 255 is the documented "unknown". Read as "unplugged" it would be a
	// critical alert raised because an API call came back empty.
	if _, err := onACFrom(255); err == nil {
		t.Error("an unknown ACLineStatus was read as an answer")
	}

	onAC, err := onACFrom(acOnline)
	if err != nil || !onAC {
		t.Errorf("plugged in read as (%v, %v)", onAC, err)
	}
	onAC, err = onACFrom(acOffline)
	if err != nil || onAC {
		t.Errorf("unplugged read as (%v, %v)", onAC, err)
	}
}
