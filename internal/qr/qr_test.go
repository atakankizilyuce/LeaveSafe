package qr

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// The dashboard draws the code into a fixed box and positions the status panel
// beside it. A row of a different width than its neighbors would shear the
// layout, so equal visual width is a real constraint rather than a nicety.
func TestEveryRowHasTheSameWidth(t *testing.T) {
	lines, err := Lines("https://192.168.1.24:9443?key=1234567890123456")
	if err != nil {
		t.Fatalf("Lines: %v", err)
	}
	if len(lines) == 0 {
		t.Fatal("Lines returned nothing")
	}

	want := utf8.RuneCountInString(lines[0])
	for i, line := range lines {
		if got := utf8.RuneCountInString(line); got != want {
			t.Errorf("row %d is %d runes wide, want %d", i, got, want)
		}
	}
}

// Half-block characters let one text row carry two QR rows. Anything else in
// the output would either not render or not line up.
func TestOnlyBlockCharactersAreEmitted(t *testing.T) {
	lines, err := Lines("https://example.invalid")
	if err != nil {
		t.Fatalf("Lines: %v", err)
	}

	allowed := map[rune]bool{'█': true, '▀': true, '▄': true, ' ': true}
	for i, line := range lines {
		for _, r := range line {
			if !allowed[r] {
				t.Fatalf("row %d contains %q, which is not a block character", i, r)
			}
		}
	}
}

// A QR code is square, and each text row carries two of its rows.
func TestOutputIsHalfAsTallAsItIsWide(t *testing.T) {
	lines, err := Lines("https://192.168.1.24:9443?key=1234567890123456")
	if err != nil {
		t.Fatalf("Lines: %v", err)
	}

	width := utf8.RuneCountInString(lines[0])
	wantRows := (width + 1) / 2
	if len(lines) != wantRows {
		t.Errorf("got %d rows for a %d-wide code, want %d", len(lines), width, wantRows)
	}
}

// The pairing URL grew a certificate fingerprint, which makes it substantially
// longer. It still has to fit in a code the library will produce.
func TestEncodesAFullPairingURL(t *testing.T) {
	url := "https://192.168.1.24:9443?key=1234567890123456" +
		"&fp=" + strings.Repeat("ab", 32)

	lines, err := Lines(url)
	if err != nil {
		t.Fatalf("Lines on a full pairing URL: %v", err)
	}
	if len(lines) == 0 {
		t.Fatal("Lines returned nothing for a full pairing URL")
	}
}

// Different inputs must produce different codes: a renderer that silently
// returned the same picture would be worse than one that failed.
func TestDifferentInputsProduceDifferentCodes(t *testing.T) {
	a, err := Lines("https://example.invalid?key=1111111111111111")
	if err != nil {
		t.Fatalf("Lines: %v", err)
	}
	b, err := Lines("https://example.invalid?key=2222222222222222")
	if err != nil {
		t.Fatalf("Lines: %v", err)
	}
	if strings.Join(a, "\n") == strings.Join(b, "\n") {
		t.Error("two different URLs rendered to the same code")
	}
}

func TestSameInputIsStable(t *testing.T) {
	const url = "https://example.invalid?key=1111111111111111"
	a, err := Lines(url)
	if err != nil {
		t.Fatalf("Lines: %v", err)
	}
	b, err := Lines(url)
	if err != nil {
		t.Fatalf("Lines: %v", err)
	}
	if strings.Join(a, "\n") != strings.Join(b, "\n") {
		t.Error("the same URL rendered to two different codes")
	}
}

func TestRejectsInputTooLargeToEncode(t *testing.T) {
	// Well past what any QR version holds.
	if _, err := Lines(strings.Repeat("x", 8000)); err == nil {
		t.Error("an unencodable input returned no error")
	}
}
