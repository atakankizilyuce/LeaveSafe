package ws

import (
	"fmt"
	"os"
	"testing"
)

// isolatedConfigRoot is the directory every test in this package sees as the
// user's home and application-data directory. Empty only if TestMain failed to
// create one, which it treats as fatal.
var isolatedConfigRoot string

// TestMain points the config directory at a temporary one for the whole
// package, before any test runs.
//
// The hub saves configuration as part of doing its job: handleUpdateConfig,
// handleResetConfig, handleConfigure and upgradePinHash all call config.Save,
// which writes to the real config directory — %APPDATA%\LeaveSafe on Windows,
// ~/.leavesafe elsewhere. That is correct in production and ruinous in a test,
// because the tests here call those handlers directly.
//
// So `go test ./...` on a developer's own machine overwrote that developer's
// LeaveSafe configuration with whatever payload the test happened to send.
// config.Save renames a temporary file over the original, so there was no
// backup and nothing to recover: a disarm PIN hash and a geolocation API key
// live nowhere else, and both would have gone. CI never noticed because a fresh
// runner has no configuration to lose.
//
// It is done here rather than with t.Setenv in each test because the trap is
// not any one test — it is that a future test calling any hub method will fall
// into it without ever knowing config.Save was on the other end.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "leavesafe-ws-config")
	if err != nil {
		fmt.Fprintf(os.Stderr, "isolate the config directory: %v\n", err)
		os.Exit(1)
	}
	isolatedConfigRoot = dir

	// APPDATA is what config.ConfigDir reads on Windows; HOME and USERPROFILE
	// are what os.UserHomeDir reads on the other platforms and on Windows
	// respectively. All three, so the isolation does not depend on which
	// platform the suite happens to be running on.
	for _, name := range []string{"APPDATA", "HOME", "USERPROFILE"} {
		if err := os.Setenv(name, dir); err != nil {
			fmt.Fprintf(os.Stderr, "set %s: %v\n", name, err)
			os.Exit(1)
		}
	}

	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
