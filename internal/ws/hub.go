package ws

import (
	"context"
	"encoding/json"
	"strings"

	log "github.com/sirupsen/logrus"
	"sync"
	"time"

	"github.com/leavesafe/leavesafe/internal/auth"
	"github.com/leavesafe/leavesafe/internal/config"
	"github.com/leavesafe/leavesafe/internal/eventlog"
	"github.com/leavesafe/leavesafe/internal/monitor"
	"nhooyr.io/websocket"
)

const (
	heartbeatInterval     = 15 * time.Second
	disconnectGracePeriod = 30 * time.Second
)

// Hub manages all WebSocket connections and dispatches alerts.
type Hub struct {
	mu              sync.RWMutex
	clients         map[*Client]bool
	authManager     *auth.Manager
	sensorMgr       *monitor.Manager
	armed           bool
	version         string
	onAllDisconnect func()
	onClientChange  func(count int, armed bool)
	onAlarmTrigger  func()
	onAlarmDismiss  func()
	autoArmOnLock   bool
	pinEnabled      bool
	pinCode         string
	alertChan       chan ServerMessage
	eventLog        *eventlog.Logger

	cfg *config.Config

	// Alarm state tracking to prevent re-trigger loops
	alarmActive       bool
	alarmSensor       string
	suppressedSensors map[string]time.Time
}

// NewHub creates a new WebSocket hub.
func NewHub(authMgr *auth.Manager, sensorMgr *monitor.Manager, version string) *Hub {
	return &Hub{
		clients:           make(map[*Client]bool),
		authManager:       authMgr,
		sensorMgr:         sensorMgr,
		version:           version,
		alertChan:         make(chan ServerMessage, 100),
		suppressedSensors: make(map[string]time.Time),
	}
}

// SetPinProtection configures optional PIN-based disarm protection.
func (h *Hub) SetPinProtection(enabled bool, pin string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pinEnabled = enabled
	h.pinCode = pin
}

// SetAutoArmOnLock enables automatic arm/disarm on screen lock/unlock.
func (h *Hub) SetAutoArmOnLock(enabled bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.autoArmOnLock = enabled
}

// SetConfig stores the application config reference for web-based configuration.
func (h *Hub) SetConfig(cfg *config.Config) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cfg = cfg
}

// SetEventLogger sets the event logger for recording security events.
func (h *Hub) SetEventLogger(el *eventlog.Logger) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.eventLog = el
}

func (h *Hub) logEvent(evt eventlog.Event) {
	h.mu.RLock()
	el := h.eventLog
	h.mu.RUnlock()
	if el != nil {
		el.Log(evt)
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

// SetAlarmTriggerCallback sets the function called when a sensor alert fires
// while the system is armed.
func (h *Hub) SetAlarmTriggerCallback(fn func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onAlarmTrigger = fn
}

// SetAlarmDismissCallback sets the function called when the alarm should stop.
func (h *Hub) SetAlarmDismissCallback(fn func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onAlarmDismiss = fn
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
	h.logEvent(eventlog.Event{Type: eventlog.EventArm, Message: "System armed"})
}

// Disarm deactivates monitoring and stops any active alarm.
func (h *Hub) Disarm() {
	h.mu.Lock()
	h.armed = false
	h.alarmActive = false
	h.alarmSensor = ""
	h.mu.Unlock()
	h.sensorMgr.StopAll()
	h.fireAlarmDismiss()
	h.broadcastStatus()
	h.logEvent(eventlog.Event{Type: eventlog.EventDisarm, Message: "System disarmed"})
}

// RegisterExternalClient creates and registers a client using a non-WebSocket transport.
func (h *Hub) RegisterExternalClient(transport Transport) *Client {
	return &Client{
		hub:       h,
		transport: transport,
	}
}

// HandleExternalMessage processes a message from an external (non-WebSocket) client.
func (h *Hub) HandleExternalMessage(client *Client, msg ClientMessage) {
	h.handleMessage(context.Background(), client, msg)
}

// RemoveExternalClient removes a non-WebSocket client from the hub.
func (h *Hub) RemoveExternalClient(client *Client) {
	h.removeClient(client)
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

// TriggerSensorTest simulates a sensor alert by name for testing.
func (h *Hub) TriggerSensorTest(sensorName string) bool {
	var displayName string
	for _, s := range h.sensorMgr.Sensors() {
		if s.Name() == sensorName {
			displayName = s.DisplayName()
			break
		}
	}
	if displayName == "" {
		return false
	}

	message := displayName + " triggered (manual test)"
	h.PushAlert(NewAlert(sensorName, "critical", message))

	if h.IsArmed() {
		h.fireAlarmTrigger()
		h.PushAlert(NewAlarmActive(sensorName, message))
	}

	log.WithField("sensor", sensorName).Info("Manual sensor trigger")
	return true
}

// RunAlertDispatcher listens for alerts from the sensor manager and dispatches them.
func (h *Hub) RunAlertDispatcher(ctx context.Context) {
	alertCh := h.sensorMgr.AlertChannel()
	for {
		select {
		case <-ctx.Done():
			return
		case alert := <-alertCh:
			// Handle auto-arm on screen lock/unlock
			h.mu.RLock()
			autoArm := h.autoArmOnLock
			h.mu.RUnlock()

			if autoArm && alert.Sensor == "screen" {
				if strings.Contains(alert.Message, "off") && !h.IsArmed() && h.ClientCount() > 0 {
					log.Info("Auto-arming: screen locked")
					h.Arm()
					continue
				}
				if strings.Contains(alert.Message, "on") && h.IsArmed() && h.ClientCount() > 0 {
					log.Info("Auto-disarming: screen unlocked")
					h.Disarm()
					continue
				}
			}

			if !h.IsArmed() {
				continue
			}

			// Skip alerts from suppressed sensors (grace period after dismiss)
			h.mu.Lock()
			if until, ok := h.suppressedSensors[alert.Sensor]; ok {
				if time.Now().Before(until) {
					h.mu.Unlock()
					continue
				}
				delete(h.suppressedSensors, alert.Sensor)
			}
			// Skip if alarm is already active (prevent re-trigger loop)
			if h.alarmActive {
				h.mu.Unlock()
				continue
			}
			h.alarmActive = true
			h.alarmSensor = alert.Sensor
			h.mu.Unlock()

			log.WithFields(log.Fields{"sensor": strings.ToUpper(alert.Sensor)}).Warn(alert.Message)
			h.PushAlert(NewAlert(alert.Sensor, string(alert.Level), alert.Message))
			h.fireAlarmTrigger()
			h.PushAlert(NewAlarmActive(alert.Sensor, alert.Message))
			h.logEvent(eventlog.Event{Type: eventlog.EventAlert, Sensor: alert.Sensor, Message: alert.Message})
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

func (h *Hub) fireAlarmTrigger() {
	h.mu.RLock()
	cb := h.onAlarmTrigger
	h.mu.RUnlock()
	if cb != nil {
		cb()
	}
}

func (h *Hub) fireAlarmDismiss() {
	h.mu.RLock()
	cb := h.onAlarmDismiss
	h.mu.RUnlock()
	if cb != nil {
		cb()
	}
}

func (h *Hub) handleMessage(ctx context.Context, client *Client, msg ClientMessage) {
	switch msg.Type {
	case MsgTypeAuth:
		h.handleAuth(ctx, client, msg)
	case MsgTypePing:
		client.send(ServerMessage{Type: MsgTypePong})
	default:
		if !client.authenticated {
			client.send(NewAuthFail("not authenticated", 0))
			return
		}
		switch msg.Type {
		case MsgTypeArm:
			h.Arm()
		case MsgTypeDisarm:
			h.mu.RLock()
			pinRequired := h.pinEnabled && h.pinCode != ""
			h.mu.RUnlock()
			if pinRequired {
				client.send(ServerMessage{Type: MsgTypePinRequired})
				return
			}
			h.Disarm()
		case MsgTypeDisarmPin:
			h.mu.RLock()
			pinOK := !h.pinEnabled || h.pinCode == "" || msg.Pin == h.pinCode
			h.mu.RUnlock()
			if !pinOK {
				client.send(ServerMessage{Type: MsgTypeAuthFail, Reason: "invalid PIN"})
				return
			}
			h.Disarm()
		case MsgTypeConfigure:
			h.handleConfigure(msg)
		case MsgTypeTestAlert:
			h.PushAlert(NewAlert("test", "warning", "Test alert triggered"))
			log.Info("Test alert triggered from client")
		case MsgTypeTriggerSensor:
			if msg.Sensor != "" {
				h.TriggerSensorTest(msg.Sensor)
			}
		case MsgTypeGetConfig:
			h.handleGetConfig(client)
		case MsgTypeUpdateConfig:
			h.handleUpdateConfig(msg)
		case MsgTypeDismissAlarm:
			h.mu.Lock()
			triggeredSensor := h.alarmSensor
			h.alarmActive = false
			h.alarmSensor = ""
			if triggeredSensor == "input" {
				h.suppressedSensors["input"] = time.Now().Add(5 * time.Second)
			}
			h.mu.Unlock()
			h.fireAlarmDismiss()
			log.Info("Alarm dismissed from client")

		case MsgTypeDismissAlarmPause:
			h.mu.Lock()
			triggeredSensor := h.alarmSensor
			h.alarmActive = false
			h.alarmSensor = ""
			h.mu.Unlock()
			h.fireAlarmDismiss()

			sensor := msg.Sensor
			if sensor == "" {
				sensor = triggeredSensor
			}
			duration := msg.Duration
			if duration <= 0 {
				duration = 5
			}
			if sensor != "" {
				h.sensorMgr.Disable(sensor)
				go func(name string, d int) {
					time.Sleep(time.Duration(d) * time.Second)
					h.sensorMgr.Enable(name)
					if h.IsArmed() {
						h.sensorMgr.StartEnabled()
					}
					h.broadcastStatus()
				}(sensor, duration)
			}
			h.broadcastStatus()
			log.WithField("sensor", sensor).Infof("Alarm dismissed, sensor paused for %ds", duration)

		case MsgTypeDismissAlarmDisable:
			h.mu.Lock()
			triggeredSensor := h.alarmSensor
			h.alarmActive = false
			h.alarmSensor = ""
			h.mu.Unlock()
			h.fireAlarmDismiss()

			sensor := msg.Sensor
			if sensor == "" {
				sensor = triggeredSensor
			}
			if sensor != "" {
				h.sensorMgr.Disable(sensor)
			}
			h.broadcastStatus()
			log.WithField("sensor", sensor).Info("Alarm dismissed, sensor permanently disabled")
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
	client.send(NewAuthOK(token, infos, h.version))
	h.logEvent(eventlog.Event{Type: eventlog.EventConnect, Message: "Client authenticated"})
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

func (h *Hub) handleGetConfig(client *Client) {
	h.mu.RLock()
	cfg := h.cfg
	h.mu.RUnlock()
	if cfg == nil {
		return
	}
	payload := configToPayload(cfg)
	client.send(ServerMessage{
		Type:   MsgTypeConfigData,
		Config: &payload,
	})
}

func (h *Hub) handleUpdateConfig(msg ClientMessage) {
	if msg.Config == nil {
		return
	}
	h.mu.Lock()
	cfg := h.cfg
	h.mu.Unlock()
	if cfg == nil {
		return
	}

	p := msg.Config
	needsRestart := p.Port != cfg.Port || (p.ConnectionMode != "" && p.ConnectionMode != cfg.ConnectionMode)

	cfg.Port = p.Port
	if p.ConnectionMode != "" {
		cfg.ConnectionMode = p.ConnectionMode
	}
	cfg.MaxSessions = p.MaxSessions
	cfg.MaxAuthAttempts = p.MaxAuthAttempts
	cfg.LockoutSeconds = p.LockoutSeconds
	cfg.HeartbeatSeconds = p.HeartbeatSeconds
	cfg.DisconnectGraceSeconds = p.DisconnectGraceSeconds
	cfg.AutoArmOnLock = p.AutoArmOnLock
	cfg.InputThreshold = p.InputThreshold
	cfg.Alarm = p.Alarm

	if p.PinProtection.Pin != "" {
		cfg.PinProtection.Pin = p.PinProtection.Pin
	}
	cfg.PinProtection.Enabled = p.PinProtection.Enabled
	h.SetPinProtection(cfg.PinProtection.Enabled, cfg.PinProtection.Pin)
	h.SetAutoArmOnLock(cfg.AutoArmOnLock)

	if p.EnabledSensors != nil {
		cfg.EnabledSensors = p.EnabledSensors
		for name, enabled := range p.EnabledSensors {
			if enabled {
				h.sensorMgr.Enable(name)
			} else {
				h.sensorMgr.Disable(name)
			}
		}
	}

	if err := config.Save(cfg); err != nil {
		log.Errorf("Failed to save config: %v", err)
	}

	h.broadcastStatus()

	if needsRestart {
		h.PushAlert(NewAlert("system", "warning", "Port changed — restart required to take effect"))
	}

	log.Info("Configuration updated from client")
}

func configToPayload(cfg *config.Config) ConfigPayload {
	return ConfigPayload{
		Port:                   cfg.Port,
		MaxSessions:            cfg.MaxSessions,
		MaxAuthAttempts:        cfg.MaxAuthAttempts,
		LockoutSeconds:         cfg.LockoutSeconds,
		HeartbeatSeconds:       cfg.HeartbeatSeconds,
		DisconnectGraceSeconds: cfg.DisconnectGraceSeconds,
		AutoArmOnLock:          cfg.AutoArmOnLock,
		InputThreshold:         cfg.InputThreshold,
		ConnectionMode:         cfg.ConnectionMode,
		Alarm:                  cfg.Alarm,
		PinProtection: PinProtectionPayload{
			Enabled: cfg.PinProtection.Enabled,
			HasPin:  cfg.PinProtection.Pin != "",
		},
		EnabledSensors: cfg.EnabledSensors,
	}
}

func (h *Hub) removeClient(client *Client) {
	h.mu.Lock()
	delete(h.clients, client)

	if client.token != "" {
		h.authManager.RemoveSession(client.token)
	}

	armed := h.armed
	clientCount := len(h.clients)
	disconnectCb := h.onAllDisconnect
	changeCb := h.onClientChange
	h.mu.Unlock()

	if changeCb != nil {
		changeCb(clientCount, armed)
	}

	h.logEvent(eventlog.Event{Type: eventlog.EventDisconnect, Message: "Client disconnected"})

	if armed && clientCount == 0 && disconnectCb != nil {
		log.Warn("All clients disconnected while armed - triggering alarm")
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
