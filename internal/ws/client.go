package ws

import (
	"context"
	"encoding/json"
	"time"

	log "github.com/sirupsen/logrus"
	"nhooyr.io/websocket"
)

const writeTimeout = 5 * time.Second

// Transport abstracts the underlying connection (WebSocket, BLE, etc.).
type Transport interface {
	Send(data []byte) error
	Close() error
}

// Client represents a single connected device.
type Client struct {
	hub           *Hub
	conn          *websocket.Conn // nil for non-WebSocket transports
	transport     Transport       // nil for WebSocket clients (uses conn)
	authenticated bool
	token         string
}

// send marshals and writes a message to the client.
func (c *Client) send(msg ServerMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Errorf("marshal message: %v", err)
		return
	}

	if c.transport != nil {
		if err := c.transport.Send(data); err != nil {
			log.Warnf("write to client: %v", err)
		}
		return
	}

	if c.conn != nil {
		ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
		defer cancel()
		if err := c.conn.Write(ctx, websocket.MessageText, data); err != nil {
			log.Warnf("write to client: %v", err)
		}
	}
}
