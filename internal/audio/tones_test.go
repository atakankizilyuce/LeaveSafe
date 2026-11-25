package audio

import (
	"encoding/binary"
	"errors"
	"testing"
)

type toneDevice struct {
	samples []int16
	stop    chan struct{}
	closed  bool
	failure error
}

func (d *toneDevice) Write(p []int16) error {
	d.samples = append(d.samples, p...)
	if len(d.samples) >= 44100*10 {
		close(d.stop)
	}
	return d.failure
}
func (d *toneDevice) Close() error { d.closed = true; return nil }

func TestApprovedTonesLoopSampleForSampleAndStop(t *testing.T) {
	old := openOutputFn
	t.Cleanup(func() { openOutputFn = old })
	for tone, file := range toneFiles {
		t.Run(tone, func(t *testing.T) {
			assertToneLoops(t, tone, file)
		})
	}
}

func assertToneLoops(t *testing.T, tone, file string) {
	t.Helper()
	wav, err := tones.ReadFile("tones/" + file)
	if err != nil {
		t.Fatal(err)
	}
	pcm, err := tonePCM(wav)
	if err != nil {
		t.Fatal(err)
	}
	d := &toneDevice{stop: make(chan struct{})}
	openOutputFn = func(rate int) (Device, error) {
		if rate != 44100 {
			t.Fatal(rate)
		}
		return d, nil
	}
	if err := PlayTone(tone, d.stop); err != nil {
		t.Fatal(err)
	}
	if !d.closed {
		t.Fatal("device left open")
	}
	for i, v := range d.samples {
		at := (i * 2) % len(pcm)
		if v != int16(binary.LittleEndian.Uint16(pcm[at:at+2])) {
			t.Fatalf("sample %d differs", i)
		}
	}
}

func TestToneFailuresAndValidation(t *testing.T) {
	old := openOutputFn
	t.Cleanup(func() { openOutputFn = old })
	for _, tone := range []string{"original", "bells", "beep"} {
		if !ValidTone(tone) {
			t.Fatal(tone)
		}
	}
	if ValidTone("../../file.wav") || ValidTone("") {
		t.Fatal("invalid tone accepted")
	}
	for _, tone := range []string{"original", "unknown", "bells"} {
		openOutputFn = func(int) (Device, error) { return nil, ErrNoOutput }
		if !errors.Is(PlayTone(tone, make(chan struct{})), ErrNoOutput) {
			t.Fatal("output error lost")
		}
	}
	d := &toneDevice{stop: make(chan struct{}), failure: ErrNoOutput}
	openOutputFn = func(int) (Device, error) { return d, nil }
	if !errors.Is(PlayTone("beep", d.stop), ErrNoOutput) || !d.closed {
		t.Fatal("write error or close lost")
	}
	wav, _ := tones.ReadFile("tones/06-calm-beep.wav")
	for _, at := range []int{0, 8, 16, 20, 22, 24, 34, 36, 40} {
		bad := append([]byte(nil), wav...)
		bad[at] ^= 0xff
		if _, err := tonePCM(bad); err == nil {
			t.Fatalf("bad field %d accepted", at)
		}
	}
	if _, err := tonePCM(nil); err == nil {
		t.Fatal("empty file accepted")
	}
}
