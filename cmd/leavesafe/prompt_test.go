package main

import (
	"io"
	"strings"
	"testing"
)

// The question is asked on every start now, so a user who has already chosen
// must be able to keep their choice by pressing Enter. Anything else turns a
// convenience into a chore, and — worse — a habit of pressing Enter would
// silently reset a setting the phone had turned on.
func TestEnterKeepsTheSavedChoice(t *testing.T) {
	cases := map[string]struct {
		typed   string
		current bool
		want    bool
	}{
		"enter keeps remote on":       {"\n", true, true},
		"enter keeps wifi only":       {"\n", false, false},
		"blank line keeps remote on":  {"   \n", true, true},
		"eof keeps the saved choice":  {"", true, true},
		"1 selects wifi":              {"1\n", true, false},
		"2 selects remote":            {"2\n", false, true},
		"nonsense keeps saved choice": {"banana\n", true, true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := askConnectionMode(strings.NewReader(tc.typed), io.Discard, tc.current)
			if got != tc.want {
				t.Errorf("askConnectionMode(%q, current=%v) = %v, want %v",
					tc.typed, tc.current, got, tc.want)
			}
		})
	}
}

// The saved choice has to be visible in the prompt, or "press Enter to keep it"
// is asking the user to remember what they picked last time.
func TestThePromptShowsWhichModeIsCurrent(t *testing.T) {
	var out strings.Builder
	askConnectionMode(strings.NewReader("\n"), &out, true)

	printed := out.String()
	if !strings.Contains(printed, "[2]:") {
		t.Errorf("prompt does not show 2 as the default:\n%s", printed)
	}
	if !strings.Contains(printed, "Mobil veri") {
		t.Errorf("prompt lost its Turkish text:\n%s", printed)
	}
	if !strings.Contains(printed, "Remote Access") {
		t.Errorf("prompt lost its English text:\n%s", printed)
	}
}

// And with remote access off, 1 is the default rather than 2.
func TestThePromptDefaultsToWiFiWhenThatIsWhatIsStored(t *testing.T) {
	var out strings.Builder
	askConnectionMode(strings.NewReader("\n"), &out, false)

	if printed := out.String(); !strings.Contains(printed, "[1]:") {
		t.Errorf("prompt does not show 1 as the default:\n%s", printed)
	}
}

// The console's `mode` command has to tell "leave it alone" apart from a mode.
// Reading a bare enter as a choice would switch remote access off for anyone
// who typed the command to see what it said.
func TestParseModeChoiceOnlyAcceptsOneOrTwo(t *testing.T) {
	cases := map[string]struct {
		typed    string
		wantMode bool
		wantOK   bool
	}{
		"1 is wifi":            {"1", false, true},
		"2 is remote":          {"2", true, true},
		"padded still counts":  {"  2  ", true, true},
		"enter leaves alone":   {"", false, false},
		"spaces leave alone":   {"   ", false, false},
		"nonsense left alone":  {"banana", false, false},
		"a third option is no": {"3", false, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			want, ok := parseModeChoice(tc.typed)
			if ok != tc.wantOK {
				t.Fatalf("parseModeChoice(%q) ok = %v, want %v", tc.typed, ok, tc.wantOK)
			}
			if ok && want != tc.wantMode {
				t.Errorf("parseModeChoice(%q) = %v, want %v", tc.typed, want, tc.wantMode)
			}
		})
	}
}
