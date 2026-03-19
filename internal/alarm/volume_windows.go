//go:build windows

package alarm

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// PowerShell script that uses COM interop to control Windows audio volume.
// Uses the IAudioEndpointVolume interface via inline C#.
const volumeScript = `
Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;

[Guid("5CDF2C82-841E-4546-9722-0CF74078229A"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
interface IAudioEndpointVolume {
    int NotImpl1();
    int NotImpl2();
    int NotImpl3();
    int NotImpl4();
    int NotImpl5();
    int NotImpl6();
    int NotImpl7();
    int SetMasterVolumeLevelScalar(float fLevel, System.Guid pguidEventContext);
    int NotImpl8();
    int GetMasterVolumeLevelScalar(out float pfLevel);
    int NotImpl9();
    int SetMute([MarshalAs(UnmanagedType.Bool)] bool bMute, System.Guid pguidEventContext);
    int GetMute(out bool pbMute);
}

[Guid("D666063F-1587-4E43-81F1-B948E807363F"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
interface IMMDevice {
    int Activate(ref System.Guid iid, int dwClsCtx, IntPtr pActivationParams, [MarshalAs(UnmanagedType.IUnknown)] out object ppInterface);
}

[Guid("A95664D2-9614-4F35-A746-DE8DB63617E6"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
interface IMMDeviceEnumerator {
    int GetDefaultAudioEndpoint(int dataFlow, int role, out IMMDevice ppDevice);
}

[ComImport, Guid("BCDE0395-E52F-467C-8E3D-C4579291692E")]
class MMDeviceEnumerator { }

public static class AudioControl {
    public static float GetVolume() {
        var enumerator = new MMDeviceEnumerator() as IMMDeviceEnumerator;
        IMMDevice device;
        enumerator.GetDefaultAudioEndpoint(0, 1, out device);
        var iid = typeof(IAudioEndpointVolume).GUID;
        object obj;
        device.Activate(ref iid, 1, IntPtr.Zero, out obj);
        var volume = (IAudioEndpointVolume)obj;
        float level;
        volume.GetMasterVolumeLevelScalar(out level);
        return level;
    }
    public static void SetVolume(float level) {
        var enumerator = new MMDeviceEnumerator() as IMMDeviceEnumerator;
        IMMDevice device;
        enumerator.GetDefaultAudioEndpoint(0, 1, out device);
        var iid = typeof(IAudioEndpointVolume).GUID;
        object obj;
        device.Activate(ref iid, 1, IntPtr.Zero, out obj);
        var volume = (IAudioEndpointVolume)obj;
        volume.SetMasterVolumeLevelScalar(level, System.Guid.Empty);
        volume.SetMute(false, System.Guid.Empty);
    }
}
'@
`

// maxVolume saves the current volume level and sets it to 100%.
// Returns the previous volume level (0.0 - 1.0).
func maxVolume() (float64, error) {
	// Get current volume
	getCmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
		volumeScript+"\n[AudioControl]::GetVolume()")
	out, err := getCmd.Output()
	if err != nil {
		// Even if getting volume fails, try to set it
		setMax()
		return 0, fmt.Errorf("get volume: %w", err)
	}

	prev, _ := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)

	// Set volume to max
	if err := setMax(); err != nil {
		return prev, fmt.Errorf("set volume: %w", err)
	}

	return prev, nil
}

func setMax() error {
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
		volumeScript+"\n[AudioControl]::SetVolume(1.0)")
	return cmd.Run()
}

// restoreVolume sets the system volume back to the saved level.
func restoreVolume(level float64) error {
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
		fmt.Sprintf("%s\n[AudioControl]::SetVolume(%f)", volumeScript, level))
	return cmd.Run()
}
