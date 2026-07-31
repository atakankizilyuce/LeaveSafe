//go:build darwin

package bluetooth

import (
	"encoding/json"
	"sync"

	"github.com/leavesafe/leavesafe/internal/ws"
	log "github.com/sirupsen/logrus"
)

// Tracking one client per central is only meaningful where the stack says which
// central a message came from, so this lives with the backend that can — see
// ErrNoCentralIdentity for what happens on the platforms that cannot.

// Server manages BLE peripheral advertising and GATT connections. Each
// connected central gets its own ws.Client with independent auth state, which
// macOS makes possible by reporting a per-central identifier.
type Server struct {
	hub *ws.Hub
	mu  sync.Mutex
	// clients maps a central's identifier to its ws.Client.
	clients map[string]*ws.Client
}

// NewServer creates a new BLE server.
func NewServer(hub *ws.Hub) *Server {
	return &Server{
		hub:     hub,
		clients: make(map[string]*ws.Client),
	}
}

// getOrCreateClient returns the ws.Client for the given connection ID,
// creating a new one with the provided transport factory if needed.
//
// The client is registered with a callback that drops this map's entry when the
// hub lets the client go. Without it the two disagree: the hub can retire a
// client — an expired session, a rotated pairing key — while this map keeps
// handing the same one back, so a central that reconnects onto a recycled
// identifier would inherit whatever authentication the last one had.
func (s *Server) getOrCreateClient(connID string, newTransport func() *BLETransport) *ws.Client {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.clients[connID]; ok {
		return c
	}
	transport := newTransport()
	client := s.hub.RegisterExternalClient(transport, func() { s.forget(connID) })
	s.clients[connID] = client
	log.WithField("conn", connID).Info("BLE: client registered")
	return client
}

// forget drops the entry for connID if it is still the one being retired.
//
// The identity check matters: a central can disconnect and reconnect under the
// same identifier, and the hub's removal of the old client must not evict the
// new one that has taken its place.
func (s *Server) forget(connID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.clients, connID)
}

// handleIncoming processes a raw JSON message from a specific BLE connection.
func (s *Server) handleIncoming(connID string, data []byte, newTransport func() *BLETransport) {
	client := s.getOrCreateClient(connID, newTransport)

	var msg ws.ClientMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		log.Warnf("BLE: invalid message: %v", err)
		return
	}
	s.hub.HandleExternalMessage(client, msg)
}

// removeClient removes and unregisters the client for a given connection ID.
func (s *Server) removeClient(connID string) {
	s.mu.Lock()
	client, ok := s.clients[connID]
	if ok {
		delete(s.clients, connID)
	}
	s.mu.Unlock()
	if ok && client != nil {
		s.hub.RemoveExternalClient(client)
		log.WithField("conn", connID).Info("BLE: client disconnected")
	}
}

// disconnectAll removes all BLE clients from the hub.
func (s *Server) disconnectAll() {
	s.mu.Lock()
	clients := s.clients
	s.clients = make(map[string]*ws.Client)
	s.mu.Unlock()
	for _, client := range clients {
		s.hub.RemoveExternalClient(client)
	}
	if len(clients) > 0 {
		log.Infof("BLE: disconnected %d client(s)", len(clients))
	}
}
