//go:build linux

package alarm

import (
	"errors"
	"fmt"
	"unsafe"

	"github.com/ebitengine/purego"
)

var (
	sndMixerOpen                        uintptr
	sndMixerClose                       uintptr
	sndMixerAttach                      uintptr
	sndMixerSelemRegister               uintptr
	sndMixerLoad                        uintptr
	sndMixerFirstElem                   uintptr
	sndMixerElemNext                    uintptr
	sndMixerSelemGetName                uintptr
	sndMixerSelemHasPlaybackVolume      uintptr
	sndMixerSelemGetPlaybackVolumeRange uintptr
	sndMixerSelemGetPlaybackVolume      uintptr
	sndMixerSelemSetPlaybackVolumeAll   uintptr
	sndMixerSelemSetPlaybackSwitchAll   uintptr

	alsaAvailable bool
)

func init() {
	lib, err := purego.Dlopen("libasound.so.2", purego.RTLD_LAZY)
	if err != nil {
		return
	}
	alsaAvailable = true

	sndMixerOpen, _ = purego.Dlsym(lib, "snd_mixer_open")
	sndMixerClose, _ = purego.Dlsym(lib, "snd_mixer_close")
	sndMixerAttach, _ = purego.Dlsym(lib, "snd_mixer_attach")
	sndMixerSelemRegister, _ = purego.Dlsym(lib, "snd_mixer_selem_register")
	sndMixerLoad, _ = purego.Dlsym(lib, "snd_mixer_load")
	sndMixerFirstElem, _ = purego.Dlsym(lib, "snd_mixer_first_elem")
	sndMixerElemNext, _ = purego.Dlsym(lib, "snd_mixer_elem_next")
	sndMixerSelemGetName, _ = purego.Dlsym(lib, "snd_mixer_selem_get_name")
	sndMixerSelemHasPlaybackVolume, _ = purego.Dlsym(lib, "snd_mixer_selem_has_playback_volume")
	sndMixerSelemGetPlaybackVolumeRange, _ = purego.Dlsym(lib, "snd_mixer_selem_get_playback_volume_range")
	sndMixerSelemGetPlaybackVolume, _ = purego.Dlsym(lib, "snd_mixer_selem_get_playback_volume")
	sndMixerSelemSetPlaybackVolumeAll, _ = purego.Dlsym(lib, "snd_mixer_selem_set_playback_volume_all")
	sndMixerSelemSetPlaybackSwitchAll, _ = purego.Dlsym(lib, "snd_mixer_selem_set_playback_switch_all")
}

// openMasterMixer opens ALSA, finds the Master element, and returns it with a cleanup function.
func openMasterMixer() (elem uintptr, cleanup func(), err error) {
	if !alsaAvailable {
		return 0, nil, fmt.Errorf("ALSA not available")
	}

	var mixer uintptr
	ret, _, _ := purego.SyscallN(sndMixerOpen, uintptr(unsafe.Pointer(&mixer)), 0)
	if int32(ret) < 0 {
		return 0, nil, fmt.Errorf("snd_mixer_open failed: %d", int32(ret))
	}

	card := []byte("default\x00")
	ret, _, _ = purego.SyscallN(sndMixerAttach, mixer, uintptr(unsafe.Pointer(&card[0])))
	if int32(ret) < 0 {
		purego.SyscallN(sndMixerClose, mixer)
		return 0, nil, fmt.Errorf("snd_mixer_attach failed: %d", int32(ret))
	}

	purego.SyscallN(sndMixerSelemRegister, mixer, 0, 0)
	purego.SyscallN(sndMixerLoad, mixer)

	e := findMasterElem(mixer)
	if e == 0 {
		purego.SyscallN(sndMixerClose, mixer)
		return 0, nil, errors.New("master mixer element not found")
	}

	return e, func() { purego.SyscallN(sndMixerClose, mixer) }, nil
}

func maxVolume() (float64, error) {
	elem, cleanup, err := openMasterMixer()
	if err != nil {
		return 0, err
	}
	defer cleanup()

	var minVol, maxVol int64
	purego.SyscallN(sndMixerSelemGetPlaybackVolumeRange, elem,
		uintptr(unsafe.Pointer(&minVol)), uintptr(unsafe.Pointer(&maxVol)))
	if maxVol <= minVol {
		return 0, fmt.Errorf("invalid volume range")
	}

	var currentVol int64
	purego.SyscallN(sndMixerSelemGetPlaybackVolume, elem, 0, uintptr(unsafe.Pointer(&currentVol)))
	prevLevel := float64(currentVol-minVol) / float64(maxVol-minVol)

	purego.SyscallN(sndMixerSelemSetPlaybackVolumeAll, elem, uintptr(maxVol))
	purego.SyscallN(sndMixerSelemSetPlaybackSwitchAll, elem, 1)

	return prevLevel, nil
}

func setVolume(level float64) (float64, error) {
	elem, cleanup, err := openMasterMixer()
	if err != nil {
		return 0, err
	}
	defer cleanup()

	var minVol, maxVol int64
	purego.SyscallN(sndMixerSelemGetPlaybackVolumeRange, elem,
		uintptr(unsafe.Pointer(&minVol)), uintptr(unsafe.Pointer(&maxVol)))
	if maxVol <= minVol {
		return 0, fmt.Errorf("invalid volume range")
	}

	var currentVol int64
	purego.SyscallN(sndMixerSelemGetPlaybackVolume, elem, 0, uintptr(unsafe.Pointer(&currentVol)))
	prevLevel := float64(currentVol-minVol) / float64(maxVol-minVol)

	targetVol := int64(clampLevel(level)*float64(maxVol-minVol)) + minVol
	purego.SyscallN(sndMixerSelemSetPlaybackVolumeAll, elem, uintptr(targetVol))
	// SetPlaybackSwitchAll(1) unmutes every channel of the element. Both this and
	// SetPlaybackVolumeAll apply to all channels alike, so neither one can leave
	// the user's left/right balance lopsided.
	purego.SyscallN(sndMixerSelemSetPlaybackSwitchAll, elem, 1)

	return prevLevel, nil
}

func restoreVolume(level float64) error {
	elem, cleanup, err := openMasterMixer()
	if err != nil {
		return err
	}
	defer cleanup()

	var minVol, maxVol int64
	purego.SyscallN(sndMixerSelemGetPlaybackVolumeRange, elem,
		uintptr(unsafe.Pointer(&minVol)), uintptr(unsafe.Pointer(&maxVol)))

	if maxVol <= minVol {
		return fmt.Errorf("invalid volume range")
	}

	targetVol := int64(clampLevel(level)*float64(maxVol-minVol)) + minVol
	purego.SyscallN(sndMixerSelemSetPlaybackVolumeAll, elem, uintptr(targetVol))
	return nil
}

func findMasterElem(mixer uintptr) uintptr {
	elem, _, _ := purego.SyscallN(sndMixerFirstElem, mixer)
	var fallback uintptr

	for elem != 0 {
		hasVol, _, _ := purego.SyscallN(sndMixerSelemHasPlaybackVolume, elem)
		if hasVol != 0 {
			namePtr, _, _ := purego.SyscallN(sndMixerSelemGetName, elem)
			if namePtr != 0 {
				name := goString(namePtr)
				if name == "Master" {
					return elem
				}
				if fallback == 0 {
					fallback = elem
				}
			}
		}
		elem, _, _ = purego.SyscallN(sndMixerElemNext, elem)
	}
	return fallback
}

func goString(ptr uintptr) string {
	if ptr == 0 {
		return ""
	}
	// ALSA hands back a raw C string address through purego, so the pointer has
	// to be reconstituted from a uintptr. go vet's unsafeptr check cannot tell
	// that the referenced memory is owned by libasound and stays alive.
	base := unsafe.Pointer(ptr) //nolint:govet // FFI boundary, not Go-managed memory

	const maxLen = 256
	var length int
	for length < maxLen && *(*byte)(unsafe.Add(base, length)) != 0 {
		length++
	}
	return string(unsafe.Slice((*byte)(base), length))
}
