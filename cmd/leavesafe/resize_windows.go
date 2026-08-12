//go:build windows

package main

import "os"

// notifyResize asks to be told when the terminal window changes size, which
// Windows does not offer.
//
// A console reports its size changing as a record on the input handle rather
// than as a signal, and that handle is the one the console loop reads typed
// commands from — reading it from two places would take keystrokes away from
// the person typing them. So the window is not watched here, and the
// five-second repaint is what notices instead: it checks the layout still
// describes the screen before painting anything, so a resized window is put
// right within five seconds rather than immediately.
func notifyResize(chan os.Signal) {
	// Deliberately empty: there is no signal on Windows to ask for. See above.
}

// stopResizeNotices undoes notifyResize.
func stopResizeNotices(chan os.Signal) {
	// Deliberately empty: nothing was asked for, so there is nothing to undo.
}
