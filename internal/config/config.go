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
	Enabled bool   `json:"enabled"`
	Pin     string `json:"pin,omitempty"`
}

// Config holds all application settings.
type Config struct {
	Port                   int            `json:"port"`
	MaxSessions            int            `json:"max_sessions"`
	MaxAuthAttempts        int            `json:"max_auth_attempts"`
	LockoutSeconds         int            `json:"lockout_seconds"`
	HeartbeatSeconds       int            `json:"heartbeat_seconds"`
	DisconnectGraceSeconds int            `json:"disconnect_grace_seconds"`
	AutoArmOnLock          bool           `json:"auto_arm_on_lock"`
	InputThreshold         int            `json:"input_threshold"`
	Alarm                  AlarmConfig    `json:"alarm"`
	PinProtection          PinProtection  `json:"pin_protection"`
	EnabledSensors         map[string]bool `json:"enabled_sensors,omitempty"`
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
		InputThreshold:         3,
		Alarm: AlarmConfig{
			EscalationEnabled: false,
			Levels: []AlarmLevel{
				{DelaySeconds: 0, Action: "notify_phone_only"},
				{DelaySeconds: 15, Action: "medium_volume", VolumePercent: 50},
				{DelaySeconds: 30, Action: "full_volume", VolumePercent: 100},
			},
		},
		PinProtection: PinProtection{
			Enabled: false,
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
