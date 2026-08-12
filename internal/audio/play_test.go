package audio

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// What PlaySiren owes its caller: sound until it is told to stop, an honest
// answer when the machine will not make any, and the device given back either
// way. The alarm acts on all three — it falls back to beeping when this fails,
// and a device left open is a machine that goes on shrieking after the owner
// has dismissed it.

// silentDevice is somewhere to send sound that makes none, and remembers what
// it was given.
type silentDevice struct {
	mu      sync.Mutex
	buffers int
	samples int
	closed  int

	// writeErr is the device failing part way through, the way one does when
	// the headphones it belongs to are unplugged.
	writeErr  error
	failAfter int

	wrote chan struct{}
}

func newSilentDevice() *silentDevice {
	return &silentDevice{wrote: make(chan struct{}, 1)}
}

func (d *silentDevice) Write(samples []int16) error {
	d.mu.Lock()
	d.buffers++
	d.samples += len(samples)
	err := d.writeErr
	failed := err != nil && d.buffers > d.failAfter
	d.mu.Unlock()

	select {
	case d.wrote <- struct{}{}:
	default:
	}

	if failed {
		return err
	}
	// A real device blocks here until it has room for another buffer, which is
	// what stops this generating hours of siren in seconds.
	time.Sleep(time.Millisecond)
	return nil
}

func (d *silentDevice) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed++
	return nil
}

func (d *silentDevice) counts() (buffers, samples, closed int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.buffers, d.samples, d.closed
}

// install points the package at this device for the duration of one test.
func (d *silentDevice) install(t *testing.T, openErr error) {
	t.Helper()

	previous := openOutputFn
	t.Cleanup(func() { openOutputFn = previous })

	openOutputFn = func(int) (Device, error) {
		if openErr != nil {
			return nil, openErr
		}
		return d, nil
	}
}

func TestTheSirenSoundsUntilItIsStopped(t *testing.T) {
	dev := newSilentDevice()
	dev.install(t, nil)

	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- PlaySiren(stop) }()

	<-dev.wrote
	close(stop)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("the siren stopped with an error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the siren did not stop when it was told to")
	}

	buffers, samples, closed := dev.counts()
	if buffers == 0 || samples != buffers*bufferSamples {
		t.Errorf("%d buffers carrying %d samples, want %d each", buffers, samples, bufferSamples)
	}
	// The device is given back, or the machine goes on shrieking after the
	// owner has dismissed the alarm.
	if closed != 1 {
		t.Errorf("the device was closed %d times, want once", closed)
	}
}

// A machine with no sound card has to say so rather than pretend. The alarm
// answers this by beeping instead, and an error swallowed here is a laptop
// being carried away in silence.
func TestNowhereToSendSoundIsReportedRatherThanIgnored(t *testing.T) {
	newSilentDevice().install(t, ErrNoOutput)

	err := PlaySiren(make(chan struct{}))

	if !errors.Is(err, ErrNoOutput) {
		t.Errorf("PlaySiren = %v, want it to report that there is no output", err)
	}
}

// A device that stops accepting sound half way — headphones unplugged, a driver
// that fell over — is the same situation as one that never opened: the room has
// gone quiet and something else has to make the noise.
func TestADeviceThatFailsPartWayThroughIsReported(t *testing.T) {
	dev := newSilentDevice()
	dev.writeErr = errors.New("the device went away")
	dev.failAfter = 2
	dev.install(t, nil)

	err := PlaySiren(make(chan struct{}))

	if err == nil {
		t.Fatal("a device that stopped accepting sound was reported as a siren that played")
	}
	if _, _, closed := dev.counts(); closed != 1 {
		t.Errorf("the device was closed %d times after failing, want once", closed)
	}
}

// The bytes are what the sound actually is by the time it reaches a device, and
// every backend wants them little-endian. Getting the order wrong is not a
// subtle degradation: it is noise at full volume.
func TestSamplesAreLaidOutLittleEndian(t *testing.T) {
	got := pcmBytes([]int16{0, 1, -1, 32767, -32768}, make([]byte, 10))

	want := []byte{
		0x00, 0x00, // 0
		0x01, 0x00, // 1
		0xff, 0xff, // -1
		0xff, 0x7f, // 32767
		0x00, 0x80, // -32768
	}
	if len(got) != len(want) {
		t.Fatalf("%d bytes for 5 samples, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("byte %d is %#02x, want %#02x (whole buffer %#v)", i, got[i], want[i], got)
		}
	}
}
