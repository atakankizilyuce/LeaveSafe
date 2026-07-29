package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/scrypt"
)

const pinSaltBytes = 16

// scrypt parameters. N=16384, r=8, p=1 is the interactive-login preset from the
// scrypt paper: roughly 16 MiB of memory and tens of milliseconds per guess on
// ordinary hardware.
//
// The cost is paid once per disarm attempt by someone standing at their own
// laptop, where 30ms is imperceptible. It is paid per guess by anyone working
// through a stolen config file, where it is the difference between sweeping the
// whole four-digit space in under a second and taking minutes for it — on top
// of the five-attempt lockout that guards the live PIN.
const (
	scryptN      = 16384
	scryptR      = 8
	scryptP      = 1
	scryptKeyLen = 32
)

// HashPin returns a hash of pin in the form
// "scrypt$<N>$<r>$<p>$<salt>$<digest>".
//
// A four-digit PIN has ten thousand possibilities, so no hash function makes it
// genuinely hard to recover given the file — that is what the lockout is for.
// What this does buy is time: the digits are not sitting in cleartext where a
// backup, a synced folder or a pasted bug report would expose them, and reading
// them back out costs real work rather than ten thousand instant SHA-256s.
func HashPin(pin string) (string, error) {
	salt := make([]byte, pinSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate PIN salt: %w", err)
	}
	digest, err := scrypt.Key([]byte(pin), salt, scryptN, scryptR, scryptP, scryptKeyLen)
	if err != nil {
		return "", fmt.Errorf("hash PIN: %w", err)
	}
	return fmt.Sprintf("scrypt$%d$%d$%d$%s$%s",
		scryptN, scryptR, scryptP, hex.EncodeToString(salt), hex.EncodeToString(digest)), nil
}

// VerifyPinHash reports whether pin matches an encoded hash.
//
// Both the current scrypt encoding and the legacy single-round SHA-256 one are
// accepted. Rejecting the old format would lock every existing user out of
// their own disarm PIN on upgrade, with no way to set a new one without first
// disarming. NeedsRehash reports which is which so callers can upgrade the
// stored hash the next time they hold the PIN.
func VerifyPinHash(pin, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) == 0 {
		return false
	}

	switch parts[0] {
	case "scrypt":
		return verifyScrypt(pin, parts)
	case "sha256":
		return verifyLegacySHA256(pin, parts)
	default:
		return false
	}
}

// NeedsRehash reports whether an encoded hash uses an older scheme or weaker
// parameters than HashPin produces now. A hash that fails to parse also returns
// true: replacing something unreadable is the right move.
func NeedsRehash(encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "scrypt" {
		return true
	}
	n, errN := strconv.Atoi(parts[1])
	r, errR := strconv.Atoi(parts[2])
	p, errP := strconv.Atoi(parts[3])
	if errN != nil || errR != nil || errP != nil {
		return true
	}
	return n < scryptN || r < scryptR || p < scryptP
}

// verifyScrypt checks a "scrypt$N$r$p$salt$digest" encoding. The parameters
// come from the stored hash rather than the constants above, so hashes written
// by a build with different settings still verify.
func verifyScrypt(pin string, parts []string) bool {
	if len(parts) != 6 {
		return false
	}
	n, errN := strconv.Atoi(parts[1])
	r, errR := strconv.Atoi(parts[2])
	p, errP := strconv.Atoi(parts[3])
	if errN != nil || errR != nil || errP != nil {
		return false
	}
	// Guard against a hand-edited file asking for parameters that would hang
	// the process or be rejected outright. scrypt.Key wants N to be a power of
	// two greater than one, and the upper bounds keep a verify from allocating
	// gigabytes.
	if n < 2 || n > 1<<22 || n&(n-1) != 0 || r < 1 || r > 64 || p < 1 || p > 16 {
		return false
	}

	salt, err := hex.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := hex.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return false
	}

	got, err := scrypt.Key([]byte(pin), salt, n, r, p, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

// verifyLegacySHA256 checks the "sha256$salt$digest" encoding written by
// versions before the move to scrypt.
func verifyLegacySHA256(pin string, parts []string) bool {
	if len(parts) != 3 {
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
