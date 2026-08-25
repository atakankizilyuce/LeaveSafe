//go:build windows

// The Win32 calls the console decisions in console_windows.go are made from.
//
// Nothing here decides anything: each function is one call into a DLL and a
// value handed back, with no branch in any of them. They are in a file of their
// own, and named in sonar-project.properties as excluded from coverage, because
// a test of them could assert nothing beyond "it did not throw" — and one that
// changed the mode of the console would change the console of whoever ran the
// tests.
//
// The decisions they feed are in console_windows.go, which stands on these
// functions precisely so that it can be covered to the line.

package main

import (
	"syscall"
	"unsafe"
)

var kernel32 = syscall.NewLazyDLL("kernel32.dll")

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
