//go:build !windows

package monitor

// No documented built-in accelerometer API has been established for macOS.
// Do not use private SPU protocols, elevated helpers, or input as a substitute.
func openMotionReader() (motionReader, error) { return nil, errMotionUnavailable }
