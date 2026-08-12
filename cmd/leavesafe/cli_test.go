package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A flag that exists and is not in the help text is one nobody finds.
func TestEveryFlagThatChangesTheRunIsDescribed(t *testing.T) {
	var help strings.Builder

	writeUsage(&help)

	text := help.String()
	for _, name := range []string{"-dev", "-plain", "-headless", "-version"} {
		if !strings.Contains(text, name) {
			t.Errorf("%s is a flag nobody reading the help text would find", name)
		}
	}
}

// The help text is what somebody who double-clicked a binary reads first, so it
// has to say what the program is and where its files are, not just list flags.
func TestTheHelpTextSaysWhatTheProgramIsAndWhereItKeepsThings(t *testing.T) {
	var help strings.Builder

	writeUsage(&help)

	text := help.String()
	for _, part := range []string{"LeaveSafe", "Usage:", "Commands:", "Files:", "config.json"} {
		if !strings.Contains(text, part) {
			t.Errorf("the help text does not mention %q", part)
		}
	}
}

// The flag package calls printUsage, and what it prints has to be the help text
// above rather than the package's own bare list of switches.
func TestPrintUsageWritesTheHelpTextToStandardError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stderr")
	captured, err := os.Create(path)
	if err != nil {
		t.Fatalf("create the capture file: %v", err)
	}
	real := os.Stderr
	os.Stderr = captured
	t.Cleanup(func() {
		os.Stderr = real
		_ = captured.Close()
	})

	printUsage()

	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the capture back: %v", err)
	}
	if !strings.Contains(string(written), "LeaveSafe turns your phone") {
		t.Errorf("printUsage wrote something other than the help text:\n%s", string(written))
	}
}

// A bug report should start with this line, and it has to name the build well
// enough to tell two of them apart.
func TestTheVersionLineNamesTheBuild(t *testing.T) {
	line := versionLine()

	for _, part := range []string{"LeaveSafe", version} {
		if !strings.Contains(line, part) {
			t.Errorf("versionLine = %q, want it to contain %q", line, part)
		}
	}
	// Built from a checkout the toolchain stamps the commit in; built any other
	// way it cannot, and the line is expected to be useful either way.
	if rev := vcsRevision(); rev != "" && !strings.Contains(line, rev) {
		t.Errorf("versionLine = %q, want it to carry the commit %q", line, rev)
	}
}
