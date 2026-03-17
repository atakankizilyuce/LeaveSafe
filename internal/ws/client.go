package ws

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"nhooyr.io/websocket"
)

const writeTimeout = 5 * time.Second

// Client represents a single WebSocket connection.
type Client struct {
	hub           *Hub
	conn          *websocket.Conn
	authenticated bool
	token         string
}

// send marshals and writes a message to the client's WebSocket connection.
func (c *Client) send(msg ServerMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("[ERROR] marshal message: %v", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()

	if err := c.conn.Write(ctx, websocket.MessageText, data); err != nil {
		log.Printf("[WARN] write to client: %v", err)
	}
}
