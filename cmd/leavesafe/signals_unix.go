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
func raiseStop(ch chan os.Signal) {
	signal.Reset(syscall.SIGTSTP)
	// A failure here means the process carries on running rather than stopping,
	// which is survivable and has nowhere useful to be reported: the terminal
	// has just been handed back, so there is no dashboard to write it on.
	_ = syscall.Kill(syscall.Getpid(), syscall.SIGTSTP)
	signal.Notify(ch, syscall.SIGTSTP)
}
