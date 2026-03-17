package ws

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/leavesafe/leavesafe/internal/auth"
	"github.com/leavesafe/leavesafe/internal/monitor"
	"nhooyr.io/websocket"
)

const (
	heartbeatInterval   = 15 * time.Second
	disconnectGracePeriod = 30 * time.Second
)

// Hub manages all WebSocket connections and dispatches alerts.
type Hub struct {
	mu              sync.RWMutex
	clients         map[*Client]bool
	authManager     *auth.Manager
	sensorMgr       *monitor.Manager
	armed           bool
	onAllDisconnect func()
	onClientChange  func(count int, armed bool)
	alertChan       chan ServerMessage
}

// NewHub creates a new WebSocket hub.
func NewHub(authMgr *auth.Manager, sensorMgr *monitor.Manager) *Hub {
	return &Hub{
		clients:     make(map[*Client]bool),
		authManager: authMgr,
		sensorMgr:   sensorMgr,
		alertChan:   make(chan ServerMessage, 100),
	}
}

// SetDisconnectCallback sets the function called when all authenticated clients
// disconnect while the system is armed.
func (h *Hub) SetDisconnectCallback(fn func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onAllDisconnect = fn
}

// SetClientChangeCallback sets the function called when client count changes.
func (h *Hub) SetClientChangeCallback(fn func(count int, armed bool)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onClientChange = fn
}

// ClientCount returns the number of connected authenticated clients.
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// IsArmed returns whether the system is armed.
func (h *Hub) IsArmed() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.armed
}

// Arm activates monitoring.
func (h *Hub) Arm() {
	h.mu.Lock()
	h.armed = true
	h.mu.Unlock()
	h.sensorMgr.StartEnabled()
	h.broadcastStatus()
}

// Disarm deactivates monitoring.
func (h *Hub) Disarm() {
	h.mu.Lock()
	h.armed = false
	h.mu.Unlock()
	h.sensorMgr.StopAll()
	h.broadcastStatus()
}

// HandleConnection handles a new WebSocket connection.
func (h *Hub) HandleConnection(ctx context.Context, conn *websocket.Conn) {
	client := &Client{
		hub:  h,
		conn: conn,
	}

	defer func() {
		h.removeClient(client)
		conn.Close(websocket.StatusNormalClosure, "")
	}()

	// Read loop
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}

		var msg ClientMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}

		h.handleMessage(ctx, client, msg)
	}
}

// PushAlert sends an alert to all connected authenticated clients.
func (h *Hub) PushAlert(alert ServerMessage) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for client := range h.clients {
		if client.authenticated {
			client.send(alert)
		}
	}
}

// RunAlertDispatcher listens for alerts from the sensor manager and dispatches them.
func (h *Hub) RunAlertDispatcher(ctx context.Context) {
	alertCh := h.sensorMgr.AlertChannel()
	for {
		select {
		case <-ctx.Done():
			return
		case alert := <-alertCh:
			if !h.IsArmed() {
				continue
			}
			log.Printf("[ALERT] %s — %s", strings.ToUpper(alert.Sensor), alert.Message)
			msg := NewAlert(alert.Sensor, string(alert.Level), alert.Message)
			h.PushAlert(msg)
		}
	}
}

// RunHeartbeat sends periodic status updates to all clients.
func (h *Hub) RunHeartbeat(ctx context.Context) {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.broadcastStatus()
		}
	}
}

// GetSensorInfos returns sensor info for all registered sensors.
func (h *Hub) GetSensorInfos() []SensorInfo {
	sensors := h.sensorMgr.Sensors()
	infos := make([]SensorInfo, 0, len(sensors))
	for _, s := range sensors {
		infos = append(infos, SensorInfo{
			Name:        s.Name(),
			DisplayName: s.DisplayName(),
			Available:   s.Available(),
			Enabled:     h.sensorMgr.IsEnabled(s.Name()),
		})
	}
	return infos
}

func (h *Hub) handleMessage(ctx context.Context, client *Client, msg ClientMessage) {
	switch msg.Type {
	case MsgTypeAuth:
		h.handleAuth(ctx, client, msg)
	case MsgTypePing:
		client.send(ServerMessage{Type: MsgTypePong})
	default:
		// All other messages require authentication
		if !client.authenticated {
			client.send(NewAuthFail("not authenticated", 0))
			return
		}
		switch msg.Type {
		case MsgTypeArm:
			h.Arm()
		case MsgTypeDisarm:
			h.Disarm()
		case MsgTypeConfigure:
			h.handleConfigure(msg)
		case MsgTypeTestAlert:
			msg := NewAlert("test", "warning", "Test alert triggered")
			h.PushAlert(msg)
			log.Println("[TEST] Test alert triggered from client")
		}
	}
}

func (h *Hub) handleAuth(ctx context.Context, client *Client, msg ClientMessage) {
	token, remaining, err := h.authManager.Authenticate(msg.Key)
	if err != nil {
		client.send(NewAuthFail(err.Error(), remaining))
		return
	}

	client.authenticated = true
	client.token = token

	h.mu.Lock()
	h.clients[client] = true
	count := len(h.clients)
	isArmed := h.armed
	changeCb := h.onClientChange
	h.mu.Unlock()

	if changeCb != nil {
		changeCb(count, isArmed)
	}

	infos := h.GetSensorInfos()
	client.send(NewAuthOK(token, infos))
}

func (h *Hub) handleConfigure(msg ClientMessage) {
	if msg.Sensors == nil {
		return
	}
	for name, enabled := range msg.Sensors {
		if enabled {
			h.sensorMgr.Enable(name)
		} else {
			h.sensorMgr.Disable(name)
		}
	}
	h.broadcastStatus()
}

func (h *Hub) removeClient(client *Client) {
	h.mu.Lock()
	delete(h.clients, client)

	if client.token != "" {
		h.authManager.RemoveSession(client.token)
	}

	// Check if all clients disconnected while armed
	armed := h.armed
	clientCount := len(h.clients)
	disconnectCb := h.onAllDisconnect
	changeCb := h.onClientChange
	h.mu.Unlock()

	if changeCb != nil {
		changeCb(clientCount, armed)
	}

	if armed && clientCount == 0 && disconnectCb != nil {
		log.Println("[WARN] All clients disconnected while armed - triggering alarm")
		go func() {
			time.Sleep(disconnectGracePeriod)
			h.mu.RLock()
			count := len(h.clients)
			isArmed := h.armed
			h.mu.RUnlock()
			if count == 0 && isArmed {
				disconnectCb()
			}
		}()
	}
}

func (h *Hub) broadcastStatus() {
	h.mu.RLock()
	defer h.mu.RUnlock()

	states := make(map[string]*SensorState)
	for _, s := range h.sensorMgr.Sensors() {
		status := "ok"
		if !s.Available() {
			status = "unavailable"
		}
		states[s.Name()] = &SensorState{
			Enabled: h.sensorMgr.IsEnabled(s.Name()),
			Status:  status,
		}
	}

	msg := NewStatus(h.armed, states)
	for client := range h.clients {
		if client.authenticated {
			client.send(msg)
		}
	}
}
