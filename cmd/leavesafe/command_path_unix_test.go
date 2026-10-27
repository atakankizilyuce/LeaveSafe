//go:build linux || darwin

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// A same-named executable in a user-controlled PATH must never run when asking
// the operating system about services or hardware. The real queries below are
// read-only; their results may vary with the host and are deliberately ignored.
func assertIgnoresCommandPath(t *testing.T, names []string, query func()) {
	t.Helper()
	dir := t.TempDir()
	marker := filepath.Join(dir, "executed")
	t.Setenv("LEAVESAFE_COMMAND_TEST_MARKER", marker)
	for _, name := range names {
		script := "#!/bin/sh\nprintf hijacked > \"$LEAVESAFE_COMMAND_TEST_MARKER\"\nexit 99\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	query()
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("a PATH executable ran, or its marker could not be checked: %v", err)
	}
}
