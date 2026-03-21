package bluetooth

import (
	"encoding/json"
	"sync"

	log "github.com/sirupsen/logrus"
	"github.com/leavesafe/leavesafe/internal/ws"
)

// Custom 128-bit UUIDs for LeaveSafe BLE GATT service.
const (
	ServiceUUIDString = "4c454156-4553-4146-452d-424c45000001"
	TxCharUUIDString  = "4c454156-4553-4146-452d-424c45000002" // server -> client (notify)
	RxCharUUIDString  = "4c454156-4553-4146-452d-424c45000003" // client -> server (write)
)

// BLETransport implements ws.Transport for BLE clients.
type BLETransport struct {
	mu       sync.Mutex
	sendFunc func(data []byte) error
}

func (t *BLETransport) Send(data []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.sendFunc != nil {
		return t.sendFunc(data)
	}
	return nil
}

func (t *BLETransport) Close() error {
	return nil
}

// Server manages BLE peripheral advertising and GATT connections.
// Each connected central gets its own ws.Client with independent auth state.
type Server struct {
	hub *ws.Hub
	mu  sync.Mutex
	// clients maps a connection identifier to its ws.Client.
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
func (s *Server) getOrCreateClient(connID string, newTransport func() *BLETransport) *ws.Client {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.clients[connID]; ok {
		return c
	}
	transport := newTransport()
	client := s.hub.RegisterExternalClient(transport)
	s.clients[connID] = client
	log.WithField("conn", connID).Info("BLE: client registered")
	return client
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
