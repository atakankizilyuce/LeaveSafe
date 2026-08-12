//go:build darwin

// The macOS audio output, and nothing else.
//
// It is a file of its own, and named in sonar-project.properties as excluded
// from coverage, for the same reason as its Windows and Linux counterparts:
// every line here is a call into AudioToolbox against a real sound device, and
// a machine running this suite either has none — in which case
// AudioQueueNewOutput fails at the first line and every line after it is
// unreachable — or has one, in which case the test suite shrieks at full volume.
//
// An audio queue rather than a helper process, because a queue plays without a
// gap and a process does not: afplay can only be handed a whole file, so a siren
// made of them restarts every few seconds, and the restart is heard. It is also
// the same shape as the other two backends — a handful of buffers going round —
// which is worth more than it sounds: three backends that work the same way can
// be read against each other.
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
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
)

var (
	audioQueueNewOutput      uintptr
	audioQueueAllocateBuffer uintptr
	audioQueueEnqueueBuffer  uintptr
	audioQueueStart          uintptr
	audioQueueStop           uintptr
	audioQueueDispose        uintptr

	audioToolboxLoaded bool
)

func init() {
	lib, err := purego.Dlopen(
		"/System/Library/Frameworks/AudioToolbox.framework/AudioToolbox", purego.RTLD_LAZY)
	if err != nil {
		return
	}

	// Every symbol, or none of them. A missing one would leave a zero here, and
	// calling through a zero is not an error the alarm could report — it is the
	// process dying with the machine unguarded.
	for _, sym := range []struct {
		into *uintptr
		name string
	}{
		{&audioQueueNewOutput, "AudioQueueNewOutput"},
		{&audioQueueAllocateBuffer, "AudioQueueAllocateBuffer"},
		{&audioQueueEnqueueBuffer, "AudioQueueEnqueueBuffer"},
		{&audioQueueStart, "AudioQueueStart"},
		{&audioQueueStop, "AudioQueueStop"},
		{&audioQueueDispose, "AudioQueueDispose"},
	} {
		addr, err := purego.Dlsym(lib, sym.name)
		if err != nil || addr == 0 {
			return
		}
		*sym.into = addr
	}
	audioToolboxLoaded = true
}

const (
	// formatLinearPCM is 'lpcm', and the two flags say the samples are signed
	// integers with no padding between them — which is what pcmBytes produces.
	formatLinearPCM      = 0x6C70636D
	flagSignedInteger    = 1 << 2
	flagPacked           = 1 << 3
	linearPCMFlags       = flagSignedInteger | flagPacked
	bytesPerSample       = 2
	channelsPerFrame     = 1
	bitsPerSample        = 16
	framesPerPacketPCM   = 1
	immediately          = 1
	noAudioTimeStamp     = 0
	noPacketDescriptions = 0

	// buffersInQueue is how many handovers the queue holds at once. Two would
	// leave nothing playing while the next is being filled, which is heard as a
	// gap; more than three only adds to how long the noise carries on after the
	// alarm is dismissed.
	buffersInQueue = 3

	// handoverWaitDarwin is how long a buffer is given to come back before the
	// queue is called dead. A buffer is twenty milliseconds of sound, so a
	// second is fifty of them: long past anything a busy machine explains, and
	// short enough that the alarm can fall back to another noise while it still
	// matters.
	handoverWaitDarwin = time.Second
)

// streamDescription is AudioStreamBasicDescription: what the samples handed
// over mean.
type streamDescription struct {
	SampleRate       float64
	FormatID         uint32
	FormatFlags      uint32
	BytesPerPacket   uint32
	FramesPerPacket  uint32
	BytesPerFrame    uint32
	ChannelsPerFrame uint32
	BitsPerChannel   uint32
	Reserved         uint32
}

// queueBuffer is AudioQueueBuffer: one buffer the queue owns, and the memory it
// points at. Every field is here, including the ones this never touches,
// because the layout is what makes Data land at the right offset.
type queueBuffer struct {
	Capacity uint32
	_        uint32 // padding to the pointer that follows
	Data     uintptr
	ByteSize uint32
	_        uint32
	UserData uintptr

	PacketDescriptionCapacity uint32
	_                         uint32
	PacketDescriptions        uintptr
	PacketDescriptionCount    uint32
}

// bufferReturned is what the audio queue calls on a thread of its own when it
// has finished playing a buffer.
//
// It is created once, at package level, rather than per device: purego has a
// fixed number of callbacks a process may ever make, so building one for each
// alarm would be a program that eventually panics after enough alarms. The
// device it belongs to is not looked up at all — the counter's own address is
// handed to the queue as user data, so all this does is add one to it.
//
// It does the least a callback can do for the same reason: it runs on the audio
// thread, and anything that blocks there is heard.
var bufferReturned = purego.NewCallback(func(userData, queue, buffer uintptr) uintptr {
	// The address was pinned by openOutput and belongs to a device that has not
	// been disposed of, because AudioQueueDispose does not return until the
	// callbacks have.
	free := (*int32)(unsafe.Pointer(userData)) //nolint:govet // FFI boundary, not Go-managed memory
	atomic.AddInt32(free, 1)
	return 0
})

// audioQueueOut is the machine's audio output, with a few buffers in rotation.
type audioQueueOut struct {
	queue   uintptr
	buffers []uintptr
	next    int
	started bool

	// free counts the buffers the queue has handed back. It is written by
	// bufferReturned from the audio thread and read here, so every touch is
	// atomic — and its address is pinned, because the queue was given it.
	free int32

	pinner runtime.Pinner
}

// openOutput opens an audio queue on the default output device for the siren.
func openOutput(rate int) (Device, error) {
	if !audioToolboxLoaded {
		return nil, fmt.Errorf("%w: AudioToolbox could not be loaded", ErrNoOutput)
	}

	format := streamDescription{
		SampleRate:       float64(rate),
		FormatID:         formatLinearPCM,
		FormatFlags:      linearPCMFlags,
		BytesPerPacket:   bytesPerSample,
		FramesPerPacket:  framesPerPacketPCM,
		BytesPerFrame:    bytesPerSample,
		ChannelsPerFrame: channelsPerFrame,
		BitsPerChannel:   bitsPerSample,
	}

	d := &audioQueueOut{}
	d.pinner.Pin(&d.free)

	// A nil run loop asks the queue to call back on a thread of its own. The
	// alternative is this thread's run loop, and this thread does not have one:
	// a Go program has no CFRunLoop being pumped, so the callbacks would never
	// arrive and the siren would play three buffers and stop.
	ret, _, _ := purego.SyscallN(audioQueueNewOutput,
		uintptr(unsafe.Pointer(&format)),
		bufferReturned,
		uintptr(unsafe.Pointer(&d.free)),
		0, 0, 0,
		uintptr(unsafe.Pointer(&d.queue)),
	)
	if status := osStatus(ret); status != 0 {
		d.pinner.Unpin()
		return nil, fmt.Errorf("%w: AudioQueueNewOutput: OSStatus %d", ErrNoOutput, status)
	}

	for range buffersInQueue {
		var buffer uintptr
		ret, _, _ := purego.SyscallN(audioQueueAllocateBuffer,
			d.queue, bufferSamples*bytesPerSample, uintptr(unsafe.Pointer(&buffer)))
		if status := osStatus(ret); status != 0 {
			_ = d.Close()
			return nil, fmt.Errorf("%w: AudioQueueAllocateBuffer: OSStatus %d", ErrNoOutput, status)
		}
		d.buffers = append(d.buffers, buffer)
	}
	d.free = buffersInQueue

	return d, nil
}

// Write hands one buffer to the queue, waiting first for the buffer it is about
// to reuse to come back.
func (d *audioQueueOut) Write(samples []int16) error {
	if err := d.waitForABuffer(); err != nil {
		return err
	}

	buffer := d.buffers[d.next]
	d.next = (d.next + 1) % len(d.buffers)

	// The queue allocated this memory and told us where it is and how much of
	// it there is; pcmBytes fills it and says how far it got.
	//
	// Both conversions cross the FFI boundary, where a uintptr is the only way
	// an address arrives. go vet's unsafeptr check cannot tell that this one
	// names memory the audio queue owns and keeps until it is disposed of.
	hdr := (*queueBuffer)(unsafe.Pointer(buffer))                         //nolint:govet // memory owned by the queue
	into := unsafe.Slice((*byte)(unsafe.Pointer(hdr.Data)), hdr.Capacity) //nolint:govet // memory owned by the queue

	// pcmBytes fills what it is given and grows a new slice if it will not fit.
	// Growing here would mean the samples landing in Go memory the queue knows
	// nothing about, and a siren that reports itself playing into silence — so
	// a buffer smaller than asked for is refused rather than filled.
	if int(hdr.Capacity) < len(samples)*bytesPerSample {
		return fmt.Errorf("the audio queue gave back %d bytes for %d samples",
			hdr.Capacity, len(samples))
	}
	hdr.ByteSize = uint32(len(pcmBytes(samples, into))) //nolint:gosec // one buffer, twenty milliseconds long

	ret, _, _ := purego.SyscallN(audioQueueEnqueueBuffer,
		d.queue, buffer, noPacketDescriptions, 0)
	if status := osStatus(ret); status != 0 {
		return fmt.Errorf("AudioQueueEnqueueBuffer: OSStatus %d", status)
	}

	// Started here rather than in openOutput, so the queue never starts with
	// nothing to play.
	if !d.started {
		ret, _, _ := purego.SyscallN(audioQueueStart, d.queue, noAudioTimeStamp)
		if status := osStatus(ret); status != 0 {
			return fmt.Errorf("AudioQueueStart: OSStatus %d", status)
		}
		d.started = true
	}
	return nil
}

// waitForABuffer blocks until the queue has handed one back.
//
// This is also what paces the siren: the generator can produce sound far faster
// than a speaker can play it, and without waiting here it would fill the same
// three buffers over and over while the queue was still playing them.
func (d *audioQueueOut) waitForABuffer() error {
	deadline := time.Now().Add(handoverWaitDarwin)
	for atomic.LoadInt32(&d.free) <= 0 {
		if time.Now().After(deadline) {
			return fmt.Errorf("the audio device kept every buffer for %s", handoverWaitDarwin)
		}
		time.Sleep(time.Millisecond)
	}
	atomic.AddInt32(&d.free, -1)
	return nil
}

// Close stops the sound and gives the queue back.
//
// Stopping immediately is what makes the noise stop now: letting it play out
// would sound the buffers already queued, which is up to three of them after
// the alarm was dismissed. Disposing of the queue frees its buffers with it,
// and does not return until the callbacks have — which is what makes it safe to
// unpin the counter they were writing to.
func (d *audioQueueOut) Close() error {
	_, _, _ = purego.SyscallN(audioQueueStop, d.queue, immediately)

	ret, _, _ := purego.SyscallN(audioQueueDispose, d.queue, immediately)
	d.pinner.Unpin()
	if status := osStatus(ret); status != 0 {
		return fmt.Errorf("AudioQueueDispose: OSStatus %d", status)
	}
	return nil
}

// osStatus is what a framework call answered.
//
// It comes back in a pointer-sized register and an OSStatus is a signed 32-bit
// code, so the low half of that register is the whole of it and the top half is
// whatever was there before. Reading the whole register is how a success tests
// as a failure and how -50 reads as four billion; this is the only place either
// number is turned back into the one Apple documents.
func osStatus(ret uintptr) int32 {
	return int32(ret) //nolint:gosec // reinterpreting the low half is the point
}
