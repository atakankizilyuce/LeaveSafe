package main

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/leavesafe/leavesafe/internal/syspath"
)

// taskName is the Scheduled Task LeaveSafe registers.
//
// A Scheduled Task rather than a Windows service: a service runs in session 0
// with no access to the desktop, and LeaveSafe needs the interactive session to
// read the lock screen and input state — and to sound the alarm on the device
// the user can hear. A logon-triggered task runs in exactly the right place and
// needs no administrator rights to install.
const taskName = "LeaveSafe"

func installAutostart(exe string) (string, error) {
	// /rl LIMITED keeps it at the user's normal privilege level; nothing here
	// wants administrator. /f replaces an existing task so reinstalling works.
	out, err := runSchtasks(
		"/create",
		"/tn", taskName,
		"/tr", `"`+exe+`" -headless`,
		"/sc", "onlogon",
		"/rl", "LIMITED",
		"/f",
	)
	if err != nil {
		return "", fmt.Errorf("schtasks /create: %w: %s", err, out)
	}

	detail := "Scheduled Task: " + taskName
	detail += "\nTrigger:        at logon"
	detail += "\nCommand:        " + exe + " -headless"
	detail += "\nLogs:           " + LogFilePath()
	detail += "\n\nInspect it in Task Scheduler, or with:\n  schtasks /query /tn " + taskName
	return detail, nil
}

func uninstallAutostart() error {
	out, err := runSchtasks("/delete", "/tn", taskName, "/f")
	if err != nil {
		if taskMissing(out) {
			return nil
		}
		return fmt.Errorf("schtasks /delete: %w: %s", err, out)
	}
	return nil
}

func autostartStatus() (bool, string, error) {
	out, err := runSchtasks("/query", "/tn", taskName, "/fo", "list")
	if err != nil {
		if taskMissing(out) {
			return false, "", nil
		}
		return false, "", fmt.Errorf("schtasks /query: %w: %s", err, out)
	}

	detail := "Scheduled Task: " + taskName
	if status := fieldValue(out, "Status:"); status != "" {
		detail += "\nStatus:         " + status
	}
	if next := fieldValue(out, "Next Run Time:"); next != "" {
		detail += "\nNext run:       " + next
	}
	detail += "\nLogs:           " + LogFilePath()
	return true, detail, nil
}

func runSchtasks(args ...string) (string, error) {
	// Absolute path, not a PATH lookup — see internal/syspath. This is the call
	// that most needs it: install-service may be run from an elevated prompt,
	// so a hijacked schtasks would run as administrator.
	//
	// #nosec G204 -- arguments are literals and this program's own path, never user input
	cmd := exec.Command(syspath.System32("schtasks.exe"), args...)
	// And no window for it. See syspath.HideWindow.
	syspath.HideWindow(cmd)

	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// taskMissing reports whether schtasks failed because the task does not exist,
// which is a state to report rather than an error. The message is localized, so
// this matches on the error code Windows includes alongside it.
func taskMissing(out string) bool {
	lower := strings.ToLower(out)
	return strings.Contains(lower, "cannot find the file specified") ||
		strings.Contains(lower, "does not exist") ||
		strings.Contains(out, "ERROR: The system cannot find the file specified.")
}

// fieldValue pulls one "Key: value" line out of schtasks list output.
func fieldValue(out, key string) string {
	for _, line := range strings.Split(out, "\n") {
		if idx := strings.Index(line, key); idx >= 0 {
			return strings.TrimSpace(line[idx+len(key):])
		}
	}
	return ""
}
