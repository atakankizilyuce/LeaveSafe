package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/leavesafe/leavesafe/internal/auth"
	"github.com/leavesafe/leavesafe/internal/config"
	"github.com/leavesafe/leavesafe/internal/localization"
)

// runInstallService registers this binary to start when the user logs in.
//
// A theft monitor that does not survive a reboot is a monitor with a hole in
// it: the machine restarts — flat battery, a Windows update, someone holding
// the power button — and comes back watching nothing, with no indication that
// anything changed. The autostart entry closes that.
func runInstallService() int {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, localization.Text(localization.SystemLanguage(), "Could not determine this program's path: %v\n"), err)
		return 1
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		fmt.Fprintf(os.Stderr, localization.Text(localization.SystemLanguage(), "Could not resolve this program's path: %v\n"), err)
		return 1
	}

	// The background copy has no terminal to print a QR code to, so the key it
	// generates would be unreadable — and a phone paired before the reboot
	// would be locked out by a key it never saw. Writing the key now makes the
	// pairing survive the restart the service exists to cover.
	keyPath := filepath.Join(config.ConfigDir(), auth.KeyFileName)
	// The salt is read with the key and written with it; nothing here shows it,
	// because it is not a secret and nobody types it. See auth.LoadOrCreateKeyFile.
	key, _, err := auth.LoadOrCreateKeyFile(keyPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, localization.Text(localization.SystemLanguage(), "Could not prepare the pairing key: %v\n"), err)
		return 1
	}

	detail, err := installAutostart(exe)
	if err != nil {
		fmt.Fprintf(os.Stderr, localization.Text(localization.SystemLanguage(), "Could not install the autostart entry: %v\n"), err)
		return 1
	}

	fmt.Printf(localization.Text(localization.SystemLanguage(), "LeaveSafe will now start when you log in.\n\n%s\n\n"), indent(detail, "  "))
	fmt.Printf(localization.Text(localization.SystemLanguage(), "Pairing key: %s\n"), formatKey(key))
	fmt.Printf(localization.Text(localization.SystemLanguage(), "  Stored in %s, readable only by you.\n"), keyPath)
	fmt.Print(localization.Text(localization.SystemLanguage(), "  The background copy has no screen to show a QR code on, so it reuses this\n"))
	fmt.Print(localization.Text(localization.SystemLanguage(), "  key every start — which is what lets your phone reconnect after a reboot.\n"))
	fmt.Print(localization.Text(localization.SystemLanguage(), "  Delete the file to force a new key on the next start.\n\n"))
	fmt.Print(localization.Text(localization.SystemLanguage(), "Run 'leavesafe service-status' to check on it, or 'leavesafe uninstall-service' to remove it.\n"))
	return 0
}

// runUninstallService removes the autostart entry.
func runUninstallService() int {
	if err := uninstallAutostart(); err != nil {
		fmt.Fprintf(os.Stderr, localization.Text(localization.SystemLanguage(), "Could not remove the autostart entry: %v\n"), err)
		return 1
	}
	fmt.Println(serviceText("LeaveSafe will no longer start automatically."))
	fmt.Printf(localization.Text(localization.SystemLanguage(), "The stored pairing key was left in place at %s.\n"),
		filepath.Join(config.ConfigDir(), auth.KeyFileName))
	fmt.Println(serviceText("Delete that file if you want a fresh key on the next start."))
	return 0
}

// runServiceStatus reports whether the autostart entry is installed.
func runServiceStatus() int {
	installed, detail, err := autostartStatus()
	if err != nil {
		fmt.Fprintf(os.Stderr, localization.Text(localization.SystemLanguage(), "Could not read the autostart entry: %v\n"), err)
		return 1
	}
	if !installed {
		fmt.Println(serviceText("LeaveSafe is not set to start automatically."))
		fmt.Println(serviceText("Run 'leavesafe install-service' to change that."))
		return 1
	}
	fmt.Printf(localization.Text(localization.SystemLanguage(), "LeaveSafe starts automatically.\n\n%s\n"), indent(detail, "  "))
	return 0
}

// formatKey renders a raw 16-digit key in the grouped form the UI shows.
func formatKey(raw string) string {
	if len(raw) != 16 {
		return raw
	}
	return fmt.Sprintf("%s-%s-%s-%s", raw[0:4], raw[4:8], raw[8:12], raw[12:16])
}

// serviceText localizes user-facing service descriptions, leaving OS command output opaque.
func serviceText(source string, args ...any) string {
	return fmt.Sprintf(localization.Text(localization.SystemLanguage(), source), args...)
}
