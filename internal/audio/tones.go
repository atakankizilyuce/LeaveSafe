package audio

import (
	"embed"
	"encoding/binary"
	"fmt"
)

//go:embed tones/*.wav
var tones embed.FS

var toneFiles = map[string]string{
	"bells": "01-soft-bells.wav", "pulse": "02-soft-pulse.wav",
	"beacon": "03-double-beacon.wav", "siren": "04-rising-siren.wav",
	"rhythm": "05-rhythmic-alert.wav", "beep": "06-calm-beep.wav",
}

// ValidTone accepts only bundled sounds, never a caller-supplied path.
func ValidTone(tone string) bool {
	_, ok := toneFiles[tone]
	return ok || tone == "original"
}

// PlayTone loops the approved mono PCM assets through the native audio device.
// Unknown selections retain the original alarm rather than falling silent.
func PlayTone(tone string, stop <-chan struct{}) error {
	file, ok := toneFiles[tone]
	if !ok {
		return PlaySiren(stop)
	}
	wav, err := tones.ReadFile("tones/" + file)
	if err != nil {
		return PlaySiren(stop)
	}
	pcm, err := tonePCM(wav)
	if err != nil {
		return PlaySiren(stop)
	}
	dev, err := openOutputFn(sampleRate)
	if err != nil {
		return fmt.Errorf("open tone output: %w", err)
	}
	defer func() { _ = dev.Close() }()
	buf := make([]int16, bufferSamples)
	at := 0
	for {
		select {
		case <-stop:
			return nil
		default:
		}
		for i := range buf {
			buf[i] = int16(binary.LittleEndian.Uint16(pcm[at : at+2]))
			at = (at + 2) % len(pcm)
		}
		if err := dev.Write(buf); err != nil {
			return fmt.Errorf("play tone: %w", err)
		}
	}
}

func tonePCM(wav []byte) ([]byte, error) {
	// Our generated assets use a standard 44-byte PCM header. Reject a changed
	// format at this boundary rather than feeding arbitrary bytes to a speaker.
	if len(wav) <= 44 || string(wav[:4]) != "RIFF" || string(wav[8:16]) != "WAVEfmt " ||
		binary.LittleEndian.Uint32(wav[16:20]) != 16 ||
		binary.LittleEndian.Uint16(wav[20:22]) != 1 ||
		binary.LittleEndian.Uint16(wav[22:24]) != 1 ||
		binary.LittleEndian.Uint32(wav[24:28]) != sampleRate ||
		binary.LittleEndian.Uint16(wav[34:36]) != 16 || string(wav[36:40]) != "data" ||
		int(binary.LittleEndian.Uint32(wav[40:44])) != len(wav)-44 || (len(wav)-44)%2 != 0 {
		return nil, fmt.Errorf("unsupported alarm tone PCM")
	}
	return wav[44:], nil
}
