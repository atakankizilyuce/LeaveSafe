//go:build darwin

package main

import (
	"encoding/xml"
	"fmt"
	"strings"
	"testing"
)

// A LaunchAgent is XML, and the paths written into it are not this program's to
// choose — the binary sits wherever it was unpacked, in a directory whose name
// someone else may have picked. macOS allows <, > and & in a path, so a raw
// interpolation lets that name close its own <string> and open keys of its own.
// launchd reads this file as root and honors it at every login.
func TestAPathCannotWriteItsOwnLaunchAgentKeys(t *testing.T) {
	const injected = `/Users/Shared/x</string></array>` +
		`<key>EnvironmentVariables</key><dict>` +
		`<key>DYLD_INSERT_LIBRARIES</key><string>/tmp/evil.dylib</string></dict>` +
		`<key>ProgramArguments</key><array><string>/bin/sh`

	plist := fmt.Sprintf(plistTemplate,
		agentLabel, plistString(injected), plistString("/tmp/out"), plistString("/tmp/err"))

	if strings.Contains(plist, "DYLD_INSERT_LIBRARIES</key>") {
		t.Error("a path closed its own element and added a key launchd would act on")
	}
	if strings.Contains(plist, "</string></array>") {
		t.Error("a path escaped the element it was written into")
	}

	// Still well-formed, and the injected text has to survive as the literal
	// path it was — mangling it into something else would be its own bug.
	var parsed struct {
		Strings []string `xml:"dict>array>string"`
	}
	if err := xml.Unmarshal([]byte(plist), &parsed); err != nil {
		t.Fatalf("the escaped plist is not valid XML: %v", err)
	}
	if len(parsed.Strings) == 0 || parsed.Strings[0] != injected {
		t.Errorf("the program path came back as %q, want the literal path", parsed.Strings)
	}
}

// A newline in a path is not a mangled path, it is a second line — the trap the
// systemd side names explicitly. XML carries it as a character reference, so it
// stays one value.
func TestALineBreakInThePathStaysPartOfThePath(t *testing.T) {
	const withBreak = "/Users/Shared/one\ntwo"

	plist := fmt.Sprintf(plistTemplate,
		agentLabel, plistString(withBreak), plistString("/tmp/out"), plistString("/tmp/err"))

	var parsed struct {
		Strings []string `xml:"dict>array>string"`
	}
	if err := xml.Unmarshal([]byte(plist), &parsed); err != nil {
		t.Fatalf("the escaped plist is not valid XML: %v", err)
	}
	if len(parsed.Strings) == 0 || parsed.Strings[0] != withBreak {
		t.Errorf("the program path came back as %q, want %q", parsed.Strings, withBreak)
	}
}

// The ordinary path must come through untouched, or the escaping would be a way
// to break every normal install.
func TestAnOrdinaryPathIsUnchanged(t *testing.T) {
	const ordinary = "/Users/ataka/Applications/leavesafe"
	if got := plistString(ordinary); got != ordinary {
		t.Errorf("plistString(%q) = %q, want it unchanged", ordinary, got)
	}
}
