package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// isolate points the config directory at a temporary location so tests never
// touch the developer's real settings.
func isolate(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		t.Setenv("APPDATA", dir)
		return filepath.Join(dir, "LeaveSafe")
	}
	t.Setenv("HOME", dir)
	return filepath.Join(dir, ".leavesafe")
}

func TestLoadWithNoFileReturnsDefaults(t *testing.T) {
	isolate(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load with no file: %v", err)
	}
	if cfg.MaxSessions != 3 || cfg.HeartbeatSeconds != 15 {
		t.Errorf("Load returned %+v, want the defaults", cfg)
	}
}

func TestSaveThenLoadRoundTrip(t *testing.T) {
	isolate(t)

	cfg := Default()
	cfg.MaxSessions = 7
	cfg.InputThreshold = 4
	cfg.Location.GeolocateKey = "secret-key"
	if err := Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.MaxSessions != 7 {
		t.Errorf("MaxSessions = %d, want 7", got.MaxSessions)
	}
	if got.InputThreshold != 4 {
		t.Errorf("InputThreshold = %d, want 4", got.InputThreshold)
	}
	if got.Location.GeolocateKey != "secret-key" {
		t.Errorf("GeolocateKey = %q, want it preserved", got.Location.GeolocateKey)
	}
}

func TestSaveWritesOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits do not apply on Windows")
	}
	dir := isolate(t)

	if err := Save(Default()); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	// The file holds a PIN hash and a geolocation API key.
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("config mode = %v, want 0600", perm)
	}
}

// The program runs on with defaults and saves over this path at the first
// settings change, so a broken file left in place would be destroyed silently —
// along with a PIN hash and an API key that exist nowhere else.
func TestCorruptConfigIsBackedUpRatherThanLost(t *testing.T) {
	dir := isolate(t)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	original := `{"max_sessions": 9, THIS IS NOT JSON`
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	cfg, err := Load()
	if err == nil {
		t.Fatal("a corrupt config loaded without an error")
	}
	if cfg.MaxSessions != 3 {
		t.Errorf("MaxSessions = %d, want the default after a corrupt load", cfg.MaxSessions)
	}

	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		t.Fatalf("read dir: %v", readErr)
	}
	var backup string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "config.json.corrupt-") {
			backup = filepath.Join(dir, e.Name())
		}
	}
	if backup == "" {
		t.Fatal("the corrupt config was not backed up")
	}
	kept, readErr := os.ReadFile(backup)
	if readErr != nil {
		t.Fatalf("read backup: %v", readErr)
	}
	if string(kept) != original {
		t.Errorf("backup holds %q, want the original bytes", kept)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Error("the corrupt file was left in place as well as backed up")
	}
	// The error has to name the backup, or the user cannot find it.
	if !strings.Contains(err.Error(), "corrupt-") {
		t.Errorf("error %q does not name the backup", err)
	}
}

func TestLoadAcceptsAPartialConfig(t *testing.T) {
	dir := isolate(t)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// A hand-written file naming one setting must not blank out the rest.
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"max_sessions": 5}`), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MaxSessions != 5 {
		t.Errorf("MaxSessions = %d, want the value from the file", cfg.MaxSessions)
	}
	if cfg.HeartbeatSeconds != 15 {
		t.Errorf("HeartbeatSeconds = %d, want the default to survive", cfg.HeartbeatSeconds)
	}
}

func TestValidateClampsNonsense(t *testing.T) {
	cfg := Default()
	cfg.MaxSessions = 0
	cfg.HeartbeatSeconds = -5
	cfg.LockoutSeconds = 999999
	cfg.Port = 70000
	cfg.InputThreshold = 0
	cfg.ConnectionMode = "carrier-pigeon"

	notes := cfg.Validate()

	if cfg.MaxSessions < 1 {
		t.Errorf("MaxSessions = %d, want it clamped to at least 1", cfg.MaxSessions)
	}
	// A zero heartbeat is a ticker panic on the next restart.
	if cfg.HeartbeatSeconds < 1 {
		t.Errorf("HeartbeatSeconds = %d, want it clamped to at least 1", cfg.HeartbeatSeconds)
	}
	// An enormous lockout is the owner locked out of their own alarm.
	if cfg.LockoutSeconds > 3600 {
		t.Errorf("LockoutSeconds = %d, want it clamped to an hour", cfg.LockoutSeconds)
	}
	if cfg.Port > 65535 {
		t.Errorf("Port = %d, want it clamped into range", cfg.Port)
	}
	if cfg.InputThreshold < 1 {
		t.Errorf("InputThreshold = %d, want it clamped to at least 1", cfg.InputThreshold)
	}
	if cfg.ConnectionMode != "wifi" {
		t.Errorf("ConnectionMode = %q, want the unknown value replaced", cfg.ConnectionMode)
	}
	if len(notes) < 6 {
		t.Errorf("Validate reported %d adjustments, want one per clamped field: %v", len(notes), notes)
	}
}

func TestValidateLeavesGoodValuesAlone(t *testing.T) {
	cfg := Default()
	before := *cfg

	if notes := cfg.Validate(); len(notes) != 0 {
		t.Errorf("Validate adjusted the defaults: %v", notes)
	}
	if cfg.MaxSessions != before.MaxSessions || cfg.HeartbeatSeconds != before.HeartbeatSeconds {
		t.Error("Validate changed values that were already valid")
	}
}

func TestValidateClampsAlarmLevels(t *testing.T) {
	cfg := Default()
	cfg.Alarm.Levels = []AlarmLevel{
		{DelaySeconds: -10, Action: "medium_volume", VolumePercent: 500},
		{DelaySeconds: 5, Action: "full_volume", VolumePercent: 50},
	}

	cfg.Validate()

	if cfg.Alarm.Levels[0].DelaySeconds < 0 {
		t.Errorf("delay = %d, want it clamped to at least 0", cfg.Alarm.Levels[0].DelaySeconds)
	}
	if cfg.Alarm.Levels[0].VolumePercent > 100 {
		t.Errorf("volume = %d%%, want it clamped to 100", cfg.Alarm.Levels[0].VolumePercent)
	}
	if cfg.Alarm.Levels[1].VolumePercent != 50 {
		t.Errorf("a valid volume was changed to %d", cfg.Alarm.Levels[1].VolumePercent)
	}
}

// The location poll is optional, and a zero there means "unset" rather than
// "poll as fast as possible".
func TestValidateLeavesAnUnsetPollAlone(t *testing.T) {
	cfg := Default()
	cfg.Location.PollSeconds = 0

	cfg.Validate()

	if cfg.Location.PollSeconds != 0 {
		t.Errorf("PollSeconds = %d, want an unset value left at 0", cfg.Location.PollSeconds)
	}
}

func TestUpdateCheckDefaultsToOn(t *testing.T) {
	cfg := Default()
	if !cfg.UpdateCheckEnabled() {
		t.Error("the update check is off by default")
	}

	off := false
	cfg.UpdateCheck = &off
	if cfg.UpdateCheckEnabled() {
		t.Error("the update check ran despite being switched off")
	}

	on := true
	cfg.UpdateCheck = &on
	if !cfg.UpdateCheckEnabled() {
		t.Error("the update check is off despite being switched on")
	}
}

// The legacy cleartext PIN field must still round-trip, since startup reads it
// to migrate old configs.
func TestLegacyPinFieldSurvivesADecode(t *testing.T) {
	var cfg Config
	if err := json.Unmarshal([]byte(`{"pin_protection":{"enabled":true,"pin":"4271"}}`), &cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if cfg.PinProtection.Pin != "4271" {
		t.Errorf("legacy PIN = %q, want it readable for migration", cfg.PinProtection.Pin)
	}
}

// An empty PIN hash must not be written into the file as an empty string that
// later reads as "a PIN is set".
func TestEmptyPinFieldsAreOmitted(t *testing.T) {
	data, err := json.Marshal(Default())
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if strings.Contains(string(data), `"pin"`) || strings.Contains(string(data), `"pin_hash"`) {
		t.Errorf("empty PIN fields were written: %s", data)
	}
}
