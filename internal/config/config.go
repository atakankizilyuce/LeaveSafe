package config

import (
	"encoding/json"
	"fmt"
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
	RestoreArmedState bool            `json:"restore_armed_state"`
	Alarm             AlarmConfig     `json:"alarm"`
	PinProtection     PinProtection   `json:"pin_protection"`
	ConnectionMode    string          `json:"connection_mode,omitempty"`
	EnabledSensors    map[string]bool `json:"enabled_sensors,omitempty"`
	RemoteAccess      *bool           `json:"remote_access,omitempty"`
	RemotePort        int             `json:"remote_port,omitempty"`
	Location          Location        `json:"location"`
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
			return Default(), fmt.Errorf("config is not valid JSON (%w), and it could not be backed up: %v", err, backupErr)
		}
		return Default(), fmt.Errorf("config is not valid JSON (%w); the previous file was kept as %s", err, backup)
	}

	return cfg, nil
}

// backupCorruptConfig renames an unparseable config out of the way and returns
// the new path.
func backupCorruptConfig(path string) (string, error) {
	backup := fmt.Sprintf("%s.corrupt-%d", path, time.Now().Unix())
	if err := os.Rename(path, backup); err != nil {
		return "", err
	}
	return backup, nil
}

// Validate clamps values that would break the program if honoured literally and
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

	if c.ConnectionMode != "" {
		switch c.ConnectionMode {
		case "wifi", "bluetooth", "both":
		default:
			notes = append(notes, fmt.Sprintf("connection_mode was %q, which is not one of wifi, bluetooth or both — using wifi", c.ConnectionMode))
			c.ConnectionMode = "wifi"
		}
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

// Save writes the config to disk, creating the directory if needed.
func Save(cfg *Config) error {
	dir := ConfigDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(ConfigPath(), data, 0o600)
}
