//go:build !windows

package main

import "os"

// maximizeConsole does nothing away from Windows, and the emptiness is the
// point rather than an omission.
func maximizeConsole() {
	// On Windows the program is usually started by double-clicking it, which
	// opens a console window at whatever size Windows felt like, so the other
	// half of this pair asks for it to be maximized and the log becomes
	// readable. Everywhere else the program is started from a terminal the user
	// already opened and already sized. Resizing that terminal for them would be
	// taking something over, not helping.
}

// takeConsole does nothing away from Windows, and the emptiness is the point
// here too.
//
// A Unix terminal acts on the escape sequences the dashboard is drawn with
// because that is what a terminal is, and nothing it does with the mouse holds
// up a write. On Windows both of those are console settings that default the
// wrong way for a full-screen program, so the other half of this pair asks for
// them and hands them back. There is nothing here to ask for.
func takeConsole(_, _ *os.File) func() {
	return nil
}
