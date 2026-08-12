package certfp

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

// The format is a protocol detail, not a preference. The server derives it, the
// QR code carries it and the phone stores it, so a change here is a change to
// what a paired phone will accept — every phone paired before it stops
// connecting. This is the test that says so out loud.
func TestTheFingerprintIsColonSeparatedLowercaseHexOfTheSHA256(t *testing.T) {
	der := []byte("not a real certificate, but bytes are bytes")

	got := Of(der)

	sum := sha256.Sum256(der)
	if want := hex.EncodeToString(sum[:]); strings.ReplaceAll(got, ":", "") != want {
		t.Errorf("fingerprint = %q, which is not the SHA-256 of the input", got)
	}
	if parts := strings.Split(got, ":"); len(parts) != sha256.Size {
		t.Errorf("fingerprint has %d colon-separated parts, want %d", len(parts), sha256.Size)
	}
	if got != strings.ToLower(got) {
		t.Errorf("fingerprint = %q, want it lowercase", got)
	}
}

func TestTwoCertificatesDoNotShareAFingerprint(t *testing.T) {
	if Of([]byte("one")) == Of([]byte("another")) {
		t.Error("two different inputs produced the same fingerprint")
	}
}

// The two forms in circulation are the same fingerprint. The dashboard shows
// the colons because they make sixty-four hex characters readable; the URL
// fragment drops them because every character costs modules in a QR code, and
// past a certain payload the code needs a bigger grid than the terminal has
// rows for. Comparing them as strings would say a certificate is not itself —
// and the thing that comparison guards is whether a phone will pair.
func TestTheColonsAndTheCaseArePresentation(t *testing.T) {
	withColons := "AA:BB:CC:dd:ee:ff"

	cases := map[string]string{
		"the same string":     withColons,
		"lowercased":          "aa:bb:cc:dd:ee:ff",
		"uppercased":          "AA:BB:CC:DD:EE:FF",
		"without the colons":  "aabbccddeeff",
		"the QR code's form":  "aabbccddeeff",
		"with space around":   "  AA:BB:CC:dd:ee:ff  ",
		"uppercase, no colon": "AABBCCDDEEFF",
	}
	for name, other := range cases {
		t.Run(name, func(t *testing.T) {
			if !Equal(withColons, other) {
				t.Errorf("Equal(%q, %q) = false, want them to name the same certificate", withColons, other)
			}
		})
	}
}

func TestADifferentCertificateIsNotEqual(t *testing.T) {
	if Equal("aa:bb:cc", "aa:bb:cd") {
		t.Error("two different fingerprints compared equal")
	}
	if Equal("aa:bb:cc", "") {
		t.Error("a fingerprint compared equal to nothing at all")
	}
}
