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
	if h.cfg != nil && audio.ValidTone(h.cfg.Alarm.Tone) {
		return h.cfg.Alarm.Tone
	}
	return "original"
}

var saveAlarmSoundConfig = config.Save

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
			copy := *h.cfg
			copy.Alarm.Tone = msg.Tone
			// Serialize this short atomic file write with arming: success must
			// mean both persisted and active, before protection can begin.
			if err := saveAlarmSoundConfig(&copy); err != nil {
				reason = "save_failed"
			} else {
				h.cfg.Alarm.Tone = msg.Tone
			}
		}
	}
	reply := ServerMessage{Type: "alarm_sound", Tone: h.alarmToneLocked(), RequestID: msg.RequestID, Reason: reason}
	h.mu.Unlock()
	client.send(reply)
}
