//go:build !windows

package main

import (
	"os"
	"os/signal"
	"syscall"
)

// notifyResize asks to be told when the terminal window changes size.
func notifyResize(ch chan os.Signal) {
	signal.Notify(ch, syscall.SIGWINCH)
}

// stopResizeNotices undoes notifyResize.
func stopResizeNotices(ch chan os.Signal) {
	signal.Stop(ch)
}
