//go:build windows

package main

import "syscall"

// maximizeConsole expands the console window to fill the screen.
func maximizeConsole() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	user32 := syscall.NewLazyDLL("user32.dll")

	getConsoleWindow := kernel32.NewProc("GetConsoleWindow")
	showWindow := user32.NewProc("ShowWindow")

	hwnd, _, _ := getConsoleWindow.Call()
	if hwnd != 0 {
		const swMaximize = 3
		showWindow.Call(hwnd, uintptr(swMaximize))
	}
}
