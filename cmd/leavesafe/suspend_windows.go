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
