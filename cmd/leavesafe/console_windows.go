//go:build windows

package main

// The Win32 calls in win32_windows.go, as variables so a test can answer for a
// console it does not have and a window it must not resize. Nothing in the
// running program reassigns them.
var (
	consoleProcessCountFn = consoleProcessCount
	consoleWindowFn       = consoleWindow
	maximizeWindowFn      = maximizeWindow
)

// maximizeConsole expands the console window to fill the screen, but only when
// the window belongs to this program.
//
// Started by double-clicking the executable, Windows opens a console at
// whatever size it felt like and the dashboard does not fit in it. Started from
// a terminal the user already has open, this used to reach out and maximize
// *their* window — a program taking over the whole screen because it was run,
// which is not a thing a command is allowed to do to the terminal it was typed
// into.
func maximizeConsole() {
	if !ownsConsole() {
		return
	}
	showConsoleMaximized()
}

// ownsConsole reports whether this process is the only one attached to the
// console, which is what distinguishes a window Windows opened for it from a
// terminal somebody else was already using.
//
// A double-click gives the executable a console of its own and nothing else is
// attached to it. Run from cmd, PowerShell or Windows Terminal, the shell is
// attached as well, so the count is at least two.
//
// A console that cannot be asked answers zero, which counts as somebody else's.
// Guessing wrong in that direction costs a window that stays small; guessing
// wrong in the other costs the user's screen.
func ownsConsole() bool {
	return consoleProcessCountFn() == 1
}

// showConsoleMaximized maximizes this process's console window, if it has one.
func showConsoleMaximized() {
	hwnd := consoleWindowFn()
	if hwnd == 0 {
		return
	}
	maximizeWindowFn(hwnd)
}
