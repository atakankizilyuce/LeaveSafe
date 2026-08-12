// Package certfp is the one place that says what a certificate fingerprint is.
//
// LeaveSafe's certificate is self-signed, so nothing in a chain vouches for it.
// What stands in for that is this string: the server derives it, the QR code
// carries it, the phone stores it and refuses to talk to anything presenting a
// different one, and the reachability check uses it to tell "this machine
// answered" apart from "something at that address answered". Four places and
// one format — and any two of them disagreeing about the format is a phone that
// will not pair with a machine that is working perfectly.
package certfp

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// Of is the fingerprint of a certificate, in the form this program shows it:
// the SHA-256 of the DER, as lowercase hex, a colon between every byte.
func Of(der []byte) string {
	sum := sha256.Sum256(der)
	parts := make([]string, len(sum))
	for i, b := range sum {
		parts[i] = hex.EncodeToString([]byte{b})
	}
	return strings.Join(parts, ":")
}

// Equal reports whether two fingerprints name the same certificate.
//
// The colons and the case are presentation, not content. The dashboard shows
// the colons because they make sixty-four hex characters readable; the URL
// fragment drops them because every character in a QR code costs modules, and
// past a certain payload the code needs a bigger grid than the terminal has
// rows for. Comparing the two forms as strings would say a certificate is not
// itself.
func Equal(a, b string) bool {
	return normal(a) == normal(b)
}

func normal(fp string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(fp), ":", ""))
}
