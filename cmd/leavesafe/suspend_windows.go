//go:build windows

package main

import (
	"context"
	"io"
)

// watchSuspend does nothing on Windows, which has no equivalent of Ctrl+Z.
//
// The console window is minimized or moved behind another one instead, and
// nothing about that reaches the process — there is no signal to catch and
// nothing to give back, because the terminal is never taken away.
func watchSuspend(ctx context.Context, _ io.Writer, _ func()) {
	<-ctx.Done()
}

// requestSuspend does nothing on Windows, for the same reason.
//
// Ctrl+Z arrives as a keystroke here rather than as a signal — reading the
// keyboard raw is what makes it visible at all — and there is nothing to do
// with it. On a console it has never meant "put this in the background"; it is
// the end-of-file key, and the console loop already answers Ctrl+D with that.
func requestSuspend() {
	// Deliberately empty: there is nothing on Windows to ask for. See above.
}
