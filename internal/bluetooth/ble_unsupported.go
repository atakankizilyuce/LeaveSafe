//go:build !darwin

package bluetooth

import (
	"context"

	"github.com/leavesafe/leavesafe/internal/ws"
)

// Server is what remains of the BLE server where the transport is refused: the
// hub it would have fed, and nothing else. There are no clients to track
// because none is ever accepted — see ErrNoCentralIdentity.
type Server struct {
	hub *ws.Hub
}

// NewServer creates a new BLE server.
func NewServer(hub *ws.Hub) *Server {
	return &Server{hub: hub}
}

// Start refuses to advertise.
//
// Neither the BlueZ nor the WinRT backend tells the GATT layer which central
// performed a write — tinygo's bluetooth package says so outright for BlueZ and
// leaves a comment where Windows should report it, and passes a connection ID
// of zero on both.
// With no way to tell two phones apart, one authenticated session would cover
// every device in radio range. See ErrNoCentralIdentity.
//
// Restoring this means the stack reporting a real per-connection identity that
// reaches WriteEvent; the backend that advertised is in the history next to
// this. Until then Wi-Fi is the transport, and it runs whatever
// connection_mode says.
func (s *Server) Start(_ context.Context) error {
	return ErrNoCentralIdentity
}

// Available reports whether BLE can be offered on this system. It is false for
// the reason Start refuses, not because the hardware is missing.
func Available() bool { return false }
