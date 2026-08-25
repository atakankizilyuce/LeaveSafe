//go:build windows

// The Win32 calls the console decisions in console_windows.go are made from.
//
// Nothing here decides anything: each function is one call into a DLL and a
// value handed back, with no branch in any of them. They are in a file of their
// own, and named in sonar-project.properties as excluded from coverage, because
// a test of them could assert nothing beyond "it did not throw" — and the one
// that maximizes a window would maximize the window of whoever ran the tests,
// which is the very thing the change they serve was made to stop.
//
// The decisions they feed are in console_windows.go, which stands on these
// three functions precisely so that it can be covered to the line.

package main

import (
	"syscall"
	"unsafe"
)

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	user32   = syscall.NewLazyDLL("user32.dll")
)

// consoleProcessCount is how many processes are attached to this console, or 0
// when there is no console to ask.
func consoleProcessCount() int {
	proc := kernel32.NewProc("GetConsoleProcessList")

	var pids [8]uint32
	n, _, _ := proc.Call(uintptr(unsafe.Pointer(&pids[0])), uintptr(len(pids)))
	return int(n)
}

// consoleWindow is the handle of this process's console window, or 0.
func consoleWindow() uintptr {
	hwnd, _, _ := kernel32.NewProc("GetConsoleWindow").Call()
	return hwnd
}

// maximizeWindow asks Windows to maximize a window.
func maximizeWindow(hwnd uintptr) {
	const swMaximize = 3
	// ShowWindow returns the previous visibility state, not an error.
	_, _, _ = user32.NewProc("ShowWindow").Call(hwnd, uintptr(swMaximize))
}

// consoleMode is the mode flags set on a console handle, and false when there
// is no console behind it to ask — a handle redirected to a file or a pipe.
func consoleMode(handle uintptr) (uint32, bool) {
	proc := kernel32.NewProc("GetConsoleMode")

	var mode uint32
	ok, _, _ := proc.Call(handle, uintptr(unsafe.Pointer(&mode)))
	return mode, ok != 0
}

// setConsoleMode puts mode on a console handle and reports whether it took.
func setConsoleMode(handle uintptr, mode uint32) bool {
	proc := kernel32.NewProc("SetConsoleMode")

	ok, _, _ := proc.Call(handle, uintptr(mode))
	return ok != 0
}
