package ws

import (
	"github.com/leavesafe/leavesafe/internal/audio"
	"github.com/leavesafe/leavesafe/internal/config"
)

// AlarmTone is read by the alarm at playback start, not cached at daemon launch.
func (h *Hub) AlarmTone() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.alarmToneLocked()
}

func (h *Hub) alarmToneLocked() string {
	if audio.ValidTone(h.alarmTone) {
		return h.alarmTone
	}
	return "original"
}

// SetAlarmSound restores the independent device sound preference at startup.
func (h *Hub) SetAlarmSound(tone string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.alarmTone = tone
}

var saveAlarmSoundConfig = config.SaveAlarmSound

// The paired command changes only sound, with no sensor or PIN side effects.
func (h *Hub) handleAlarmSound(msg ClientMessage, client *Client) {
	h.mu.Lock()
	reason := ""
	if msg.Type == "set_alarm_sound" {
		switch {
		case h.armed || h.alarmActive:
			reason = "armed"
		case h.cfg == nil || !audio.ValidTone(msg.Tone):
			reason = "invalid"
		default:
			// Serialize this short atomic file write with arming: success must
			// mean both persisted and active, before protection can begin.
			if err := saveAlarmSoundConfig(msg.Tone); err != nil {
				reason = "save_failed"
			} else {
				h.alarmTone = msg.Tone
			}
		}
	}
	reply := ServerMessage{Type: "alarm_sound", Tone: h.alarmToneLocked(), RequestID: msg.RequestID, Reason: reason}
	h.mu.Unlock()
	client.send(reply)
}
