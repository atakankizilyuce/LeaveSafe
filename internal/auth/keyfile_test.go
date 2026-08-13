package auth

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The persisted pairing key is the whole of the front door on a headless
// install. It is written once and then read on every start for months, with
// nobody watching either event — so the failure that matters is not a crash but
// a silent one: a key quietly replaced locks out the phone it was there to keep
// paired, and a key quietly kept when it should not be is a stale secret.

// aKey builds a key the program itself would accept: the digits, plus the check
// digit this package computes for them. Written out as a constant it would be a
// magic number nobody could verify — and the first draft of this file had one
// that was wrong, which every test then agreed with.
func aKey(digits string) string {
	return digits + string(luhnCheckDigit(digits))
}

// readKey returns what is actually on disk, normalised the way the loader
// normalises it.
func readKey(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the key file back: %v", err)
	}
	return strings.TrimSpace(string(data))
}

func TestAMissingKeyFileIsCreatedWithAUsableKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), KeyFileName)

	key, err := LoadOrCreateKeyFile(path)
	if err != nil {
		t.Fatalf("LoadOrCreateKeyFile: %v", err)
	}

	if len(key) != 16 {
		t.Errorf("key is %d digits, want 16: %q", len(key), key)
	}
	// The check digit is what makes a mistyped digit a typo rather than a
	// failed pairing attempt against the lockout counter.
	if !luhnValid(key) {
		t.Errorf("the generated key does not pass its own check digit: %q", key)
	}
	if stored := readKey(t, path); stored != key {
		t.Errorf("the file holds %q and the caller was given %q", stored, key)
	}
}

// The key is the single secret guarding the alarm. On a shared machine another
// account reading it is the whole compromise, so the file is owner-only — and
// checked here rather than assumed, because os.WriteFile applies its mode only
// when it creates the file.
func TestTheKeyFileIsOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits are not what guards this file on Windows")
	}
	path := filepath.Join(t.TempDir(), KeyFileName)

	if _, err := LoadOrCreateKeyFile(path); err != nil {
		t.Fatalf("LoadOrCreateKeyFile: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != keyFileMode {
		t.Errorf("key file mode is %04o, want %04o", perm, keyFileMode)
	}
}

// The case the explicit chmod in writeKeyFile exists for: a file that already
// exists keeps the permissions it had, so a key rotated into a world-readable
// file would stay world-readable.
func TestRewritingAKeyRestrictsAFileThatWasLoose(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits are not what guards this file on Windows")
	}
	path := filepath.Join(t.TempDir(), KeyFileName)
	if err := os.WriteFile(path, []byte(aKey("111111111111111")+"\n"), 0o644); err != nil {
		t.Fatalf("plant a loose key file: %v", err)
	}

	if err := SaveKeyFile(path, aKey("222222222222222")); err != nil {
		t.Fatalf("SaveKeyFile: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != keyFileMode {
		t.Errorf("key file mode is %04o after rewriting, want %04o", perm, keyFileMode)
	}
}

// A key that is already good is kept. Generating a new one on every start would
// defeat the whole point of the file: the phone paired before the reboot would
// be refused by a key it never saw.
func TestAValidStoredKeyIsKept(t *testing.T) {
	path := filepath.Join(t.TempDir(), KeyFileName)
	first, err := LoadOrCreateKeyFile(path)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	second, err := LoadOrCreateKeyFile(path)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}

	if second != first {
		t.Errorf("the stored key changed between starts: %q then %q", first, second)
	}
}

// What a person writes down, and what the dashboard shows, carries grouping
// dashes. A file hand-copied from either has to be read back as the same key.
func TestAStoredKeyIsAcceptedWithDashesAndStrayWhitespace(t *testing.T) {
	want := aKey("111111111111111")
	grouped := want[0:4] + "-" + want[4:8] + "-" + want[8:12] + "-" + want[12:16]
	cases := map[string]string{
		"grouped with dashes":  grouped,
		"trailing newline":     want + "\n",
		"leading whitespace":   "  " + want,
		"windows line ending":  want + "\r\n",
		"dashes and a newline": grouped + "\n",
	}
	for name, stored := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), KeyFileName)
			if err := os.WriteFile(path, []byte(stored), keyFileMode); err != nil {
				t.Fatalf("plant the key file: %v", err)
			}

			got, err := LoadOrCreateKeyFile(path)
			if err != nil {
				t.Fatalf("LoadOrCreateKeyFile: %v", err)
			}
			if got != want {
				t.Errorf("read %q back as %q, want %q", stored, got, want)
			}
		})
	}
}

// A file this program could not have written is replaced rather than trusted.
// Pairing against a key that fails its own check digit would fail every time,
// and every failure counts against the lockout — so the owner would be locked
// out of their own laptop by a corrupt file.
func TestAKeyThatCouldNotHaveBeenGeneratedIsReplaced(t *testing.T) {
	good := aKey("111111111111111")
	wrongCheck := good[:15] + string('0'+(good[15]-'0'+1)%10)
	cases := map[string]string{
		"empty file":          "",
		"truncated":           good[:15],
		"one digit too many":  good + "1",
		"fails the check":     wrongCheck,
		"not digits at all":   "not-a-pairing-key",
		"a novel":             strings.Repeat("1", 4096),
		"digits with a space": good[0:4] + " " + good[4:8] + " " + good[8:12] + " " + good[12:16],
		"nul bytes":           good + "\x00\x00",
	}
	for name, stored := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), KeyFileName)
			if err := os.WriteFile(path, []byte(stored), keyFileMode); err != nil {
				t.Fatalf("plant the key file: %v", err)
			}

			got, err := LoadOrCreateKeyFile(path)
			if err != nil {
				t.Fatalf("LoadOrCreateKeyFile: %v", err)
			}
			if got == strings.TrimSpace(stored) {
				t.Fatalf("an unusable stored key was handed back as it was: %q", got)
			}
			if len(got) != 16 || !luhnValid(got) {
				t.Errorf("the replacement is not a usable key: %q", got)
			}
			if written := readKey(t, path); written != got {
				t.Errorf("the replacement was not written down: file has %q, caller got %q", written, got)
			}
		})
	}
}

// A file that exists but cannot be read is not the same as one that is missing,
// and the difference matters more here than almost anywhere else. Treating an
// unreadable file as absent would mint a new key and lock out the paired phone
// — over a permission problem that a retry might fix.
func TestAnUnreadableKeyFileIsAnErrorRatherThanANewKey(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod does not deny the owner a read on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root reads it anyway")
	}
	path := filepath.Join(t.TempDir(), KeyFileName)
	if err := os.WriteFile(path, []byte(aKey("111111111111111")+"\n"), keyFileMode); err != nil {
		t.Fatalf("plant the key file: %v", err)
	}
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("make it unreadable: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, keyFileMode) })

	if _, err := LoadOrCreateKeyFile(path); err == nil {
		t.Error("an unreadable key file was treated as a missing one")
	}
}

// Nowhere to write is reported rather than swallowed. A headless start that
// cannot persist its key would come back with a different one after every
// reboot, and the owner would be re-pairing from a QR code they cannot see.
func TestAKeyThatCannotBeWrittenIsAnError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a read-only directory does not stop a write on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root writes there anyway")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("make the directory read-only: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	if _, err := LoadOrCreateKeyFile(filepath.Join(dir, KeyFileName)); err == nil {
		t.Error("a key file that could not be written was reported as written")
	}
}

// The other way the directory can fail, and the one that needs no permission
// bits to provoke: something that is not a directory already sits where one has
// to be. A stray file named like the config directory is enough, and the answer
// has to be an error rather than a key nobody can read back.
func TestAKeyDirectoryBlockedByAFileIsAnError(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(blocker, []byte("in the way"), 0o600); err != nil {
		t.Fatalf("plant the blocking file: %v", err)
	}

	if _, err := LoadOrCreateKeyFile(filepath.Join(blocker, KeyFileName)); err == nil {
		t.Error("a key directory blocked by a file was reported as created")
	}
}

// The directory is created on the way, because the first headless start writes
// the key before anything else has had a reason to make the config directory.
func TestTheKeyDirectoryIsCreatedOnDemand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "deeper", KeyFileName)

	key, err := LoadOrCreateKeyFile(path)
	if err != nil {
		t.Fatalf("LoadOrCreateKeyFile: %v", err)
	}
	if stored := readKey(t, path); stored != key {
		t.Errorf("the key was not written into the directory it made: %q", stored)
	}
}

// Rotation has to reach the file, or the next start comes back with the key the
// user just invalidated. The dashboard hands it over grouped, so the dashes
// have to come off on the way in.
func TestSavingARotatedKeyStripsItsGrouping(t *testing.T) {
	path := filepath.Join(t.TempDir(), KeyFileName)

	want := aKey("222222222222222")
	grouped := want[0:4] + "-" + want[4:8] + "-" + want[8:12] + "-" + want[12:16]

	if err := SaveKeyFile(path, grouped); err != nil {
		t.Fatalf("SaveKeyFile: %v", err)
	}

	if stored := readKey(t, path); stored != want {
		t.Errorf("the file holds %q, want the key without its grouping", stored)
	}
	back, err := LoadOrCreateKeyFile(path)
	if err != nil {
		t.Fatalf("LoadOrCreateKeyFile after saving: %v", err)
	}
	if back != want {
		t.Errorf("the saved key read back as %q", back)
	}
}
