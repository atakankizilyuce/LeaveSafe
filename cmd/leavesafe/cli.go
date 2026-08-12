package main

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/leavesafe/leavesafe/internal/config"
)

// versionLine describes this build in one line: the version stamped in at link
// time, the platform, and the Go toolchain. It is what a bug report should
// start with.
func versionLine() string {
	line := fmt.Sprintf("LeaveSafe %s (%s/%s, %s)", version, runtime.GOOS, runtime.GOARCH, runtime.Version())
	if rev := vcsRevision(); rev != "" {
		line += " commit " + rev
	}
	return line
}

// vcsRevision returns the short commit hash the Go toolchain embeds in binaries
// built from a Git checkout, or "" for builds where it is unavailable.
func vcsRevision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, setting := range info.Settings {
		if setting.Key != "vcs.revision" {
			continue
		}
		rev := setting.Value
		if len(rev) > 12 {
			rev = rev[:12]
		}
		return rev
	}
	return ""
}

// printUsage writes the help text where the flag package expects it.
func printUsage() {
	writeUsage(os.Stderr)
}

// writeUsage is the help text itself. The default flag package output lists
// flags and nothing else, which does not tell someone who just double-clicked a
// binary what it is or where its files live.
//
// The destination is a parameter so a test can read what it says. A flag that
// exists and is not described here is one nobody finds.
func writeUsage(out io.Writer) {
	fmt.Fprintf(out, `%s

LeaveSafe turns your phone into a remote alarm for your laptop. Run it, scan the
QR code it prints, tap Arm, and walk away.

Usage:
  leavesafe [flags]
  leavesafe <command>

Commands:
  install-service     Start LeaveSafe automatically when you log in
  uninstall-service   Remove the autostart entry
  service-status      Report whether the autostart entry is installed

Flags:
  -dev                Serve the phone UI from web/dist instead of the embedded
                      copy, for development
  -plain              Print log lines instead of drawing the full-screen
                      dashboard, leaving the terminal as you found it. Chosen
                      automatically when output is redirected to a file or pipe
  -headless           Run with no terminal interface at all, for autostart
  -version            Print the version and exit
  -h, -help           Show this help

Environment:
  PORT                Override the listening port from the config

Files:
  %s
    config.json       Settings, also editable from the phone
    events.jsonl      Security event history, size-rotated
    leavesafe.log     Application log, size-rotated
    state.json        Whether the machine was armed when this last stopped
    tls/              Self-signed certificate used for remote access

Report bugs at %s
`, versionLine(), config.ConfigDir(), issuesURL)
}

// runSubcommand dispatches a non-flag command and returns the process exit
// code. Unknown commands are an error rather than being ignored, so a typo does
// not silently start the monitor instead.
func runSubcommand(args []string) int {
	switch args[0] {
	case "install-service":
		return runInstallService()
	case "uninstall-service":
		return runUninstallService()
	case "service-status":
		return runServiceStatus()
	case "help":
		printUsage()
		return 0
	case "version":
		fmt.Println(versionLine())
		return 0
	default:
		fmt.Fprintf(os.Stderr, "Unknown command %q.\n\nRun 'leavesafe -help' for the list of commands.\n", args[0])
		return 2
	}
}

// indent prefixes every line of s with pad, for printing multi-line command
// output under a heading.
func indent(s, pad string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = pad + l
	}
	return strings.Join(lines, "\n")
}
