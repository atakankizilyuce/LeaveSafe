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
func LoadOrCreateKeyFile(path string) (string, error) {
	// #nosec G304 -- path is built from the app's own config dir, never user input
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		key := strings.TrimSpace(strings.ReplaceAll(string(data), "-", ""))
		if len(key) == 16 && luhnValid(key) {
			return key, nil
		}
		// A truncated or hand-edited file is replaced rather than trusted: a
		// key that fails its own check digit could never have been produced by
		// this program, and pairing against it would fail every time.
	case !errors.Is(err, os.ErrNotExist):
		return "", fmt.Errorf("read pairing key: %w", err)
	}

	key, err := generatePairingKey()
	if err != nil {
		return "", fmt.Errorf("generate pairing key: %w", err)
	}
	if err := writeKeyFile(path, key); err != nil {
		return "", err
	}
	return key, nil
}

// SaveKeyFile writes key to path with owner-only permissions. Used when the key
// is rotated while running headless, so the next start uses the new one.
func SaveKeyFile(path, key string) error {
	return writeKeyFile(path, strings.ReplaceAll(key, "-", ""))
}

func writeKeyFile(path, key string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create key dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(key+"\n"), keyFileMode); err != nil {
		return fmt.Errorf("write pairing key: %w", err)
	}
	// WriteFile only applies the mode when it creates the file, so an existing
	// file keeps whatever permissions it had. Set them explicitly.
	if err := os.Chmod(path, keyFileMode); err != nil {
		return fmt.Errorf("restrict pairing key permissions: %w", err)
	}
	return nil
}
