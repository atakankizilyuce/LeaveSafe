//go:build linux

// The Linux audio output, and nothing else.
//
// It is a file of its own, and named in sonar-project.properties as excluded
// from coverage, for the same reason as its Windows counterpart: every line
// here is a call into libasound against a real sound card, and a machine
// running this suite either has none — in which case snd_pcm_open fails at the
// first line and every line after it is unreachable — or has one, in which case
// the test suite shrieks at full volume.
//
// ALSA rather than a sound server, because it is the layer every Linux desktop
// has underneath whichever server it runs: PulseAudio and PipeWire both install
// a plugin that makes the "default" device theirs, so this reaches the mixer the
// user is actually listening to without this file having to know which one it
// is. It is also the library the volume control already loads, so a machine that
// can be turned up is a machine that can be played to.
//
// What can be decided rather than called is decided elsewhere: the waveform is
// arithmetic in siren.go, the byte layout is pcmBytes in play.go, and what
// happens when this returns an error is in the alarm. All three are covered to
// the line. Excluded from coverage only, not from analysis — every rule still
// reads this file.

package audio

import (
	"fmt"
	"unsafe"

	"github.com/ebitengine/purego"
)

var (
	sndPCMOpen      uintptr
	sndPCMSetParams uintptr
	sndPCMWritei    uintptr
	sndPCMRecover   uintptr
	sndPCMDrop      uintptr
	sndPCMClose     uintptr
	sndStrerror     uintptr

	alsaLoaded bool
)

func init() {
	lib, err := purego.Dlopen("libasound.so.2", purego.RTLD_LAZY)
	if err != nil {
		return
	}

	// Every symbol, or none of them. A missing one would leave a zero here, and
	// calling through a zero is not an error the alarm could report — it is the
	// process dying with the machine unguarded.
	loaded := true
	for _, sym := range []struct {
		into *uintptr
		name string
	}{
		{&sndPCMOpen, "snd_pcm_open"},
		{&sndPCMSetParams, "snd_pcm_set_params"},
		{&sndPCMWritei, "snd_pcm_writei"},
		{&sndPCMRecover, "snd_pcm_recover"},
		{&sndPCMDrop, "snd_pcm_drop"},
		{&sndPCMClose, "snd_pcm_close"},
	} {
		addr, err := purego.Dlsym(lib, sym.name)
		if err != nil || addr == 0 {
			loaded = false
			break
		}
		*sym.into = addr
	}

	// This one is only used to make a failure readable, so its absence is not
	// worth refusing to play over.
	sndStrerror, _ = purego.Dlsym(lib, "snd_strerror")

	alsaLoaded = loaded
}

const (
	// The three things snd_pcm_set_params is told about the sound being handed
	// to it: signed 16-bit little-endian samples, written a buffer at a time
	// rather than mapped, into the playback direction.
	pcmPlayback            = 0
	pcmFormatS16LE         = 2
	pcmAccessRWInterleaved = 3

	// resampleAllowed lets ALSA convert if the device does not do 44.1 kHz
	// itself. A siren is worth resampling; refusing to play because the card
	// only does 48 kHz is not.
	resampleAllowed = 1

	// latencyUs is how much sound ALSA is asked to keep buffered. It is the
	// delay between the alarm being dismissed and the room going quiet, so it
	// is short — and snd_pcm_drop on the way out cuts even that.
	latencyUs = 100000

	// recoveries is how many times a broken stream is put back together before
	// this gives up and lets the alarm fall back to another noise. An underrun
	// is ordinary — the machine was busy — and recovering from it is what every
	// ALSA program does. Recovering from it five times in a row is a device
	// that is not going to play.
	recoveries = 5
)

// alsaOut is a PCM stream on the machine's default device.
type alsaOut struct {
	handle uintptr
	buf    []byte
}

// openOutput opens the default ALSA device for the siren.
func openOutput(rate int) (Device, error) {
	if !alsaLoaded {
		return nil, fmt.Errorf("%w: libasound.so.2 could not be loaded", ErrNoOutput)
	}

	device := []byte("default\x00")
	var handle uintptr
	ret, _, _ := purego.SyscallN(sndPCMOpen,
		uintptr(unsafe.Pointer(&handle)),
		uintptr(unsafe.Pointer(&device[0])),
		pcmPlayback, 0,
	)
	if code := alsaCode(ret); code < 0 {
		return nil, fmt.Errorf("%w: snd_pcm_open: %s", ErrNoOutput, alsaError(code))
	}

	ret, _, _ = purego.SyscallN(sndPCMSetParams, handle,
		pcmFormatS16LE, pcmAccessRWInterleaved,
		1, uintptr(rate), resampleAllowed, latencyUs,
	)
	if code := alsaCode(ret); code < 0 {
		_, _, _ = purego.SyscallN(sndPCMClose, handle)
		return nil, fmt.Errorf("%w: snd_pcm_set_params: %s", ErrNoOutput, alsaError(code))
	}

	return &alsaOut{handle: handle, buf: make([]byte, bufferSamples*2)}, nil
}

// Write plays one buffer, blocking until the device has taken all of it.
//
// The blocking is what paces the whole thing: the generator can produce sound
// far faster than a speaker can play it, and snd_pcm_writei returning only when
// there is room for more is what keeps the two in step.
func (d *alsaOut) Write(samples []int16) error {
	pcm := pcmBytes(samples, d.buf)

	// A short write is ordinary — a signal arrived, or the device took what it
	// had room for — so the rest is offered again rather than dropped.
	for at, left := 0, recoveries; at < len(samples); {
		ret, _, _ := purego.SyscallN(sndPCMWritei, d.handle,
			uintptr(unsafe.Pointer(&pcm[at*2])), uintptr(len(samples)-at))

		// This one alone is not truncated: snd_pcm_writei answers with a
		// frame count, which is a C long, so the whole register is the answer.
		written := int(ret)
		if written > 0 {
			at += written
			continue
		}

		// Nothing written counts against the same budget as an error. A
		// blocking device does not answer zero, but looping on it if one did
		// would spin a core for as long as the alarm lasted rather than falling
		// back to a noise that works.
		left--
		if left == 0 {
			return fmt.Errorf("the audio device took %d of %d frames and stopped: %s",
				at, len(samples), alsaError(int32(written))) //nolint:gosec // an errno, which fits
		}
		if err := d.recoverFrom(int32(written)); err != nil { //nolint:gosec // an errno, which fits
			return err
		}
	}
	return nil
}

// recoverFrom puts the stream back together after an underrun or a suspend,
// which is what snd_pcm_recover is for and what every ALSA program does.
func (d *alsaOut) recoverFrom(code int32) error {
	const silent = 1
	//nolint:gosec // an errno on its way back to the library that issued it
	ret, _, _ := purego.SyscallN(sndPCMRecover, d.handle, uintptr(code), silent)
	if r := alsaCode(ret); r < 0 {
		return fmt.Errorf("the audio stream could not be recovered: %s", alsaError(r))
	}
	return nil
}

// Close stops the sound and gives the device back.
//
// snd_pcm_drop first, because it is what makes the noise stop now: closing
// without it plays out everything already buffered, which is a tenth of a
// second of siren after the alarm was dismissed.
func (d *alsaOut) Close() error {
	_, _, _ = purego.SyscallN(sndPCMDrop, d.handle)

	ret, _, _ := purego.SyscallN(sndPCMClose, d.handle)
	if code := alsaCode(ret); code < 0 {
		return fmt.Errorf("snd_pcm_close: %s", alsaError(code))
	}
	return nil
}

// alsaCode is what an ALSA call answered.
//
// Every one of these but snd_pcm_writei returns a C int, which arrives in the
// low half of a pointer-sized register with the top half left as it was. Read
// the whole register and -2 becomes four billion — a failure that tests as a
// success, which is how this went from returning an error to calling the next
// function with a null handle and taking the process down with it.
func alsaCode(ret uintptr) int32 {
	return int32(ret) //nolint:gosec // reading the low half is what makes the sign mean anything
}

// alsaError is what ALSA says about a code of its own, so a failure reads as
// "No such file or directory" rather than as the number -2.
func alsaError(code int32) string {
	if sndStrerror == 0 {
		return fmt.Sprintf("error %d", code)
	}
	ptr, _, _ := purego.SyscallN(sndStrerror, uintptr(code)) //nolint:gosec // an errno on its way back to the library that issued it
	if ptr == 0 {
		return fmt.Sprintf("error %d", code)
	}

	// libasound hands back a raw C string address through purego, so the
	// pointer has to be reconstituted from a uintptr. go vet's unsafeptr check
	// cannot tell that the referenced memory is a string constant inside the
	// library and outlives everything here.
	base := unsafe.Pointer(ptr) //nolint:govet // FFI boundary, not Go-managed memory

	const most = 256
	length := 0
	for length < most && *(*byte)(unsafe.Add(base, length)) != 0 {
		length++
	}
	return string(unsafe.Slice((*byte)(base), length))
}
