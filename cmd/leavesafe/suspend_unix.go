//go:build !windows

package main

import (
	"context"
	"io"
	"os"
	"os/signal"
	"syscall"
)

// stopProcess is raiseStop, as a variable so a test can stand in for it: a test
// that let the real one run would stop the test binary, and nothing in the run
// would ever continue it. Nothing in the running program reassigns it.
var stopProcess = raiseStop

// watchSuspend makes Ctrl+Z work again.
//
// The dashboard leaves the terminal on the alternate screen with a scrolling
// region pinned under its header. Suspended in that state the program hands the
// shell back a window it cannot use: the prompt lands inside the dashboard's
// scrolling region, on top of a QR code, and scrolling up shows nothing because
// the scrollback belongs to the screen the dashboard switched away from. Which
// is why the program looked as though it could not be put in the background —
// it could, and what came back was unusable.
//
// So the terminal is given back before the process stops, and taken again when
// it is brought forward. The dashboard is redrawn rather than assumed intact:
// switching to the alternate screen clears it, and whatever the user did at the
// shell in between is on the other one.
func watchSuspend(ctx context.Context, out io.Writer, repaint func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTSTP)
	defer signal.Stop(ch)

	watchSuspendOn(ctx, ch, out, repaint)
}

// watchSuspendOn is the loop, over a channel somebody else registered.
//
// Split out so a test can deliver the signal by writing to the channel. Raising
// a real one would be a test that suspends the test binary.
func watchSuspendOn(ctx context.Context, ch chan os.Signal, out io.Writer, repaint func()) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-ch:
			suspend(ch, out, repaint)
		}
	}
}

// suspend gives the terminal back, stops the process, and takes the terminal
// again when the process is resumed.
//
// Whether it was ever taken is read before it is given back, because a -plain
// run reaches here too and must not come back holding a screen it never had.
func suspend(ch chan os.Signal, out io.Writer, repaint func()) {
	taken := terminalScreen.active()
	terminalScreen.restore()

	stopProcess(ch)

	if !taken {
		return
	}
	terminalScreen.enter(out)
	repaint()
}
