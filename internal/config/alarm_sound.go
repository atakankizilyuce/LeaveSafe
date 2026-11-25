package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const alarmSoundFile = "alarm-sound.json"

// Sound is device-local, separate from full browser configuration snapshots.
// An older client saving its config cannot overwrite a concurrent sound change.
func SaveAlarmSound(tone string) error {
	data, err := json.Marshal(tone)
	if err != nil {
		return err
	}
	return saveConfigData(alarmSoundFile, data)
}

func LoadAlarmSound() string {
	data, err := os.ReadFile(filepath.Join(ConfigDir(), alarmSoundFile))
	if err != nil {
		return "original"
	}
	var tone string
	if json.Unmarshal(data, &tone) != nil {
		return "original"
	}
	return tone
}
