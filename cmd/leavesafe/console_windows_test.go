//go:build windows

package main

import (
	"os"
	"testing"
)

// Ctrl+Z arrives as a keystroke on Windows too, because the keyboard is read
// raw. There is nothing to do with it here, and what matters is that asking is
// harmless: the console loop calls this without knowing which platform it is on.
func TestAskingToSuspendOnWindowsDoesNothing(t *testing.T) {
	requestSuspend()
}

// ---- the console settings a dashboard needs ------------------------------

// stubConsoleMode answers for a console handle without one, and records every
// mode that was set on it, in order.
func stubConsoleMode(t *testing.T, before uint32, present, accepts bool) *[]uint32 {
	t.Helper()

	var set []uint32
	realGet, realSet := consoleModeFn, setConsoleModeFn
	consoleModeFn = func(uintptr) (uint32, bool) { return before, present }
	setConsoleModeFn = func(_ uintptr, mode uint32) bool {
		if !accepts {
			return false
		}
		set = append(set, mode)
		return true
	}
	t.Cleanup(func() { consoleModeFn, setConsoleModeFn = realGet, realSet })
	return &set
}

// Without this the dashboard is not drawn at all — the escapes it is built from
// are printed as text on a console conhost opened, which is the console a
// double-clicked executable gets.
func TestTheConsoleIsAskedToActOnEscapes(t *testing.T) {
	set := stubConsoleMode(t, 0, true, true)

	changeConsoleMode(os.Stdout, enableVTProcessing, 0)

	if len(*set) != 1 || (*set)[0]&enableVTProcessing == 0 {
		t.Errorf("the console was set to %#x, which does not act on escapes", *set)
	}
}

// Quick edit means a click in the window starts a selection, and a console with
// a selection in it holds up every write the program makes. What the user sees
// is a full-screen window that froze because they clicked on it.
func TestClickingTheWindowCanNoLongerHoldTheProgramUp(t *testing.T) {
	set := stubConsoleMode(t, enableQuickEdit, true, true)

	changeConsoleMode(os.Stdin, enableExtendedFlags, enableQuickEdit)

	if len(*set) != 1 {
		t.Fatalf("the console mode was set %d times, want once", len(*set))
	}
	if (*set)[0]&enableQuickEdit != 0 {
		t.Error("quick edit is still on, so a click still stops the program writing")
	}
	// Without the extended flag the console does not read what it was told
	// about quick edit at all.
	if (*set)[0]&enableExtendedFlags == 0 {
		t.Error("the extended flag is off, so the console ignores the rest of it")
	}
}

// The mode is put back byte for byte. A console left with the dashboard's
// settings on it is the same class of bug as a terminal left in raw mode.
func TestTheConsoleModeIsPutBackExactlyAsItWas(t *testing.T) {
	const before = enableQuickEdit | 0x0201
	set := stubConsoleMode(t, before, true, true)

	back := changeConsoleMode(os.Stdin, enableExtendedFlags, enableQuickEdit)
	if back == nil {
		t.Fatal("a console that took the change reported nothing to undo")
	}
	back()

	if len(*set) != 2 || (*set)[1] != before {
		t.Errorf("the console was left on %#x, want %#x", *set, uint32(before))
	}
}

// Redirected to a file or a pipe there is no console behind the handle, and
// nothing to put back either. Answering otherwise would leave an undo that sets
// a mode on something that never had one.
func TestNothingIsChangedOnSomethingThatIsNotAConsole(t *testing.T) {
	set := stubConsoleMode(t, 0, false, true)

	if back := changeConsoleMode(os.Stdout, enableVTProcessing, 0); back != nil {
		t.Error("a handle with no console behind it reported something to undo")
	}
	if len(*set) != 0 {
		t.Errorf("a mode was set on something that is not a console: %#x", *set)
	}
}

// A console already set the way the dashboard wants it is left alone, so that
// what comes back is "nothing to undo" rather than an undo that writes the same
// mode again.
func TestAConsoleAlreadySetTheRightWayIsLeftAlone(t *testing.T) {
	set := stubConsoleMode(t, enableVTProcessing, true, true)

	if back := changeConsoleMode(os.Stdout, enableVTProcessing, 0); back != nil {
		t.Error("a console that needed no change reported something to undo")
	}
	if len(*set) != 0 {
		t.Errorf("a console that needed no change was written to: %#x", *set)
	}
}

// A console that will not take the setting is not a reason to stop. What is
// lost is a readable dashboard, and refusing to watch the machine over it would
// be the worse answer.
func TestAConsoleThatRefusesIsNotAFailure(t *testing.T) {
	stubConsoleMode(t, 0, true, false)

	if back := changeConsoleMode(os.Stdout, enableVTProcessing, 0); back != nil {
		t.Error("a console that refused the change reported something to undo")
	}
}

// A nil handle is the run with no terminal to take: headless, or input that is
// not a keyboard. There is nothing there to ask.
func TestTakingNoConsoleAsksForNothing(t *testing.T) {
	set := stubConsoleMode(t, 0, true, true)

	if back := takeConsole(nil, nil); back != nil {
		t.Error("a run with no console handles reported something to undo")
	}
	if len(*set) != 0 {
		t.Errorf("a mode was set with no handle to set it on: %#x", *set)
	}
}
