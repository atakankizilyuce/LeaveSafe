//go:build windows

package main

import (
	"os"

	log "github.com/sirupsen/logrus"
)

// The Win32 calls in win32_windows.go, as variables so a test can answer for a
// console it does not have and a mode it must not change. Nothing in the
// running program reassigns them.
var (
	consoleModeFn    = consoleMode
	setConsoleModeFn = setConsoleMode
)

// The two things a full-screen program has to ask a Windows console for, and
// what each of them costs when it is not asked for.
//
// Neither has an equivalent anywhere else. A Unix terminal acts on escape
// sequences because that is what a terminal is, and nothing it does with the
// mouse stops this program writing to it. On Windows both are settings, both
// default the wrong way for a program like this one, and both were left alone.
const (
	// ENABLE_VIRTUAL_TERMINAL_PROCESSING: whether the console acts on the
	// escapes written to it or prints them.
	//
	// It is off by default on a console conhost opens, which is the console a
	// double-clicked executable gets. Without it the dashboard is not a
	// dashboard — it is a window filling with "<ESC>[2J<ESC>[H<ESC>[1;1H" as
	// fast as the program can write it, over a screen that has just been
	// maximized. Windows Terminal turns it on for its own, which is why this
	// was invisible wherever the program was started by typing its name.
	enableVTProcessing = 0x0004

	// ENABLE_QUICK_EDIT_MODE, and the flag without which a console ignores what
	// it is told about it.
	//
	// Quick edit is on by default, and it means a click in the window starts a
	// selection — and a console with a selection in it holds up every write the
	// program makes until the selection is cleared. So the program stops, and
	// what the user sees is a full-screen window that froze because they
	// clicked on it. Nothing in the program is wrong and nothing it can log
	// gets out, because the log is a write too.
	enableQuickEdit     = 0x0040
	enableExtendedFlags = 0x0080
)

// takeConsole makes the console one the dashboard can be drawn on, and returns
// what puts it back exactly as it was.
//
// A console that refuses either setting is not a reason to stop: what is lost
// is a readable dashboard, or a window that does not freeze when it is clicked,
// and neither is worth refusing to watch the machine over.
//
// out and keys are the two handles this touches, and either may be nil or point
// at something that is not a console at all — a run with its output redirected
// reaches here through the same door.
func takeConsole(out, keys *os.File) func() {
	var undo []func()
	for _, back := range []func(){
		changeConsoleMode(out, enableVTProcessing, 0),
		changeConsoleMode(keys, enableExtendedFlags, enableQuickEdit),
	} {
		if back != nil {
			undo = append(undo, back)
		}
	}
	if len(undo) == 0 {
		return nil
	}

	return func() {
		for _, back := range undo {
			back()
		}
	}
}

// changeConsoleMode sets the flags in set and clears the ones in clear on a
// console handle, and returns what puts the mode back byte for byte.
//
// Nil whenever there is nothing to undo: no handle, no console behind it, the
// mode already as wanted, or a console that would not take the change. The
// caller is not told which — every one of them means "carry on without it".
func changeConsoleMode(f *os.File, set, clear uint32) func() {
	if f == nil {
		return nil
	}

	handle := f.Fd()
	before, ok := consoleModeFn(handle)
	if !ok {
		return nil
	}

	wanted := (before | set) &^ clear
	if wanted == before {
		return nil
	}
	if !setConsoleModeFn(handle, wanted) {
		log.Debugf("The console would not take the mode the dashboard asked for (%#x)", wanted)
		return nil
	}

	return func() { setConsoleModeFn(handle, before) }
}
