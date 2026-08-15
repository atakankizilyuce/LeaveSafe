package monitor

import "testing"

// What the charger, the lid and the screen have in common: each is read on a
// timer, and each has to tell a change apart from a repetition and from the
// first look of all. Getting that wrong in one direction is an alarm that never
// fires; in the other it is an alarm every two seconds, or — worst of the three
// — an alarm the moment somebody arms a laptop that was already shut.

const (
	open   = true
	closed = false
)

func TestTheFirstReadingIsWhereItStartsFromRatherThanAnEvent(t *testing.T) {
	// Nothing has happened yet. Something *is* the case, and knowing it is what
	// makes the next reading meaningful.
	var w stateWatch[bool]

	if w.sample(closed) {
		t.Error("the first look at the hardware was reported as a change")
	}
	if w.sample(closed) {
		t.Error("a second reading of the same state was reported as a change")
	}
}

func TestAChangeIsReportedOnce(t *testing.T) {
	var w stateWatch[bool]
	w.sample(open)

	if !w.sample(closed) {
		t.Fatal("the lid closing was not reported")
	}
	for i := range 5 {
		if w.sample(closed) {
			t.Fatalf("reading %d of a lid that was still shut reported it again", i+1)
		}
	}
}

func TestItReportsTheWayBackAsWell(t *testing.T) {
	// Opening again is its own event. A sensor that only reported one direction
	// would leave the panel showing a laptop shut hours after it was opened.
	var w stateWatch[bool]
	w.sample(open)
	w.sample(closed)

	if !w.sample(open) {
		t.Error("the lid opening again was not reported")
	}
}

func TestEveryFlipIsItsOwnEvent(t *testing.T) {
	var w stateWatch[bool]
	w.sample(open)

	for i := range 4 {
		if !w.sample(closed) {
			t.Fatalf("close %d was not reported", i+1)
		}
		if !w.sample(open) {
			t.Fatalf("open %d was not reported", i+1)
		}
	}
}

// The charger reports three states rather than two — plugged, unplugged, and a
// Windows API that would not say — so the watch is not a boolean with a nicer
// name.
func TestItWatchesWhateverTheHardwareAnswersWith(t *testing.T) {
	const (
		unplugged byte = 0
		plugged   byte = 1
		unknown   byte = 255
	)

	var w stateWatch[byte]
	w.sample(plugged)

	if !w.sample(unplugged) {
		t.Error("the charger coming out was not reported")
	}
	if !w.sample(unknown) {
		t.Error("the reading going unknown was not reported as a change")
	}
	if w.sample(unknown) {
		t.Error("a second unknown reading was reported again")
	}
}

func TestAWatchThatWasNotRunningDoesNotReportWhatItMissed(t *testing.T) {
	// The supervisor restarts a sensor whose driver failed. While it was down
	// the machine was not being watched, and the lid may well have moved. That
	// is a gap in the watching, not an intrusion, and reporting it as one would
	// put "Lid closed!" on somebody's phone for a lid that closed while the
	// sensor was already dead.
	var w stateWatch[bool]
	w.sample(open)

	w.forget()

	if w.sample(closed) {
		t.Error("the first reading after a restart was reported as a change")
	}
	if !w.sample(open) {
		t.Error("the watch did not pick up again from where it restarted")
	}
}
