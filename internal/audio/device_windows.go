//go:build windows

// The Windows audio output, and nothing else.
//
// It is a file of its own, and named in sonar-project.properties as excluded
// from coverage, because no test can reach it: every line here is a call into
// winmm against a real sound card, and the machines that run this suite have
// none — waveOutOpen on a CI runner fails at the first line and every line
// after it is unreachable. Running it where there is a sound card would mean a
// test suite that shrieks at full volume.
//
// What can be decided rather than called is decided elsewhere: the waveform is
// arithmetic in siren.go, the byte layout is pcmBytes in play.go, and what
// happens when this returns an error is in the alarm. All three are covered to
// the line. Excluded from coverage only, not from analysis — every rule still
// reads this file.

package audio

import (
	"fmt"
	"runtime"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

var (
	winmm                     = syscall.NewLazyDLL("winmm.dll")
	procWaveOutOpen           = winmm.NewProc("waveOutOpen")
	procWaveOutPrepareHeader  = winmm.NewProc("waveOutPrepareHeader")
	procWaveOutWrite          = winmm.NewProc("waveOutWrite")
	procWaveOutUnprepare      = winmm.NewProc("waveOutUnprepareHeader")
	procWaveOutReset          = winmm.NewProc("waveOutReset")
	procWaveOutClose          = winmm.NewProc("waveOutClose")
	procWaveOutGetErrorTextFn = winmm.NewProc("waveOutGetErrorTextW")
)

const (
	waveFormatPCM = 1
	waveMapper    = 0xFFFFFFFF
	callbackNull  = 0

	whdrDone     = 0x00000001
	whdrPrepared = 0x00000002

	// buffersInFlight is how many handovers the device holds at once. Two would
	// leave nothing playing while the next is being prepared, which is heard as
	// a gap; more than three only adds to how long the noise carries on after
	// the alarm is dismissed.
	buffersInFlight = 3

	// handoverWait is how long a buffer is given to come back before the device
	// is called dead. A buffer is twenty milliseconds of sound, so a second is
	// fifty of them: long past anything a busy machine explains, and short
	// enough that the alarm can fall back to another noise while it still
	// matters.
	handoverWait = time.Second
)

// waveFormatEx is WAVEFORMATEX: what the samples handed over mean.
type waveFormatEx struct {
	FormatTag      uint16
	Channels       uint16
	SamplesPerSec  uint32
	AvgBytesPerSec uint32
	BlockAlign     uint16
	BitsPerSample  uint16
	Size           uint16
}

// waveHdr is WAVEHDR: one buffer, and what the driver says about it.
type waveHdr struct {
	Data          uintptr
	BufferLength  uint32
	BytesRecorded uint32
	User          uintptr
	Flags         uint32
	Loops         uint32
	Next          uintptr
	Reserved      uintptr
}

// waveOut is the machine's audio output, with a few buffers in rotation.
type waveOut struct {
	handle uintptr
	hdrs   []*waveHdr
	bufs   [][]byte
	next   int

	// pinner holds the buffers and headers still. The driver keeps the pointers
	// it is given and writes to them from its own thread, long after the call
	// that handed them over has returned — so the garbage collector has to be
	// told they are spoken for.
	pinner runtime.Pinner
}

// openOutput opens the default audio device for the siren.
func openOutput(rate int) (Device, error) {
	format := waveFormatEx{
		FormatTag:      waveFormatPCM,
		Channels:       1,
		SamplesPerSec:  uint32(rate),     //nolint:gosec // a sample rate, and this one is a constant
		AvgBytesPerSec: uint32(rate * 2), //nolint:gosec // two bytes a sample at that rate
		BlockAlign:     2,
		BitsPerSample:  16,
	}

	d := &waveOut{}
	ret, _, _ := procWaveOutOpen.Call(
		uintptr(unsafe.Pointer(&d.handle)),
		waveMapper,
		uintptr(unsafe.Pointer(&format)),
		callbackNull, 0, callbackNull,
	)
	if ret != 0 {
		return nil, fmt.Errorf("%w: waveOutOpen: %s", ErrNoOutput, mmErrorText(ret))
	}

	for range buffersInFlight {
		buf := make([]byte, bufferSamples*2)
		hdr := &waveHdr{}
		d.pinner.Pin(&buf[0])
		d.pinner.Pin(hdr)
		d.bufs = append(d.bufs, buf)
		d.hdrs = append(d.hdrs, hdr)
	}
	return d, nil
}

// Write hands one buffer to the device, waiting first for the buffer it is
// about to reuse to come back.
func (d *waveOut) Write(samples []int16) error {
	hdr, buf := d.hdrs[d.next], d.bufs[d.next]
	d.next = (d.next + 1) % len(d.hdrs)

	if err := d.reclaim(hdr); err != nil {
		return err
	}

	pcmBytes(samples, buf)
	hdr.Data = uintptr(unsafe.Pointer(&buf[0]))
	hdr.BufferLength = uint32(len(samples) * 2) //nolint:gosec // one buffer, twenty milliseconds long
	hdr.Flags = 0

	if ret, _, _ := procWaveOutPrepareHeader.Call(d.handle,
		uintptr(unsafe.Pointer(hdr)), unsafe.Sizeof(*hdr)); ret != 0 {
		return fmt.Errorf("waveOutPrepareHeader: %s", mmErrorText(ret))
	}
	if ret, _, _ := procWaveOutWrite.Call(d.handle,
		uintptr(unsafe.Pointer(hdr)), unsafe.Sizeof(*hdr)); ret != 0 {
		return fmt.Errorf("waveOutWrite: %s", mmErrorText(ret))
	}
	return nil
}

// reclaim waits for a header the device still has, and takes it back.
//
// This is also what paces the siren: the generator can produce sound far faster
// than a speaker can play it, and without waiting here it would queue hours of
// it in seconds.
func (d *waveOut) reclaim(hdr *waveHdr) error {
	if !prepared(hdr) {
		return nil
	}

	deadline := time.Now().Add(handoverWait)
	for !done(hdr) {
		if time.Now().After(deadline) {
			return fmt.Errorf("the audio device kept a buffer for %s", handoverWait)
		}
		time.Sleep(time.Millisecond)
	}

	if ret, _, _ := procWaveOutUnprepare.Call(d.handle,
		uintptr(unsafe.Pointer(hdr)), unsafe.Sizeof(*hdr)); ret != 0 {
		return fmt.Errorf("waveOutUnprepareHeader: %s", mmErrorText(ret))
	}
	return nil
}

// The two flags the driver sets, read atomically because it sets them from its
// own thread while this one is looking at them.
func prepared(hdr *waveHdr) bool { return flags(hdr)&whdrPrepared != 0 }
func done(hdr *waveHdr) bool     { return flags(hdr)&whdrDone != 0 }

func flags(hdr *waveHdr) uint32 {
	return atomic.LoadUint32((*uint32)(unsafe.Pointer(&hdr.Flags)))
}

// Close stops the sound and gives the device back.
//
// waveOutReset first, because it is what makes the noise stop now: closing
// without it plays out everything already queued, which is up to three buffers
// of siren after the alarm was dismissed. Every step is attempted whatever the
// last one said — a device that refuses one call still has to be let go of.
func (d *waveOut) Close() error {
	_, _, _ = procWaveOutReset.Call(d.handle)

	for _, hdr := range d.hdrs {
		if prepared(hdr) {
			_, _, _ = procWaveOutUnprepare.Call(d.handle, uintptr(unsafe.Pointer(hdr)), unsafe.Sizeof(*hdr))
		}
	}

	ret, _, _ := procWaveOutClose.Call(d.handle)
	d.pinner.Unpin()
	if ret != 0 {
		return fmt.Errorf("waveOutClose: %s", mmErrorText(ret))
	}
	return nil
}

// mmErrorText is what winmm says about a code of its own, so a failure reads as
// "no audio device is installed" rather than as the number 6.
func mmErrorText(code uintptr) string {
	var text [256]uint16
	if ret, _, _ := procWaveOutGetErrorTextFn.Call(code,
		uintptr(unsafe.Pointer(&text[0])), uintptr(len(text))); ret != 0 {
		return fmt.Sprintf("error %d", code)
	}
	return syscall.UTF16ToString(text[:])
}
