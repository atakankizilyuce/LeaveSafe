//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

var kernel32 = syscall.NewLazyDLL("kernel32.dll")

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

	user32 := syscall.NewLazyDLL("user32.dll")
	getConsoleWindow := kernel32.NewProc("GetConsoleWindow")
	showWindow := user32.NewProc("ShowWindow")

	hwnd, _, _ := getConsoleWindow.Call()
	if hwnd != 0 {
		const swMaximize = 3
		// ShowWindow returns the previous visibility state, not an error.
		_, _, _ = showWindow.Call(hwnd, uintptr(swMaximize))
	}
}

// ownsConsole reports whether this process is the only one attached to the
// console, which is what distinguishes a window Windows opened for it from a
// terminal somebody else was already using.
//
// A double-click gives the executable a console of its own and nothing else is
// attached to it. Run from cmd, PowerShell or Windows Terminal, the shell is
// attached as well, so the count is at least two.
//
// A console that cannot be asked counts as somebody else's. Guessing wrong in
// that direction costs a window that stays small; guessing wrong in the other
// costs the user's screen.
func ownsConsole() bool {
	getConsoleProcessList := kernel32.NewProc("GetConsoleProcessList")

	var pids [8]uint32
	n, _, _ := getConsoleProcessList.Call(
		uintptr(unsafe.Pointer(&pids[0])),
		uintptr(len(pids)),
	)
	return n == 1
}
