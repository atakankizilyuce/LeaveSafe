//go:build !windows

package main

import (
	"context"
	"io"
	"os"
	"os/signal"
	"syscall"
)

// watchSuspend makes Ctrl+Z work again.
//
// The dashboard leaves the terminal on the alternate screen with a scrolling
// region pinned under its header. Suspended in that state the program hands the
// shell back a window it cannot use: the prompt lands inside the dashboard's
// scrolling region, on top of a QR code, and scrolling up shows nothing because
// the scrollback belongs to the screen the dashboard switched away from. Which
// is why the program looked like it could not be put in the background — it
// could, and what came back was unusable.
//
// So the terminal is given back before the process stops, and taken again when
// it is brought forward. The dashboard is redrawn rather than assumed intact:
// switching to the alternate screen clears it, and whatever the user did at the
// shell in between is on the other one.
func watchSuspend(ctx context.Context, out io.Writer, repaint func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTSTP)
	defer signal.Stop(ch)

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
// Stopping is done by handing the signal back to the kernel and raising it a
// second time. Nothing this program can call stops it — the default disposition
// for SIGTSTP is what does that, and while the signal is being delivered to a
// channel the default is not in force. So it is put back, raised, and installed
// again on the far side, where execution continues once the user types fg.
func suspend(ch chan os.Signal, out io.Writer, repaint func()) {
	taken := terminalScreen.active()
	terminalScreen.restore()

	signal.Reset(syscall.SIGTSTP)
	// A failure here means the process carries on running rather than stopping,
	// which is survivable and has nowhere useful to be reported: the terminal
	// has just been handed back, so there is no dashboard to write it on.
	_ = syscall.Kill(syscall.Getpid(), syscall.SIGTSTP)

	signal.Notify(ch, syscall.SIGTSTP)
	if !taken {
		return
	}
	terminalScreen.enter(out)
	repaint()
}
