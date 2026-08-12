//go:build !windows

package audio

// openOutput has nowhere to send sound on this platform yet.
//
// macOS and Linux each want a different backend — an audio queue on one, a
// sound server or ALSA on the other — and they are their own change. Until then
// the alarm hears this, says so, and falls back to the noise it was making
// before, which is the terminal bell and is not enough. That is a stated gap
// rather than a hidden one.
func openOutput(int) (Device, error) {
	return nil, ErrNoOutput
}
