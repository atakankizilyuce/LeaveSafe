//go:build windows

package main

import "testing"

// stubConsole makes the console decisions answerable without a console to ask
// about and without a window to resize, and reports which window was maximized.
func stubConsole(t *testing.T, attached int, hwnd uintptr) *uintptr {
	t.Helper()

	var maximized uintptr
	realCount, realWindow, realMax := consoleProcessCountFn, consoleWindowFn, maximizeWindowFn
	consoleProcessCountFn = func() int { return attached }
	consoleWindowFn = func() uintptr { return hwnd }
	maximizeWindowFn = func(h uintptr) { maximized = h }
	t.Cleanup(func() {
		consoleProcessCountFn, consoleWindowFn, maximizeWindowFn = realCount, realWindow, realMax
	})
	return &maximized
}

// The window Windows opens for a double-clicked executable is this program's to
// resize, and the dashboard does not fit in the size Windows picked.
func TestAConsoleOfItsOwnIsMaximized(t *testing.T) {
	maximized := stubConsole(t, 1, 0x1234)

	maximizeConsole()

	if *maximized != 0x1234 {
		t.Error("a console this program opened was left at whatever size Windows chose")
	}
}

// The one that matters: a terminal the user was already working in is not this
// program's to take over. Running a command must not resize the window it was
// typed into.
func TestSomebodyElsesTerminalIsLeftAlone(t *testing.T) {
	maximized := stubConsole(t, 3, 0x1234)

	maximizeConsole()

	if *maximized != 0 {
		t.Error("the user's own terminal was maximized out from under them")
	}
}

// A process with no console at all cannot own one. Answering yes would send the
// program on to ask Windows to resize a window that does not exist.
func TestNoConsoleIsNotAConsoleOfItsOwn(t *testing.T) {
	stubConsole(t, 0, 0)

	if ownsConsole() {
		t.Error("a process with no console claimed one as its own")
	}
}

// Owning the console is not the same as having a window for it — a console can
// be attached with nothing on screen, and asking to maximize nothing is how a
// null handle reaches Win32.
func TestNothingIsMaximizedWithoutAWindow(t *testing.T) {
	maximized := stubConsole(t, 1, 0)

	maximizeConsole()

	if *maximized != 0 {
		t.Error("a window that does not exist was maximized")
	}
}

// And the real count has to give the safe answer here. This test runs under
// `go test`, which was started from a shell — so the shell is attached to the
// console too and the count is at least two. A runner with no console cannot
// answer and reports zero. Both are "somebody else's", which is the direction
// it is safe to be wrong in.
func TestATestBinaryDoesNotOwnItsConsole(t *testing.T) {
	if ownsConsole() {
		t.Error("a process started from a shell claimed the console as its own")
	}
}
