package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// agentLabel is the launchd label. Reverse-DNS by convention, and the file is
// named after it.
const agentLabel = "com.leavesafe.monitor"

// plistTemplate is the LaunchAgent definition.
//
// A LaunchAgent rather than a LaunchDaemon: agents run inside the logged-in
// user's session, which is where the screen-lock and input state LeaveSafe
// reads actually live, and they do not need root.
const plistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
		<string>-headless</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<dict>
		<key>SuccessfulExit</key>
		<false/>
	</dict>
	<key>StandardOutPath</key>
	<string>%s</string>
	<key>StandardErrorPath</key>
	<string>%s</string>
</dict>
</plist>
`

func agentPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home directory: %w", err)
	}
	return filepath.Join(home, "Library", "LaunchAgents", agentLabel+".plist"), nil
}

func installAutostart(exe string) (string, error) {
	path, err := agentPath()
	if err != nil {
		return "", err
	}
	// 0700 on a directory in the user's own Library, and 0600 on the plist
	// below. launchd reads user agents as root, so tightening these costs
	// nothing; it refuses group- or world-writable plists outright, which these
	// are not.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("create LaunchAgents directory: %w", err)
	}

	// launchd needs somewhere to send whatever the process writes to the
	// terminal it does not have.
	stdoutPath := LogFilePath() + ".launchd.out"
	stderrPath := LogFilePath() + ".launchd.err"

	plist := fmt.Sprintf(plistTemplate, agentLabel, exe, stdoutPath, stderrPath)
	if err := os.WriteFile(path, []byte(plist), 0o600); err != nil {
		return "", fmt.Errorf("write LaunchAgent: %w", err)
	}

	target := "gui/" + strconv.Itoa(os.Getuid())
	// bootout first so reinstalling over an existing entry replaces it rather
	// than failing with "service already loaded".
	_, _ = runLaunchctl("bootout", target+"/"+agentLabel)
	if out, err := runLaunchctl("bootstrap", target, path); err != nil {
		return "", fmt.Errorf("launchctl bootstrap: %w: %s", err, out)
	}
	if out, err := runLaunchctl("enable", target+"/"+agentLabel); err != nil {
		return "", fmt.Errorf("launchctl enable: %w: %s", err, out)
	}

	detail := "LaunchAgent: " + path
	detail += "\nLoaded with: launchctl bootstrap " + target
	detail += "\nLogs:        " + LogFilePath()
	detail += "\n\nmacOS will ask for permission the first time the background copy reads input\n" +
		"state. Grant it under System Settings → Privacy & Security → Accessibility,\n" +
		"otherwise the input sensor stays unavailable."
	return detail, nil
}

func uninstallAutostart() error {
	path, err := agentPath()
	if err != nil {
		return err
	}
	target := "gui/" + strconv.Itoa(os.Getuid()) + "/" + agentLabel
	if out, err := runLaunchctl("bootout", target); err != nil {
		// Not loaded is the normal case when the plist was removed by hand.
		fmt.Fprintf(os.Stderr, "warning: launchctl bootout: %v: %s\n", err, out)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove LaunchAgent: %w", err)
	}
	return nil
}

func autostartStatus() (bool, string, error) {
	path, err := agentPath()
	if err != nil {
		return false, "", err
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return false, "", nil
	} else if err != nil {
		return false, "", err
	}

	detail := "LaunchAgent: " + path
	target := "gui/" + strconv.Itoa(os.Getuid()) + "/" + agentLabel
	if out, err := runLaunchctl("print", target); err == nil {
		detail += "\nLoaded:      yes"
		if state := firstFieldAfter(out, "state = "); state != "" {
			detail += "\nState:       " + state
		}
	} else {
		detail += "\nLoaded:      no (the plist exists but launchd has not loaded it)"
	}
	detail += "\nLogs:        " + LogFilePath()
	return true, detail, nil
}

func runLaunchctl(args ...string) (string, error) {
	cmd := exec.Command("launchctl", args...) // #nosec G204 -- arguments are literals and paths this program derived, never user input
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// firstFieldAfter returns the rest of the first line containing prefix, with
// the prefix removed. launchctl print emits an indented key = value dump; this
// pulls one value out of it without pretending to parse the whole thing.
func firstFieldAfter(s, prefix string) string {
	for _, line := range strings.Split(s, "\n") {
		if idx := strings.Index(line, prefix); idx >= 0 {
			return strings.TrimSpace(line[idx+len(prefix):])
		}
	}
	return ""
}
