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
			for _, lang := range []language{turkish, english} {
				got := askConnectionMode(strings.NewReader(tc.typed), io.Discard, lang, tc.current)
				if got != tc.want {
					t.Errorf("askConnectionMode(%q, %s, current=%v) = %v, want %v",
						tc.typed, lang.code, tc.current, got, tc.want)
				}
			}
		})
	}
}

// The saved choice has to be visible in the prompt, or "press Enter to keep it"
// is asking the user to remember what they picked last time.
func TestThePromptShowsWhichModeIsCurrent(t *testing.T) {
	var out strings.Builder
	askConnectionMode(strings.NewReader("\n"), &out, turkish, true)

	printed := out.String()
	if !strings.Contains(printed, "[2]:") {
		t.Errorf("prompt does not show 2 as the default:\n%s", printed)
	}
	if !strings.Contains(printed, "Mobil veri") {
		t.Errorf("prompt is not in Turkish:\n%s", printed)
	}
	if !strings.Contains(printed, turkish.currentMark) {
		t.Errorf("prompt does not mark the current mode:\n%s", printed)
	}
}

// And with remote access off, 1 is the default rather than 2.
func TestThePromptDefaultsToWiFiWhenThatIsWhatIsStored(t *testing.T) {
	var out strings.Builder
	askConnectionMode(strings.NewReader("\n"), &out, turkish, false)

	if printed := out.String(); !strings.Contains(printed, "[1]:") {
		t.Errorf("prompt does not show 1 as the default:\n%s", printed)
	}
}

// The point of asking for a language is that the answer is honored. A prompt
// that leaks the other language is the thing this replaced.
func TestEachLanguageAsksInItsOwnWords(t *testing.T) {
	var tr strings.Builder
	askConnectionMode(strings.NewReader("\n"), &tr, turkish, false)
	if got := tr.String(); !strings.Contains(got, "Mobil veri") || strings.Contains(got, "Mobile data") {
		t.Errorf("the Turkish prompt is not purely Turkish:\n%s", got)
	}

	var en strings.Builder
	askConnectionMode(strings.NewReader("\n"), &en, english, false)
	if got := en.String(); !strings.Contains(got, "Mobile data") || strings.Contains(got, "Mobil veri") {
		t.Errorf("the English prompt is not purely English:\n%s", got)
	}
}

// The language question is the one that cannot be asked in its own answer, so
// it names both languages — and its default has to be the number it prints.
func TestTheLanguageQuestionOffersBothAndDefaultsToTheNumberItShows(t *testing.T) {
	cases := map[string]struct {
		typed string
		want  string
	}{
		"1 is turkish":         {"1\n", "tr"},
		"2 is english":         {"2\n", "en"},
		"enter takes the [1]":  {"\n", "tr"},
		"nonsense takes the 1": {"banana\n", "tr"},
		// No terminal to answer means English, which is the language the rest
		// of the program is in.
		"eof falls back to english": {"", "en"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var out strings.Builder
			got := askLanguage(strings.NewReader(tc.typed), &out)
			if got != tc.want {
				t.Errorf("askLanguage(%q) = %q, want %q", tc.typed, got, tc.want)
			}
			printed := out.String()
			for _, want := range []string{"Türkçe", "English", "[1]:"} {
				if !strings.Contains(printed, want) {
					t.Errorf("the language question does not show %q:\n%s", want, printed)
				}
			}
		})
	}
}

// Every row of a box has to end in the same column. Turkish is exactly where
// this breaks if it is measured wrong: "ğ" is two bytes and one column, so a
// byte count would pull the right-hand border in by one for every letter with
// a tail on it — on the language the option exists for.
func TestTheBoxKeepsItsShapeInBothLanguages(t *testing.T) {
	for _, lang := range []language{turkish, english} {
		var out strings.Builder
		askConnectionMode(strings.NewReader("\n"), &out, lang, true)

		for _, line := range strings.Split(out.String(), "\n") {
			if !strings.HasPrefix(line, "  │") && !strings.HasPrefix(line, "  ┌") &&
				!strings.HasPrefix(line, "  ├") && !strings.HasPrefix(line, "  └") {
				continue
			}
			if got := visLen(line); got != promptWidth+2 {
				t.Errorf("%s: box row is %d columns wide, want %d:\n%q",
					lang.code, got, promptWidth+2, line)
			}
		}
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

// `lang` has the same duty: someone who types it to see what it says must not
// change their language by reading the answer.
func TestParseLanguageChoiceOnlyAcceptsOneOrTwo(t *testing.T) {
	cases := map[string]struct {
		typed  string
		want   string
		wantOK bool
	}{
		"1 is turkish":         {"1", "tr", true},
		"2 is english":         {"2", "en", true},
		"padded still counts":  {" 1 ", "tr", true},
		"enter leaves alone":   {"", "", false},
		"nonsense left alone":  {"deutsch", "", false},
		"a third option is no": {"3", "", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, ok := parseLanguageChoice(tc.typed)
			if ok != tc.wantOK {
				t.Fatalf("parseLanguageChoice(%q) ok = %v, want %v", tc.typed, ok, tc.wantOK)
			}
			if got != tc.want {
				t.Errorf("parseLanguageChoice(%q) = %q, want %q", tc.typed, got, tc.want)
			}
		})
	}
}

// A stored code decides the wording, and anything unrecognized has to land on
// English rather than on an empty struct that prints blank options.
func TestLanguageByCodeFallsBackToEnglish(t *testing.T) {
	if got := languageByCode("tr"); got.code != "tr" {
		t.Errorf("languageByCode(\"tr\") = %q, want tr", got.code)
	}
	for _, code := range []string{"en", "", "de", "TR"} {
		if got := languageByCode(code); got.code != "en" {
			t.Errorf("languageByCode(%q) = %q, want en", code, got.code)
		}
	}
}
