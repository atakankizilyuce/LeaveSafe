package ws

import (
	"errors"
	"github.com/leavesafe/leavesafe/internal/config"
	"testing"
)

func TestAlarmSoundPersistenceAndRefusals(t *testing.T) {
	old := saveAlarmSoundConfig
	t.Cleanup(func() { saveAlarmSoundConfig = old })
	for _, mode := range []string{"save", "get", "armed", "active", "invalid", "disk", "no_config"} {
		t.Run(mode, func(t *testing.T) {
			h, _ := hubWithSensors(t, "power")
			cfg := config.Default()
			if mode != "no_config" {
				h.SetConfig(cfg)
			}
			h.armed = mode == "armed"
			h.alarmActive = mode == "active"
			rec := &alertRecorder{}
			client := &Client{hub: h, transport: rec, authenticated: true}
			writes := 0
			saveAlarmSoundConfig = func(c *config.Config) error {
				writes++
				if mode == "disk" {
					return errors.New("full")
				}
				if c.Alarm.Tone != "beep" {
					t.Fatal(c.Alarm.Tone)
				}
				return nil
			}
			msg := ClientMessage{Type: "set_alarm_sound", Tone: "beep", RequestID: "one"}
			if mode == "get" {
				msg.Type = "get_alarm_sound"
			}
			if mode == "invalid" {
				msg.Tone = "../../file"
			}
			h.handlePairedMessage(client, msg)
			reply, ok := rec.saw("alarm_sound")
			if !ok || reply.RequestID != "one" {
				t.Fatal("missing correlated response")
			}
			if mode == "save" {
				if reply.Tone != "beep" || reply.Reason != "" || writes != 1 || h.AlarmTone() != "beep" {
					t.Fatal(reply)
				}
			} else {
				if reply.Tone != "original" || h.AlarmTone() != "original" {
					t.Fatal("failed write changed tone")
				}
				if mode != "get" && reply.Reason == "" {
					t.Fatal("refusal not reported")
				}
				if mode != "disk" && writes != 0 {
					t.Fatal("refused request wrote config")
				}
			}
		})
	}
}

func TestLegacyConfigCannotOverwriteSound(t *testing.T) {
	cfg := config.Default()
	cfg.Alarm.Tone = "bells"
	p := configToPayload(config.Default())
	p.Alarm.Tone = "beep"
	applyServerSettings(cfg, &p)
	if cfg.Alarm.Tone != "bells" {
		t.Fatal("full config bypassed sound guard")
	}
}
