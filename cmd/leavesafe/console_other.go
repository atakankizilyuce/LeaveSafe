//go:build !windows

package main

// maximizeConsole does nothing away from Windows, and the emptiness is the
// point rather than an omission.
//
// On Windows the program is usually started by double-clicking it, which opens
// a console window at whatever size Windows felt like; the other half of this
// pair asks for it to be maximized so the log is readable. Everywhere else the
// program is started from a terminal the user already opened and already sized.
// Resizing that terminal for them would be taking something over, not helping.
func maximizeConsole() {}
