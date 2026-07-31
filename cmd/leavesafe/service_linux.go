package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// unitName is the systemd user unit LeaveSafe installs. Changing it would
// orphan units installed by older versions.
const unitName = "leavesafe.service"

// unitTemplate is the systemd user unit. A *user* unit rather than a system
// one, deliberately: LeaveSafe watches one person's laptop, needs that person's
// session to read the screen-lock and input state, and must not run as root to
// do any of it.
const unitTemplate = `[Unit]
Description=LeaveSafe device security monitor
Documentation=%s
After=graphical-session.target

[Service]
Type=simple
ExecStart=%s -headless
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
`

// systemdExecPath renders an executable path for ExecStart.
//
// Three things had to be handled and none of them were. systemd splits the line
// on whitespace, so an unquoted /home/me/my apps/leavesafe becomes the command
// /home/me/my with an argument after it — pointing the autostart at a path any
// local user can then create and fill. It expands %-specifiers, so a directory
// named %h silently became the home directory. And a filename may contain a
// newline, which is not a mangled path but a second directive: everything after
// it is read as another line of the unit, and ExecStartPre runs before the
// program does.
//
// Quoting fixes the first, doubling fixes the second, and the third is refused
// rather than escaped — a unit file has no way to carry a literal newline in a
// path, and this is an install-time error with a person watching it.
func systemdExecPath(exe string) (string, error) {
	if strings.ContainsAny(exe, "\n\r") {
		return "", fmt.Errorf("refusing to install: the path to this program contains a line break (%q), "+
			"which cannot be written into a systemd unit safely — move the binary somewhere without one", exe)
	}
	// Inside double quotes systemd processes C-style escapes, so a literal
	// backslash or quote has to be doubled. %% is how a literal % survives
	// specifier expansion, which happens separately.
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `%`, `%%`).Replace(exe)
	return `"` + escaped + `"`, nil
}

func unitPath() (string, error) {
	// $XDG_CONFIG_HOME wins where it is set, which is what a user who has moved
	// their config directory expects.
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("locate home directory: %w", err)
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "systemd", "user", unitName), nil
}

func installAutostart(exe string) (string, error) {
	path, err := unitPath()
	if err != nil {
		return "", err
	}
	// 0700: this lives under the user's own config directory and only their
	// systemd instance reads it.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("create unit directory: %w", err)
	}
	quoted, err := systemdExecPath(exe)
	if err != nil {
		return "", err
	}
	unit := fmt.Sprintf(unitTemplate, repoURL, quoted)
	if err := os.WriteFile(path, []byte(unit), 0o600); err != nil {
		return "", fmt.Errorf("write unit: %w", err)
	}

	detail := "Unit file: " + path
	// Plenty of systems run something other than systemd. The unit is written
	// either way so it is ready if systemd appears, and the user is told
	// plainly that nothing was enabled — which is a state to report, not an
	// error to fail the install over.
	if !haveSystemctl() {
		return detail + "\n\nsystemctl was not found, so nothing was enabled. Start LeaveSafe\n" +
			"however your desktop session starts background programs.", nil
	}

	if out, err := runSystemctl("daemon-reload"); err != nil {
		return "", fmt.Errorf("systemctl daemon-reload: %w: %s", err, out)
	}
	if out, err := runSystemctl("enable", "--now", unitName); err != nil {
		return "", fmt.Errorf("systemctl enable: %w: %s", err, out)
	}

	detail += "\nEnabled with: systemctl --user enable --now " + unitName
	detail += "\nLogs:         journalctl --user -u " + unitName + " -f"
	// Without lingering, the user unit is torn down at logout and never starts
	// on an unattended boot — which is exactly the case this is meant to cover.
	detail += "\n\nTo keep it running when you are logged out, and to have it start on boot:\n" +
		"  sudo loginctl enable-linger " + os.Getenv("USER")
	return detail, nil
}

func uninstallAutostart() error {
	path, err := unitPath()
	if err != nil {
		return err
	}
	if haveSystemctl() {
		// Failures here are reported but not fatal: the unit file removal below
		// is what actually stops it coming back.
		if out, err := runSystemctl("disable", "--now", unitName); err != nil {
			fmt.Fprintf(os.Stderr, "warning: systemctl disable: %v: %s\n", err, out)
		}
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove unit: %w", err)
	}
	if haveSystemctl() {
		if out, err := runSystemctl("daemon-reload"); err != nil {
			fmt.Fprintf(os.Stderr, "warning: systemctl daemon-reload: %v: %s\n", err, out)
		}
	}
	return nil
}

func autostartStatus() (bool, string, error) {
	path, err := unitPath()
	if err != nil {
		return false, "", err
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return false, "", nil
	} else if err != nil {
		return false, "", err
	}

	detail := "Unit file: " + path
	// Not an error: the unit is installed either way, and its running state is
	// simply something this system cannot be asked about.
	if !haveSystemctl() {
		return true, detail + "\nsystemctl is not available, so its running state is unknown.", nil
	}

	// systemctl exits non-zero for "inactive", which is a state to report
	// rather than an error to fail on.
	active, _ := runSystemctl("is-active", unitName)
	enabled, _ := runSystemctl("is-enabled", unitName)
	detail += "\nEnabled:   " + orUnknown(enabled)
	detail += "\nRunning:   " + orUnknown(active)
	detail += "\nLogs:      journalctl --user -u " + unitName + " -f"
	return true, detail, nil
}

// haveSystemctl reports whether systemctl is on PATH. Its absence is a fact
// about the system rather than a failure, so it is a boolean here and never an
// error the callers have to decide how to swallow.
func haveSystemctl() bool {
	_, err := exec.LookPath("systemctl")
	return err == nil
}

func runSystemctl(args ...string) (string, error) {
	cmd := exec.Command("systemctl", append([]string{"--user"}, args...)...) // #nosec G204 -- arguments are literals from this file, never user input
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}
