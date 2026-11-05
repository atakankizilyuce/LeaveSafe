package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAlarmSoundSurvivesRestartAndOtherConfigWrites(t *testing.T) {
	dir := isolate(t)
	if LoadAlarmSound() != "original" {
		t.Fatal("default changed")
	}
	for _, tone := range []string{"bells", "pulse", "beacon", "siren", "rhythm", "beep", "original"} {
		if err := SaveAlarmSound(tone); err != nil {
			t.Fatal(err)
		}
		if err := Save(Default()); err != nil {
			t.Fatal(err)
		}
		if LoadAlarmSound() != tone {
			t.Fatal("sound was not persisted independently")
		}
	}
	if err := os.WriteFile(filepath.Join(dir, alarmSoundFile), []byte("broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if LoadAlarmSound() != "original" {
		t.Fatal("corruption did not fall back")
	}
}
