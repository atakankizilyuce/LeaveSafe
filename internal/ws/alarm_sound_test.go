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
			checkAlarmSoundCase(t, mode)
		})
	}
}

func checkAlarmSoundCase(t *testing.T, mode string) {
	t.Helper()
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
	saveAlarmSoundConfig = func(tone string) error {
		writes++
		if mode == "disk" {
			return errors.New("full")
		}
		if tone != "beep" {
			t.Fatal(tone)
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
	assertSoundOutcome(t, mode, reply, h.AlarmTone(), writes)
}

func assertSoundOutcome(t *testing.T, mode string, reply ServerMessage, tone string, writes int) {
	t.Helper()
	if mode == "save" {
		if reply.Tone != "beep" || reply.Reason != "" || writes != 1 || tone != "beep" {
			t.Fatal(reply)
		}
	} else {
		if reply.Tone != "original" || tone != "original" {
			t.Fatal("failed write changed tone")
		}
		if mode != "get" && reply.Reason == "" {
			t.Fatal("refusal not reported")
		}
		if mode != "disk" && writes != 0 {
			t.Fatal("refused request wrote config")
		}
	}
}

func TestLegacyConfigCannotOverwriteSound(t *testing.T) {
	h, _ := hubWithSensors(t, "power")
	cfg := config.Default()
	h.SetConfig(cfg)
	h.SetAlarmSound("bells")
	p := configToPayload(config.Default())
	applyServerSettings(cfg, &p)
	if h.AlarmTone() != "bells" {
		t.Fatal("full config overwrote device sound")
	}
}
