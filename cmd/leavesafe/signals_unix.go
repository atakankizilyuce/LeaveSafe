//go:build !windows

// The signal calls suspending in suspend_unix.go is built from.
//
// Nothing here decides anything, and there is no branch in it. It is in a file
// of its own, and named in sonar-project.properties as excluded from coverage,
// for a reason particular to what it does: a test that let this run would stop
// the test binary, and nothing in the run would ever continue it. The test
// would not fail — it would hang until something killed it.
//
// What it is called from, and in what order, is in suspend_unix.go and is
// covered to the line.

package main

import (
	"os"
	"os/signal"
	"syscall"
)

// raiseStop suspends this process and puts the handler back afterwards.
//
// Nothing this program can call stops it. The default disposition for SIGTSTP
// is what does that, and while the signal is being delivered to a channel the
// default is not in force. So it is put back, raised, and installed again —
// after the raise, which is where execution continues once the user types fg.
// requestSuspend asks for the same thing Ctrl+Z used to ask for on its own.
//
// In raw mode the terminal no longer turns that keystroke into a signal, so it
// arrives as a byte and is turned into one here. It goes through the signal
// rather than straight to the suspending code because everything that has to
// happen around a suspend — handing the terminal back, taking it again,
// redrawing what was on it — is already hung off that signal.
//
// A failure means the program carries on running rather than stopping, which is
// survivable and has nowhere useful to be reported.
func requestSuspend() {
	_ = syscall.Kill(syscall.Getpid(), syscall.SIGTSTP)
}

func raiseStop(ch chan os.Signal) {
	signal.Reset(syscall.SIGTSTP)
	// A failure here means the process carries on running rather than stopping,
	// which is survivable and has nowhere useful to be reported: the terminal
	// has just been handed back, so there is no dashboard to write it on.
	_ = syscall.Kill(syscall.Getpid(), syscall.SIGTSTP)
	signal.Notify(ch, syscall.SIGTSTP)
}
