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
type Server struct {
	hub    *ws.Hub
	client *ws.Client
	mu     sync.Mutex
}

// NewServer creates a new BLE server.
func NewServer(hub *ws.Hub) *Server {
	return &Server{hub: hub}
}

// handleIncoming processes a raw JSON message from a BLE client.
func (s *Server) handleIncoming(data []byte) {
	s.mu.Lock()
	client := s.client
	s.mu.Unlock()
	if client == nil {
		return
	}

	var msg ws.ClientMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		log.Warnf("BLE: invalid message: %v", err)
		return
	}
	s.hub.HandleExternalMessage(client, msg)
}

// disconnect removes the current BLE client from the hub.
func (s *Server) disconnect() {
	s.mu.Lock()
	client := s.client
	s.client = nil
	s.mu.Unlock()
	if client != nil {
		s.hub.RemoveExternalClient(client)
		log.Info("BLE: client disconnected")
	}
}
