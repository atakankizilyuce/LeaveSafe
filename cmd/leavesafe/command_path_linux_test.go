//go:build linux

package main

import "testing"

func TestSystemctlIgnoresUserControlledPATH(t *testing.T) {
	assertIgnoresCommandPath(t, []string{"systemctl"}, func() {
		haveSystemctl()
		_, _ = runSystemctl("--version")
	})
}
