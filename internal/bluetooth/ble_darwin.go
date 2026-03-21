//go:build darwin

package bluetooth

import (
	"context"
	"errors"
)

// Start returns an error on macOS because tinygo.org/x/bluetooth does not
// support the BLE peripheral (GATT server) role on darwin.
func (s *Server) Start(_ context.Context) error {
	return errors.New("BLE peripheral mode is not supported on macOS")
}

// Available always returns false on macOS.
func Available() bool {
	return false
}
