package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// AlarmLevel defines one step in the alarm escalation chain.
type AlarmLevel struct {
	DelaySeconds  int    `json:"delay_seconds"`
	Action        string `json:"action"`
	VolumePercent int    `json:"volume_percent,omitempty"`
}

// AlarmConfig controls how the alarm escalates.
type AlarmConfig struct {
	EscalationEnabled bool         `json:"escalation_enabled"`
	Levels            []AlarmLevel `json:"levels"`
}

// PinProtection controls optional PIN-based disarm protection.
type PinProtection struct {
	Enabled bool `json:"enabled"`
	// Pin is the legacy cleartext field. It is read for migration only: on
	// startup a non-empty value is hashed into PinHash and cleared.
	Pin string `json:"pin,omitempty"`
	// PinHash is the salted hash of the disarm PIN, produced by auth.HashPin.
	PinHash string `json:"pin_hash,omitempty"`
}

// Location controls whether and how the monitored machine reports where it is.
//
// It is off by default. Everything in here except the phone anchor involves
// talking to a third party, which is the one thing this program otherwise never
// does, so it is a decision the user makes rather than one made for them.
type Location struct {
	// Enabled turns the whole feature on. With it off nothing is scanned and
	// no request leaves the machine.
	Enabled bool `json:"enabled"`
	// PollSeconds is how often the live providers are asked for a position.
	PollSeconds int `json:"poll_seconds,omitempty"`
	// PhoneAnchor records the paired phone's own position when arming, as a
	// stand-in for the laptop's. Costs nothing and reaches no third party.
	PhoneAnchor bool `json:"phone_anchor"`
	// IPFallback looks up the public IP address. Accurate to a city.
	IPFallback bool `json:"ip_fallback"`
	// IPLookupURL overrides the IP geolocation endpoint.
	IPLookupURL string `json:"ip_lookup_url,omitempty"`
	// WiFiEnabled resolves a Wi-Fi scan through a geolocation service.
	// Accurate to tens of meters, and the only source that stays right after
	// the machine has been moved.
	WiFiEnabled bool `json:"wifi_enabled"`
	// GeolocateURL overrides the Wi-Fi geolocation endpoint. The default
	// speaks Google's Geolocation API, which several other services implement.
	GeolocateURL string `json:"geolocate_url,omitempty"`
	// GeolocateKey is the API key for that service. It is never sent to a
	// paired client.
	GeolocateKey string `json:"geolocate_key,omitempty"`
}

// Config holds all application settings.
type Config struct {
	Port                   int `json:"port"`
	MaxSessions            int `json:"max_sessions"`
	MaxAuthAttempts        int `json:"max_auth_attempts"`
	LockoutSeconds         int `json:"lockout_seconds"`
	HeartbeatSeconds       int `json:"heartbeat_seconds"`
	DisconnectGraceSeconds int `json:"disconnect_grace_seconds"`
	// SessionTTLMinutes is how long a paired session stays valid before the
	// phone has to present the pairing key again. Zero means no expiry.
	SessionTTLMinutes int `json:"session_ttl_minutes,omitempty"`
	// SessionIdleMinutes drops a session that has gone quiet for this long.
	// Zero means idle sessions are kept.
	SessionIdleMinutes int  `json:"session_idle_minutes,omitempty"`
	AutoArmOnLock      bool `json:"auto_arm_on_lock"`
	InputThreshold     int  `json:"input_threshold"`
	// RestoreArmedState re-arms on startup when the previous run ended while
	// armed. Off by default: a machine that was armed when it shut down will,
	// on the next boot, see its own lid open and its own user type — and start
	// screaming at the person who just turned it on. With it off the interrupted
	// monitoring is still reported prominently, which is the part that matters.
	RestoreArmedState bool `json:"restore_armed_state"`
	// UpdateCheck asks GitHub whether a newer release exists and reports it on
	// the dashboard and to the paired phone. Nothing is downloaded and nothing
	// is replaced. It is a pointer so an existing config without the field can
	// be told apart from one where the user switched it off; nil means on.
	UpdateCheck *bool `json:"update_check,omitempty"`
	// UpdateChannel selects which releases count: "stable" hears about full
	// releases only, "beta" hears about prereleases as well. Empty means stable,
	// because someone who has not asked for prereleases has asked for the
	// opposite.
	UpdateChannel string `json:"update_channel,omitempty"`
	// UpdateCheckHours is how often to ask, in hours. Zero means the default.
	// The check has to repeat: a copy installed as a service runs for weeks, and
	// asking only at startup means the longest-running installations are the
	// least likely to hear that a fix exists.
	UpdateCheckHours int `json:"update_check_hours,omitempty"`
	// Language is which of the two languages the terminal's first-run questions
	// are asked in: "tr" or "en". Empty means nobody has been asked yet, which
	// is what makes the question a first-run one rather than a nag — and why it
	// is a plain string with a meaningful zero rather than a defaulted one.
	//
	// It reaches no further than those questions. The dashboard, the log and the
	// phone are in English, and claiming otherwise by translating the first
	// screen and nothing else would be worse than not asking.
	Language       string          `json:"language,omitempty"`
	Alarm          AlarmConfig     `json:"alarm"`
	PinProtection  PinProtection   `json:"pin_protection"`
	ConnectionMode string          `json:"connection_mode,omitempty"`
	EnabledSensors map[string]bool `json:"enabled_sensors,omitempty"`
	RemoteAccess   *bool           `json:"remote_access,omitempty"`
	RemotePort     int             `json:"remote_port,omitempty"`
	Location       Location        `json:"location"`
}

// Default returns a Config with sensible defaults.
func Default() *Config {
	return &Config{
		Port:                   0,
		MaxSessions:            3,
		MaxAuthAttempts:        5,
		LockoutSeconds:         60,
		HeartbeatSeconds:       15,
		DisconnectGraceSeconds: 30,
		// A day of absolute lifetime and eight hours of idle. Long enough that
		// a phone paired in the morning is still paired at the end of the day,
		// short enough that a token lifted from a phone left on a desk does not
		// stay useful forever.
		SessionTTLMinutes:  24 * 60,
		SessionIdleMinutes: 8 * 60,
		AutoArmOnLock:      false,
		InputThreshold:     1,
		RestoreArmedState:  false,
		Alarm: AlarmConfig{
			EscalationEnabled: false,
			Levels: []AlarmLevel{
				{DelaySeconds: 0, Action: "notify_phone_only"},
				{DelaySeconds: 15, Action: "medium_volume", VolumePercent: 50},
				{DelaySeconds: 30, Action: "full_volume", VolumePercent: 100},
			},
		},
		ConnectionMode: "wifi",
		RemotePort:     9443,
		PinProtection: PinProtection{
			Enabled: false,
		},
		Location: Location{
			// Off by default. Turning it on is a decision to talk to a
			// geolocation service, and nobody should discover after the fact
			// that their laptop has been doing that.
			Enabled:     false,
			PollSeconds: 60,
			PhoneAnchor: true,
			IPFallback:  true,
			WiFiEnabled: false,
		},
	}
}

// UpdateCheckEnabled reports whether the update check should run.
// Absent from the config means yes: a security program that never mentions its
// own updates is one people keep running with known flaws.
func (c *Config) UpdateCheckEnabled() bool {
	return c.UpdateCheck == nil || *c.UpdateCheck
}

// DefaultUpdateCheckHours is how often the update check runs when the config
// does not say. Once a day is often enough that a fix is heard about promptly,
// and rare enough to be invisible against GitHub's rate limit.
const DefaultUpdateCheckHours = 24

// UpdateCheckInterval reports how often to ask. Callers never interpret the
// zero value themselves.
func (c *Config) UpdateCheckInterval() time.Duration {
	hours := c.UpdateCheckHours
	if hours <= 0 {
		hours = DefaultUpdateCheckHours
	}
	return time.Duration(hours) * time.Hour
}

// ConfigDir returns the platform-appropriate config directory.
func ConfigDir() string {
	if runtime.GOOS == "windows" {
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, "LeaveSafe")
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".leavesafe"
	}
	return filepath.Join(home, ".leavesafe")
}

// ConfigPath returns the full path to the config file.
func ConfigPath() string {
	return filepath.Join(ConfigDir(), "config.json")
}

// Load reads the config file from disk. If the file does not exist,
// it returns defaults without error.
//
// A file that exists but does not parse is moved aside rather than ignored.
// The program goes on to run with defaults and will save over the path at the
// first settings change, so leaving the broken file in place would quietly
// destroy a config a user might have hand-edited — including a PIN hash and a
// geolocation API key that are not recoverable from anywhere else. The returned
// error names the backup.
func Load() (*Config, error) {
	cfg := Default()

	path := ConfigPath()
	// #nosec G304 -- path is the app's own config location, never user input
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		backup, backupErr := backupCorruptConfig(path)
		if backupErr != nil {
			// The parse failure is what the caller acts on; the backup failure
			// is why the file is still sitting there, so both are wrapped.
			return Default(), fmt.Errorf("config is not valid JSON (%w), and it could not be backed up (%w)", err, backupErr)
		}
		return Default(), fmt.Errorf("config is not valid JSON (%w); the previous file was kept as %s", err, backup)
	}

	return cfg, nil
}

// backupCorruptConfig renames an unparsable config out of the way and returns
// the new path.
func backupCorruptConfig(path string) (string, error) {
	backup := fmt.Sprintf("%s.corrupt-%d", path, time.Now().Unix())
	if err := os.Rename(path, backup); err != nil {
		return "", err
	}
	return backup, nil
}

// Validate clamps values that would break the program if honored literally and
// returns a description of every adjustment it made.
//
// A hand-edited config is the normal way to change most of these, and a typo
// there should not produce a monitor that never heartbeats or an alarm that
// locks the owner out for a year. Fixing the value and saying so beats both
// refusing to start and silently obeying.
func (c *Config) Validate() []string {
	var notes []string

	clampInt := func(name string, value *int, minimum, maximum, fallback int) {
		switch {
		case *value < minimum:
			notes = append(notes, fmt.Sprintf("%s was %d, below the minimum of %d — using %d", name, *value, minimum, fallback))
			*value = fallback
		case maximum > 0 && *value > maximum:
			notes = append(notes, fmt.Sprintf("%s was %d, above the maximum of %d — using %d", name, *value, maximum, maximum))
			*value = maximum
		}
	}

	clampInt("port", &c.Port, 0, 65535, 0)
	clampInt("remote_port", &c.RemotePort, 0, 65535, 9443)
	clampInt("max_sessions", &c.MaxSessions, 1, 100, 3)
	clampInt("max_auth_attempts", &c.MaxAuthAttempts, 1, 1000, 5)
	// An hour of lockout is already far past the point where a stranger gives
	// up, and well past where the owner does.
	clampInt("lockout_seconds", &c.LockoutSeconds, 1, 3600, 60)
	clampInt("heartbeat_seconds", &c.HeartbeatSeconds, 1, 3600, 15)
	clampInt("disconnect_grace_seconds", &c.DisconnectGraceSeconds, 0, 3600, 30)
	clampInt("input_threshold", &c.InputThreshold, 1, 10000, 1)
	clampInt("session_ttl_minutes", &c.SessionTTLMinutes, 0, 60*24*30, 0)
	clampInt("session_idle_minutes", &c.SessionIdleMinutes, 0, 60*24*30, 0)

	if c.Location.PollSeconds != 0 {
		clampInt("location.poll_seconds", &c.Location.PollSeconds, 15, 86400, 60)
	}

	// The geolocation API key travels in this URL's query string, so a plain
	// HTTP endpoint puts it on the wire in cleartext — on whatever café Wi-Fi
	// the machine is sitting on. The phone is already refused a non-HTTPS
	// endpoint when it sets one; this is the same rule for the file, which is
	// the documented way to configure it and the path that was left open.
	//
	// The endpoint is dropped rather than corrected, so the default HTTPS one
	// takes over instead of the key going somewhere unintended.
	if c.Location.GeolocateURL != "" && !isHTTPS(c.Location.GeolocateURL) {
		notes = append(notes, fmt.Sprintf(
			"location.geolocate_url was %q, which is not https — ignoring it, because the API key travels in that URL",
			c.Location.GeolocateURL))
		c.Location.GeolocateURL = ""
	}
	// The IP lookup carries no key, but it decides where the machine is reported
	// to be, and over plain HTTP anyone on the path can choose that answer.
	if c.Location.IPLookupURL != "" && !isHTTPS(c.Location.IPLookupURL) {
		notes = append(notes, fmt.Sprintf(
			"location.ip_lookup_url was %q, which is not https — ignoring it", c.Location.IPLookupURL))
		c.Location.IPLookupURL = ""
	}

	if c.ConnectionMode != "" {
		switch c.ConnectionMode {
		case "wifi", "bluetooth", "both":
		default:
			notes = append(notes, fmt.Sprintf("connection_mode was %q, which is not one of wifi, bluetooth or both — using wifi", c.ConnectionMode))
			c.ConnectionMode = "wifi"
		}
	}

	// A value nobody recognizes is cleared rather than replaced with a guess:
	// empty means "ask", and asking is a better answer than picking a language
	// for someone on the strength of a typo.
	if c.Language != "" {
		switch c.Language {
		case "tr", "en":
		default:
			notes = append(notes, fmt.Sprintf(
				"language was %q, which is not one of tr or en — asking again", c.Language))
			c.Language = ""
		}
	}

	if c.UpdateChannel != "" {
		switch c.UpdateChannel {
		case "stable", "beta":
		default:
			// A typo here must not stop a security monitor from starting, and it
			// must not silently opt someone into prereleases either.
			notes = append(notes, fmt.Sprintf("update_channel was %q, which is not one of stable or beta — using stable", c.UpdateChannel))
			c.UpdateChannel = "stable"
		}
	}

	if c.UpdateCheckHours != 0 {
		// A six-hour floor keeps a hand-edited config from hammering GitHub's
		// rate limit; a week's ceiling keeps the check meaningful.
		clampInt("update_check_hours", &c.UpdateCheckHours, 6, 24*7, DefaultUpdateCheckHours)
	}

	for i := range c.Alarm.Levels {
		level := &c.Alarm.Levels[i]
		clampInt(fmt.Sprintf("alarm.levels[%d].delay_seconds", i), &level.DelaySeconds, 0, 86400, 0)
		if level.VolumePercent != 0 {
			clampInt(fmt.Sprintf("alarm.levels[%d].volume_percent", i), &level.VolumePercent, 1, 100, 100)
		}
	}

	return notes
}

// isHTTPS reports whether raw is a well-formed https:// URL. Anything else —
// plain HTTP, a scheme-relative address, a typo — is not somewhere a secret may
// be sent.
func isHTTPS(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host != ""
}

// Save writes the config to disk, creating the directory if needed.
//
// The write goes to a temporary file which is then renamed over the real one,
// so a crash or a flat battery mid-write leaves the previous config intact
// rather than a truncated one. That matters more here than the pattern usually
// does: this program is written to survive abrupt shutdowns, and a config that
// fails to parse is moved aside and replaced with defaults — taking the PIN
// hash and the geolocation API key with it, neither of which is recoverable
// from anywhere else. The state store and the update ledger already write this
// way; this is the file with the most to lose.
func Save(cfg *Config) error {
	dir := ConfigDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, "config.json.*")
	if err != nil {
		return fmt.Errorf("create temp config: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		// Only fires on the failure paths; a successful call has already
		// renamed the file away by the time it gets here.
		_ = os.Remove(tmpName)
	}()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp config: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp config: %w", err)
	}
	// Without the sync, a power loss just after the rename can leave the
	// directory entry pointing at a file whose contents never reached the disk.
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temp config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp config: %w", err)
	}
	return os.Rename(tmpName, ConfigPath())
}
