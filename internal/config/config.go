package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
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
	Port                   int             `json:"port"`
	MaxSessions            int             `json:"max_sessions"`
	MaxAuthAttempts        int             `json:"max_auth_attempts"`
	LockoutSeconds         int             `json:"lockout_seconds"`
	HeartbeatSeconds       int             `json:"heartbeat_seconds"`
	DisconnectGraceSeconds int             `json:"disconnect_grace_seconds"`
	AutoArmOnLock          bool            `json:"auto_arm_on_lock"`
	InputThreshold         int             `json:"input_threshold"`
	Alarm                  AlarmConfig     `json:"alarm"`
	PinProtection          PinProtection   `json:"pin_protection"`
	ConnectionMode         string          `json:"connection_mode,omitempty"`
	EnabledSensors         map[string]bool `json:"enabled_sensors,omitempty"`
	RemoteAccess           *bool           `json:"remote_access,omitempty"`
	RemotePort             int             `json:"remote_port,omitempty"`
	Location               Location        `json:"location"`
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
		AutoArmOnLock:          false,
		InputThreshold:         1,
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
func Load() (*Config, error) {
	cfg := Default()

	data, err := os.ReadFile(ConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return Default(), err
	}

	return cfg, nil
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
