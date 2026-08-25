//go:build !windows

package main

import "os"

// takeConsole does nothing away from Windows, and the emptiness is the point.
//
// A Unix terminal acts on the escape sequences the dashboard is drawn with
// because that is what a terminal is, and nothing it does with the mouse holds
// up a write. On Windows both of those are console settings that default the
// wrong way for a full-screen program, so the other half of this pair asks for
// them and hands them back. There is nothing here to ask for.
func takeConsole(_, _ *os.File) func() {
	return nil
}
