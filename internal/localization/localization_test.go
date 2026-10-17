package localization

import (
	"regexp"
	"slices"
	"testing"
)

func TestNormalizeLocales(t *testing.T) {
	cases := map[string]string{"tr_TR.UTF-8": "tr", "en-US": "en", "zh": "zh-Hans", "zh-CN": "zh-Hans", "zh-SG": "zh-Hans", "zh-Hans-CN": "zh-Hans", "pnb": "pnb-Arab", "pa-Arab-PK": "pnb-Arab", "pnb_PK": "pnb-Arab", "yue-HK": "yue-Hant", "fil": "tl", "fil-PH": "tl", "fr_CA": "fr", "zh-Hant": "", "xx": "", "C.UTF-8": ""}
	for input, want := range cases {
		if got := Normalize(input); got != want {
			t.Errorf("Normalize(%q)=%q, want %q", input, got, want)
		}
	}
	for _, language := range Languages {
		if Normalize(language.Code) != language.Code {
			t.Error(language.Code)
		}
	}
}
func TestSystemLocalePrecedence(t *testing.T) {
	for _, key := range []string{"LEAVESAFE_LANGUAGE", "LC_ALL", "LC_MESSAGES", "LANG"} {
		t.Setenv(key, "")
	}
	if SystemLanguage() != "en" {
		t.Fatal("empty environment must use English")
	}
	t.Setenv("LANG", "tr_TR.UTF-8")
	if SystemLanguage() != "tr" {
		t.Fatal("LANG ignored")
	}
	t.Setenv("LC_MESSAGES", "de_DE.UTF-8")
	if SystemLanguage() != "de" {
		t.Fatal("LC_MESSAGES ignored")
	}
	t.Setenv("LC_ALL", "fr_FR.UTF-8")
	if SystemLanguage() != "fr" {
		t.Fatal("LC_ALL ignored")
	}
	t.Setenv("LEAVESAFE_LANGUAGE", "ja")
	if SystemLanguage() != "ja" {
		t.Fatal("explicit preference ignored")
	}
}
func TestCatalogsPreserveFormattingAndCompleteness(t *testing.T) {
	formats := regexp.MustCompile(`%[-+ #0-9.]*[a-zA-Z]`)
	source := catalogs["en"]
	if len(source) == 0 {
		t.Fatal("source catalog missing")
	}
	for code, messages := range catalogs {
		for key, text := range messages {
			expected, ok := source[key]
			if !ok {
				t.Errorf("%s: unknown key %q", code, key)
				continue
			}
			if !slices.Equal(formats.FindAllString(expected, -1), formats.FindAllString(text, -1)) {
				t.Errorf("%s: formatting contract changed: %q", code, key)
			}
		}
	}
	if !Complete("en") || !Complete("tr") || Complete("xx") {
		t.Fatal("catalog availability is incorrect")
	}
	for _, language := range Supported() {
		if !Complete(language.Code) {
			t.Fatal("partial catalog offered")
		}
	}
}
func TestTextPreservesUnknownDiagnostics(t *testing.T) {
	if Text("tr", "LANGUAGE") != "DİL" {
		t.Fatal("Turkish copy missing")
	}
	if Text("unknown", "LANGUAGE") != "LANGUAGE" {
		t.Fatal("unknown locale must choose source")
	}
	if Text("tr", "device-specific diagnostic") != "device-specific diagnostic" {
		t.Fatal("opaque diagnostic altered")
	}
}
