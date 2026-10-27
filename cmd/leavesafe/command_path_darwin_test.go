//go:build darwin

package main

import "testing"

func TestLaunchctlIgnoresUserControlledPATH(t *testing.T) {
	assertIgnoresCommandPath(t, []string{"launchctl"}, func() {
		_, _ = runLaunchctl("help")
	})
}
