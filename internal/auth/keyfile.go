package auth

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// KeyFileName is the file a persisted pairing key is stored in, inside the
// application's config directory.
const KeyFileName = "pairing.key"

// keyFileMode keeps the key readable only by its owner. It is the single secret
// guarding the alarm, so it must not be readable by other accounts on a shared
// machine.
const keyFileMode os.FileMode = 0o600

// LoadOrCreateKeyFile returns the pairing key stored at path, creating a new one
// if the file is missing or does not hold a usable key.
//
// Persisting the key is a deliberate trade, and not the default. Run
// interactively, LeaveSafe generates a fresh key every start and shows it as a
// QR code: nothing is ever written down, and a key seen once is worthless the
// next day. That falls apart when the program starts at login with nobody
// watching — there is no screen to read the key from, and a phone paired before
// the reboot would be locked out by a key it never saw. Headless operation
// therefore keeps the key on disk, owner-readable only, and the pairing survives
// the restart it is there to cover.
//
// The salt is stored beside the key, on the second line, because it has
// exactly the key's lifetime: minted with it, replaced with it, and worth
// keeping for as long as it is. A restart that kept the key and minted a fresh
// salt would cost every paired phone a fresh Argon2 derivation for nothing.
//
// A file written before the salt existed has one line. It is not rewritten on
// read — a salt minted per start still pairs, because it travels in the
// greeting — and the salt it is given is persisted the next time the key is.
func LoadOrCreateKeyFile(path string) (key, salt string, err error) {
	// #nosec G304 -- path is built from the app's own config dir, never user input
	data, readErr := os.ReadFile(path)
	switch {
	case readErr == nil:
		key, salt = parseKeyFile(string(data))
		if key != "" {
			return key, salt, nil
		}
		// A truncated or hand-edited file is replaced rather than trusted: a
		// key that fails its own check digit could never have been produced by
		// this program, and pairing against it would fail every time.
	case !errors.Is(readErr, os.ErrNotExist):
		return "", "", fmt.Errorf("read pairing key: %w", readErr)
	}

	key, err = generatePairingKey()
	if err != nil {
		return "", "", fmt.Errorf("generate pairing key: %w", err)
	}
	salt, err = generatePairingSalt()
	if err != nil {
		return "", "", err
	}
	if err := writeKeyFile(path, key, salt); err != nil {
		return "", "", err
	}
	return key, salt, nil
}

// parseKeyFile reads a key and, if the file carries one, the salt beside it.
// An empty key means the file held nothing this program could have written.
func parseKeyFile(data string) (key, salt string) {
	lines := strings.SplitN(strings.TrimSpace(data), "\n", 2)

	key = strings.TrimSpace(strings.ReplaceAll(lines[0], "-", ""))
	if len(key) != 16 || !luhnValid(key) {
		return "", ""
	}
	if len(lines) == 2 {
		salt = strings.TrimSpace(lines[1])
	}
	return key, salt
}

// SaveKeyFile writes key and its salt to path with owner-only permissions.
// Used when the key is rotated while running headless, so the next start uses
// the new one.
func SaveKeyFile(path, key, salt string) error {
	return writeKeyFile(path, strings.ReplaceAll(key, "-", ""), salt)
}

func writeKeyFile(path, key, salt string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create key dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(key+"\n"+salt+"\n"), keyFileMode); err != nil {
		return fmt.Errorf("write pairing key: %w", err)
	}
	// WriteFile only applies the mode when it creates the file, so an existing
	// file keeps whatever permissions it had. Set them explicitly.
	if err := os.Chmod(path, keyFileMode); err != nil {
		return fmt.Errorf("restrict pairing key permissions: %w", err)
	}
	return nil
}
