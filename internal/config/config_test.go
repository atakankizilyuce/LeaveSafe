package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
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

// An absent channel means stable. Existing configs have no such field, and
// silently opting those users into prereleases would be the wrong default.
func TestUpdateChannelDefaultsToStable(t *testing.T) {
	cfg := Default()
	if cfg.UpdateChannel != "" {
		t.Errorf("UpdateChannel = %q, want it unset so stable is implied", cfg.UpdateChannel)
	}
	if notes := cfg.Validate(); len(notes) != 0 {
		t.Errorf("Validate objected to the default channel: %v", notes)
	}
}

func TestValidateAcceptsBothChannels(t *testing.T) {
	for _, channel := range []string{"stable", "beta"} {
		cfg := Default()
		cfg.UpdateChannel = channel
		if notes := cfg.Validate(); len(notes) != 0 {
			t.Errorf("Validate rejected %q: %v", channel, notes)
		}
		if cfg.UpdateChannel != channel {
			t.Errorf("UpdateChannel = %q, want %q", cfg.UpdateChannel, channel)
		}
	}
}

// A typo must not stop a security monitor from starting, and must not opt anyone
// into prereleases either.
func TestValidateResetsAnUnknownChannel(t *testing.T) {
	cfg := Default()
	cfg.UpdateChannel = "carrier-pigeon"

	notes := cfg.Validate()

	if cfg.UpdateChannel != "stable" {
		t.Errorf("UpdateChannel = %q, want stable", cfg.UpdateChannel)
	}
	if len(notes) == 0 {
		t.Error("the channel was reset without saying so")
	}
}

func TestUpdateCheckIntervalDefaults(t *testing.T) {
	cfg := Default()
	if got, want := cfg.UpdateCheckInterval(), 24*time.Hour; got != want {
		t.Errorf("UpdateCheckInterval() = %v, want %v", got, want)
	}

	cfg.UpdateCheckHours = 12
	if got, want := cfg.UpdateCheckInterval(), 12*time.Hour; got != want {
		t.Errorf("UpdateCheckInterval() = %v, want %v", got, want)
	}

	// A negative value in a hand-edited config must not produce a negative wait.
	cfg.UpdateCheckHours = -5
	if got, want := cfg.UpdateCheckInterval(), 24*time.Hour; got != want {
		t.Errorf("UpdateCheckInterval() = %v, want the default %v", got, want)
	}
}

// The floor protects GitHub's rate limit from a hand-edited config; the ceiling
// keeps the check meaningful.
func TestValidateClampsTheCheckInterval(t *testing.T) {
	cases := []struct{ in, want int }{
		{1, 24},       // below the floor, so the default
		{5, 24},       // still below
		{6, 6},        // exactly the floor
		{24, 24},      // untouched
		{168, 168},    // exactly the ceiling
		{100000, 168}, // above the ceiling
		{0, 0},        // unset means the default, and must be left alone
	}
	for _, c := range cases {
		cfg := Default()
		cfg.UpdateCheckHours = c.in
		cfg.Validate()
		if cfg.UpdateCheckHours != c.want {
			t.Errorf("update_check_hours %d clamped to %d, want %d", c.in, cfg.UpdateCheckHours, c.want)
		}
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

// The geolocation API key travels in geolocate_url's query string, so a plain
// HTTP endpoint puts it on the wire in cleartext — on whatever café Wi-Fi the
// machine happens to be sitting on. The phone was already refused a non-HTTPS
// endpoint when it set one; the config file, which is the documented way to
// configure this, was not checked at all.
func TestValidateRefusesPlainHTTPLocationEndpoints(t *testing.T) {
	cfg := Default()
	cfg.Location.GeolocateURL = "http://attacker.example/geolocate"
	cfg.Location.IPLookupURL = "http://attacker.example/ip"

	notes := cfg.Validate()

	if cfg.Location.GeolocateURL != "" {
		t.Errorf("geolocate_url = %q, want it dropped so the HTTPS default takes over",
			cfg.Location.GeolocateURL)
	}
	if cfg.Location.IPLookupURL != "" {
		t.Errorf("ip_lookup_url = %q, want it dropped", cfg.Location.IPLookupURL)
	}
	if len(notes) < 2 {
		t.Errorf("dropped two endpoints but reported %d adjustments; a silent drop is a "+
			"setting that looks applied and is not", len(notes))
	}
}

// The rule must not cost the legitimate case: an HTTPS endpoint is exactly how
// someone points LeaveSafe at a service other than Google's.
func TestValidateKeepsHTTPSLocationEndpoints(t *testing.T) {
	cfg := Default()
	cfg.Location.GeolocateURL = "https://geo.example/v1/geolocate"
	cfg.Location.IPLookupURL = "https://ip.example/json"

	cfg.Validate()

	if cfg.Location.GeolocateURL != "https://geo.example/v1/geolocate" {
		t.Errorf("an HTTPS geolocate_url was dropped: %q", cfg.Location.GeolocateURL)
	}
	if cfg.Location.IPLookupURL != "https://ip.example/json" {
		t.Errorf("an HTTPS ip_lookup_url was dropped: %q", cfg.Location.IPLookupURL)
	}
}

// An unrecognized language is cleared rather than replaced with a guess: empty
// means "ask", and asking beats picking a language for someone on the strength
// of a typo.
func TestValidateAsksAgainAfterAnUnknownLanguage(t *testing.T) {
	cfg := Default()
	cfg.Language = "elvish"

	notes := cfg.Validate()

	if cfg.Language != "" {
		t.Errorf("Language = %q, want it cleared so the user is asked", cfg.Language)
	}
	if len(notes) != 1 {
		t.Errorf("Validate reported %d adjustments, want just the language: %v", len(notes), notes)
	}
}

// A language the program does have is left exactly as it was found.
func TestValidateKeepsALanguageItKnows(t *testing.T) {
	for _, want := range []string{"tr", "en"} {
		cfg := Default()
		cfg.Language = want
		if notes := cfg.Validate(); len(notes) != 0 {
			t.Errorf("%q was adjusted: %v", want, notes)
		}
		if cfg.Language != want {
			t.Errorf("Language = %q, want %q left alone", cfg.Language, want)
		}
	}
}
