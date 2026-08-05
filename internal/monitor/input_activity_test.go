package monitor

import "testing"

// The input sensor is the only one that has to decide how much is too much.
// Every other sensor answers a hardware event; this one watches a number climb
// and has to say when a person is at the machine rather than when a mouse was
// nudged. Getting it wrong in one direction is an alarm that never fires, and
// in the other an alarm that fires every second while someone works.
//
// The rule used to live inside the ticker loop, where the only way to try it
// was to run the sensor for real against the machine's own idle clock.

// busy is an idle reading that means someone is at the machine; quiet is one
// that means they are not.
const (
	busy  = 0.0
	quiet = 5.0
)

func TestOnePassingTouchIsNotAnAlert(t *testing.T) {
	w := activityWatch{threshold: 3}

	if w.sample(busy) {
		t.Error("a single reading raised the alarm")
	}
	if w.sample(quiet) {
		t.Error("the machine going quiet raised the alarm")
	}
}

func TestSustainedActivityRaisesTheAlarmOnce(t *testing.T) {
	w := activityWatch{threshold: 3}

	// Separately, and never behind a short-circuit: each call is a reading the
	// watch has to see, so one that is skipped is a reading that never happened.
	for i := 1; i < 3; i++ {
		if w.sample(busy) {
			t.Fatalf("the alarm fired on reading %d, before the threshold of 3", i)
		}
	}
	if !w.sample(busy) {
		t.Fatal("the alarm did not fire at the threshold")
	}

	// Someone working at the laptop must produce one alert, not one a second.
	for i := range 10 {
		if w.sample(busy) {
			t.Fatalf("the alarm fired again on reading %d of continued activity", i+1)
		}
	}
}

// A break in the activity does not by itself count as leaving: the threshold
// starts over rather than carrying the earlier readings forward.
func TestAnInterruptedRunStartsCountingAgain(t *testing.T) {
	w := activityWatch{threshold: 3}

	w.sample(busy)
	w.sample(busy)
	w.sample(quiet)

	for i := 1; i < 3; i++ {
		if w.sample(busy) {
			t.Errorf("reading %d after the pause alerted, so the earlier run was still counted", i)
		}
	}
}

// Once it has reported, the sensor stays quiet until the machine has been left
// alone for five seconds — and only then will it report again.
func TestTheAlarmArmsItselfAgainAfterTheMachineIsLeftAlone(t *testing.T) {
	w := activityWatch{threshold: 1}

	if !w.sample(busy) {
		t.Fatal("the first alert did not fire")
	}

	for i := range 4 {
		w.sample(quiet)
		if w.sample(busy) {
			t.Fatalf("the alarm fired again after only %d seconds of quiet", i+1)
		}
	}

	for range 5 {
		w.sample(quiet)
	}
	if !w.sample(busy) {
		t.Error("the alarm never armed itself again after the machine was left alone")
	}
}

// A negative reading means ioreg could not be asked at all. That is not
// evidence of anyone touching the machine, and counting it as activity would
// turn a broken lookup into an alarm on a laptop nobody is near.
func TestAnUnreadableIdleClockIsNotActivity(t *testing.T) {
	w := activityWatch{threshold: 2}

	for range 10 {
		if w.sample(-1) {
			t.Fatal("a failed idle reading was counted as someone using the machine")
		}
	}
}
