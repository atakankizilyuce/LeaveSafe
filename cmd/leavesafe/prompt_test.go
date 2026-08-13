package main

import (
	"strings"
	"testing"
)

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
func TestTheBoxKeepsItsShape(t *testing.T) {
	var out strings.Builder
	askLanguage(strings.NewReader("\n"), &out)

	for _, line := range strings.Split(out.String(), "\n") {
		if !strings.HasPrefix(line, "  │") && !strings.HasPrefix(line, "  ┌") &&
			!strings.HasPrefix(line, "  ├") && !strings.HasPrefix(line, "  └") {
			continue
		}
		if got := visLen(line); got != promptWidth+2 {
			t.Errorf("box row is %d columns wide, want %d:\n%q", got, promptWidth+2, line)
		}
	}
}

// `lang` must tell "leave it alone" apart from a choice: someone who types the
// command to see what it says must not change their language by reading the
// answer.
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
