package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strings"
)

const pinSaltBytes = 16

// HashPin returns a salted hash of pin in the form "sha256$<salt>$<digest>".
//
// A disarm PIN is a few digits, so no hash makes it survive an offline attack;
// this exists so the code is not sitting in cleartext in config.json, where a
// backup or a pasted bug report would expose it.
func HashPin(pin string) (string, error) {
	salt := make([]byte, pinSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate PIN salt: %w", err)
	}
	sum := sha256.Sum256(append(salt, []byte(pin)...))
	return "sha256$" + hex.EncodeToString(salt) + "$" + hex.EncodeToString(sum[:]), nil
}

// VerifyPinHash reports whether pin matches an encoded hash produced by
// HashPin. The digest comparison is constant-time; a malformed encoding never
// matches.
func VerifyPinHash(pin, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 3 || parts[0] != "sha256" {
		return false
	}
	salt, err := hex.DecodeString(parts[1])
	if err != nil {
		return false
	}
	want, err := hex.DecodeString(parts[2])
	if err != nil {
		return false
	}
	sum := sha256.Sum256(append(salt, []byte(pin)...))
	return subtle.ConstantTimeCompare(sum[:], want) == 1
}
