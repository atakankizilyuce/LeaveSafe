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
	"github.com/leavesafe/leavesafe/internal/location"
	"github.com/leavesafe/leavesafe/internal/monitor"
	"github.com/leavesafe/leavesafe/internal/safe"
	"github.com/leavesafe/leavesafe/internal/state"
	"nhooyr.io/websocket"
)

const (
	defaultHeartbeatInterval     = 15 * time.Second
	defaultDisconnectGracePeriod = 30 * time.Second
)

// defaultAuthDeadline is how long a fresh connection has to present a valid
// pairing key before it is dropped. Only authenticated clients count against
// max_sessions, so without this an unauthenticated peer could hold connections
// open indefinitely and exhaust the server's sockets.
const defaultAuthDeadline = 20 * time.Second

// maxMessageBytes caps a single client message. The largest thing the protocol
// carries is a configuration payload, which is a couple of kilobytes; the rest
// are a line of JSON each. Sixteen kilobytes leaves room for the sensor map to
// grow without leaving the size of an unpaired peer's frame up to them.
const maxMessageBytes = 16 << 10

// Hub manages all WebSocket connections and dispatches alerts.
type Hub struct {
	mu          sync.RWMutex
	clients     map[*Client]bool
	authManager *auth.Manager
	sensorMgr   *monitor.Manager
	armed       bool
	// armedAt is when armed last became true, zero when disarmed. The phone's
	// "armed for 12 minutes" counter reads from here rather than keeping its
	// own, because the page holding it is thrown away every time the screen
	// locks and would otherwise restart from zero on every reconnect.
	armedAt           time.Time
	version           string
	onAllDisconnected func()
	onClientChange    func(count int, armed bool)
	onAlarmTrigger    func()
	onAlarmDismiss    func()
	autoArmOnLock     bool
	pinEnabled        bool
	pinHash           string
	alertChan         chan ServerMessage
	eventLog          *eventlog.Logger
	stateStore        *state.Store
	certFP            string

	heartbeatInterval     time.Duration
	disconnectGracePeriod time.Duration
	// authDeadline is per-hub rather than a package variable so a test can
	// shorten it without racing the connection goroutines that read it.
	authDeadline time.Duration

	// pending bounds how many sockets may sit unpaired at once. The deadline
	// above limits how long each one lives; this limits how many there are.
	pending *pendingConns

	tracker    *location.Tracker
	trackerCtx context.Context //nolint:containedctx // scopes tracker goroutines to the app lifetime

	cfg *config.Config

	// Alarm state tracking to prevent re-trigger loops
	alarmActive       bool
	alarmSensor       string
	suppressedSensors map[string]time.Time

	// updateAvailable is the newest release the update check found, kept so a
	// phone that pairs after the check still hears about it. Nil until one is
	// found, which is also the state of a copy with checking switched off.
	updateAvailable *UpdatePayload
}

// NewHub creates a new WebSocket hub.
func NewHub(authMgr *auth.Manager, sensorMgr *monitor.Manager, version string) *Hub {
	return &Hub{
		clients:               make(map[*Client]bool),
		authManager:           authMgr,
		sensorMgr:             sensorMgr,
		version:               version,
		alertChan:             make(chan ServerMessage, 100),
		suppressedSensors:     make(map[string]time.Time),
		heartbeatInterval:     defaultHeartbeatInterval,
		disconnectGracePeriod: defaultDisconnectGracePeriod,
		authDeadline:          defaultAuthDeadline,
		pending:               newPendingConns(maxPendingConns, maxPendingConnsPerAdr),
	}
}

// releasePending gives back the unpaired-socket slot this client holds, if it
// holds one. Calling it twice is a no-op, which is what lets both the pairing
// path and the disconnect path call it without counting the same socket twice.
//
// The flag is read and written only on the connection's own goroutine, the same
// place client.authenticated lives, so it needs no lock of its own.
func (h *Hub) releasePending(c *Client) {
	if !c.pendingHeld {
		return
	}
	c.pendingHeld = false
	h.pending.release(c.remoteAddr)
}

// SetAuthDeadline sets how long a fresh connection has to present a valid
// pairing key. Zero or negative leaves the current setting alone.
func (h *Hub) SetAuthDeadline(d time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if d > 0 {
		h.authDeadline = d
	}
}

// SetTimings configures the status broadcast interval and how long the hub
// waits after the last client drops before treating it as an intrusion. Zero
// or negative values leave the current setting untouched.
func (h *Hub) SetTimings(heartbeat, disconnectGrace time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if heartbeat > 0 {
		h.heartbeatInterval = heartbeat
	}
	if disconnectGrace > 0 {
		h.disconnectGracePeriod = disconnectGrace
	}
}

// SetPinProtection configures optional PIN-based disarm protection. pinHash is
// the salted hash produced by auth.HashPin, never the PIN itself.
func (h *Hub) SetPinProtection(enabled bool, pinHash string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pinEnabled = enabled
	h.pinHash = pinHash
}

// SetAutoArmOnLock enables automatic arm/disarm on screen lock/unlock.
func (h *Hub) SetAutoArmOnLock(enabled bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.autoArmOnLock = enabled
}

// SetLocationTracker attaches a location tracker. ctx bounds the polling
// goroutines the tracker starts when the system is armed. Passing a nil tracker
// leaves the feature off, which is what happens when it is disabled in config.
func (h *Hub) SetLocationTracker(ctx context.Context, tracker *location.Tracker) {
	h.mu.Lock()
	h.tracker = tracker
	h.trackerCtx = ctx
	h.mu.Unlock()

	if tracker != nil {
		tracker.SetUpdateCallback(func(location.Snapshot) {
			h.PushAlert(NewLocation(h.LocationPayload()))
		})
	}
}

// LocationPayload returns the current position view for the phone.
func (h *Hub) LocationPayload() LocationPayload {
	h.mu.RLock()
	tracker := h.tracker
	anchorEnabled := h.cfg != nil && h.cfg.Location.PhoneAnchor
	h.mu.RUnlock()

	if tracker == nil {
		return LocationPayload{Enabled: false}
	}

	payload := locationPayload(true, tracker.Snapshot())
	// The tracker only knows about its own providers. With the phone anchor
	// turned on there is a source even when every provider is unavailable, and
	// saying otherwise would send the panel to "nothing works here" while it
	// waits for a position the phone is about to send.
	payload.Available = payload.Available || anchorEnabled
	return payload
}

// SetConfig stores the application config reference for web-based configuration.
func (h *Hub) SetConfig(cfg *config.Config) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cfg = cfg
}

// UpdateSettings reports whether update checking is on and which channel it
// should use.
//
// The config is mutated by the phone under this lock, so a background checker has
// to ask rather than read the struct: switching to beta or switching checking off
// takes effect on the next check instead of on the next restart.
func (h *Hub) UpdateSettings() (enabled bool, channel string) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.cfg == nil {
		return true, ""
	}
	return h.cfg.UpdateCheckEnabled(), h.cfg.UpdateChannel
}

// SetEventLogger sets the event logger for recording security events.
func (h *Hub) SetEventLogger(el *eventlog.Logger) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.eventLog = el
}

// EventLogger returns the event logger, or nil if not set.
func (h *Hub) EventLogger() *eventlog.Logger {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.eventLog
}

func (h *Hub) logEvent(evt eventlog.Event) {
	h.mu.RLock()
	el := h.eventLog
	h.mu.RUnlock()
	if el != nil {
		el.Log(evt)
	}
}

// SetAllDisconnectedCallback sets the function called once every authenticated
// client has gone while the system is armed.
//
// This reports; it does not accuse. A phone drops its socket whenever its screen
// locks or its browser is backgrounded, which is the ordinary case and not an
// intrusion — the laptop keeps watching its own sensors and alarms on those.
func (h *Hub) SetAllDisconnectedCallback(fn func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onAllDisconnected = fn
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

// ArmedAt returns when the system was armed, or the zero time when it is not.
func (h *Hub) ArmedAt() time.Time {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.armedAt
}

// SetCertFingerprint records the SHA-256 fingerprint of the TLS certificate
// this server presents, so it can be announced to connecting clients. Empty
// when the server runs over plain HTTP and there is no certificate.
func (h *Hub) SetCertFingerprint(fp string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.certFP = fp
}

// SetStateStore attaches the store that records the armed state across
// restarts. A nil store turns the feature off, and every call site tolerates
// that, so tests and non-persistent embeddings need not supply one.
func (h *Hub) SetStateStore(store *state.Store) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stateStore = store
}

// RestoreArmed puts the hub back into the armed state without re-running the
// side effects of a fresh arm — no event is logged as if the user had just
// tapped Arm, because they did not. Used at startup when the previous run ended
// while armed and the config asks for the state to be restored.
//
// since is when the previous run armed, so the phone's counter resumes rather
// than restarts. A zero time means the state file did not record one, and now is
// the only honest answer left.
func (h *Hub) RestoreArmed(since time.Time) {
	if since.IsZero() {
		since = time.Now()
	}

	h.mu.Lock()
	h.armed = true
	h.armedAt = since
	tracker := h.tracker
	trackerCtx := h.trackerCtx
	h.mu.Unlock()

	h.sensorMgr.StartEnabled()
	if tracker != nil && trackerCtx != nil {
		tracker.Start(trackerCtx)
	}
	h.broadcastStatus()
	h.logEvent(eventlog.Event{Type: eventlog.EventArm, Message: "Armed state restored after restart"})
}

// persistArmed records the armed state for the next run to read. A failure here
// costs the restart warning, not the monitoring itself, so it is logged rather
// than propagated.
func (h *Hub) persistArmed(armed bool) {
	h.mu.RLock()
	store := h.stateStore
	h.mu.RUnlock()
	if store == nil {
		return
	}
	if err := store.Save(armed); err != nil {
		log.Warnf("Could not record the armed state: %v", err)
	}
}

// Arm activates monitoring.
func (h *Hub) Arm() {
	h.mu.Lock()
	h.armed = true
	h.armedAt = time.Now()
	tracker := h.tracker
	trackerCtx := h.trackerCtx
	h.mu.Unlock()

	h.persistArmed(true)

	h.sensorMgr.StartEnabled()

	// Location tracking runs only while armed. There is no reason to scan for
	// Wi-Fi or hit a geolocation service while the user is sitting at the
	// machine, and every reason not to.
	if tracker != nil && trackerCtx != nil {
		tracker.Start(trackerCtx)
	}

	h.broadcastStatus()
	h.logEvent(eventlog.Event{Type: eventlog.EventArm, Message: "System armed"})
}

// Disarm deactivates monitoring and stops any active alarm.
func (h *Hub) Disarm() {
	h.mu.Lock()
	h.armed = false
	h.armedAt = time.Time{}
	h.alarmActive = false
	h.alarmSensor = ""
	tracker := h.tracker
	h.mu.Unlock()

	h.persistArmed(false)

	h.sensorMgr.StopAll()
	if tracker != nil {
		// Stop keeps the last fix, so the phone can still show where the
		// machine was rather than an empty panel.
		tracker.Stop()
	}
	h.fireAlarmDismiss()
	h.broadcastStatus()
	h.logEvent(eventlog.Event{Type: eventlog.EventDisarm, Message: "System disarmed"})
}

// RegisterExternalClient creates and registers a client using a non-WebSocket
// transport. Such transports have no network address, so they share a single
// rate-limit bucket — which is right for BLE, where every peer is within radio
// range anyway.
func (h *Hub) RegisterExternalClient(transport Transport) *Client {
	return &Client{
		hub:        h,
		transport:  transport,
		remoteAddr: auth.UnknownAddr,
	}
}

// HandleExternalMessage processes a message from an external (non-WebSocket) client.
func (h *Hub) HandleExternalMessage(client *Client, msg ClientMessage) {
	h.handleMessage(client, msg)
}

// RemoveExternalClient removes a non-WebSocket client from the hub.
func (h *Hub) RemoveExternalClient(client *Client) {
	h.removeClient(client)
}

// HandleConnection handles a new WebSocket connection. remoteAddr is the peer
// address reported by the HTTP server; pairing attempts are rate-limited
// against it.
func (h *Hub) HandleConnection(ctx context.Context, conn *websocket.Conn, remoteAddr string) {
	client := &Client{
		hub:        h,
		conn:       conn,
		remoteAddr: remoteAddr,
	}

	// Every message this protocol defines is small JSON. Saying so explicitly
	// keeps the ceiling from being whatever the websocket library happens to
	// default to, which is not a number this program should inherit silently.
	conn.SetReadLimit(maxMessageBytes)

	// A socket that has not paired costs a goroutine and a file descriptor and
	// counts against nothing else, so the number of them is capped. Turning one
	// away is not the same as a failure: say why, then close.
	if !h.pending.acquire(auth.NormalizeAddr(remoteAddr)) {
		log.Warnf("Refusing a connection from %s: too many unpaired sockets waiting", remoteAddr)
		client.send(NewAuthFail("the laptop is busy, try again in a moment", 0))
		_ = conn.Close(websocket.StatusTryAgainLater, "too many unpaired connections")
		return
	}
	client.pendingHeld = true

	defer func() {
		h.releasePending(client)
		h.removeClient(client)
		_ = conn.Close(websocket.StatusNormalClosure, "")
	}()

	// Identify the server before asking for anything. A client that arrived by
	// QR code knows which certificate it should be talking to, and this is what
	// it compares against — before it sends the pairing key, not after.
	h.mu.RLock()
	certFP := h.certFP
	deadlineAfter := h.authDeadline
	h.mu.RUnlock()
	client.send(NewHello(certFP, h.version))

	// Until the client authenticates, every read shares one absolute deadline,
	// so a peer cannot keep a socket open forever by dribbling out unauthenticated
	// frames. client.authenticated is touched only in this goroutine (handleAuth
	// runs synchronously below), so reading it here needs no lock.
	deadline := time.Now().Add(deadlineAfter)

	for {
		readCtx := ctx
		var cancel context.CancelFunc
		if !client.authenticated {
			readCtx, cancel = context.WithDeadline(ctx, deadline)
		}
		_, data, err := conn.Read(readCtx)
		if cancel != nil {
			cancel()
		}
		if err != nil {
			return
		}

		var msg ClientMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}

		h.handleMessage(client, msg)
	}
}

// PushAlert sends an alert to all connected authenticated clients.
func (h *Hub) PushAlert(alert ServerMessage) {
	// Collect targets under the lock, then send with it released. A blocking
	// write to one slow client must not stall arm/disarm or delay the alarm
	// reaching everyone else — for an alarm system that delay is the attack.
	h.mu.RLock()
	targets := make([]*Client, 0, len(h.clients))
	for client := range h.clients {
		if client.authenticated {
			targets = append(targets, client)
		}
	}
	h.mu.RUnlock()

	for _, client := range targets {
		client.send(alert)
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

			// An alarm is exactly when the position matters, so it goes out
			// with the alert rather than waiting for the next poll.
			if payload := h.LocationPayload(); payload.Enabled {
				h.PushAlert(NewLocation(payload))
			}
		}
	}
}

// RunHeartbeat sends periodic status updates to all clients.
func (h *Hub) RunHeartbeat(ctx context.Context) {
	h.mu.RLock()
	interval := h.heartbeatInterval
	h.mu.RUnlock()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.dropExpiredSessions()
			h.broadcastStatus()
		}
	}
}

// dropExpiredSessions disconnects clients whose session is no longer valid.
//
// A session that expires while its socket is still open would otherwise stay
// usable until the phone next sent a message — which, for a phone sitting idle
// in a pocket, could be hours after the limit passed. Checking on the heartbeat
// makes the timeout mean what it says.
//
// It runs whether or not the expiry limits are configured, because expiry is
// not the only thing that ends a session. Rotating the pairing key drops every
// token at once, and that is done precisely when a key is believed to have
// leaked — so the sockets already open on the old key are the ones that most
// need severing, and waiting for them to speak first is waiting on the
// attacker's convenience.
func (h *Hub) dropExpiredSessions() {
	h.mu.RLock()
	stale := make([]*Client, 0)
	for client := range h.clients {
		if client.authenticated && !h.authManager.ValidateSession(client.token) {
			stale = append(stale, client)
		}
	}
	h.mu.RUnlock()

	for _, client := range stale {
		log.Info("Session no longer valid, disconnecting client")
		client.send(NewAuthFail("session expired, pair again", h.authManager.MaxAttempts()))
		h.logEvent(eventlog.Event{Type: eventlog.EventDisconnect, Message: "Session expired"})
		client.close()
		h.removeClient(client)
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

// upgradePinHash rewrites a stored PIN hash that uses an older scheme.
//
// Hashes cannot be upgraded in the background: the PIN itself is needed, and it
// only exists in memory for the moment a client presents a correct one. So the
// migration happens here, once, on the first successful disarm after the
// upgrade — and a user who never disarms keeps the old hash, which still
// verifies. pin is the cleartext PIN and is not stored anywhere.
func (h *Hub) upgradePinHash(pin, current string) {
	if current == "" || !auth.NeedsRehash(current) {
		return
	}

	updated, err := auth.HashPin(pin)
	if err != nil {
		log.Warnf("Could not upgrade the stored PIN hash: %v", err)
		return
	}

	h.mu.Lock()
	h.pinHash = updated
	cfg := h.cfg
	var snapshot config.Config
	if cfg != nil {
		cfg.PinProtection.PinHash = updated
		snapshot = *cfg
	}
	h.mu.Unlock()

	if cfg == nil {
		return
	}
	if err := config.Save(&snapshot); err != nil {
		log.Warnf("Could not save the upgraded PIN hash: %v", err)
		return
	}
	log.Info("Stored PIN hash upgraded to the current scheme")
}

// The alarm callbacks reach into the platform audio and volume backends, which
// speak to WinMM, PulseAudio and CoreAudio. A panic down there must not stop
// the hub from telling the phone what happened — the phone alert is the part
// the user actually receives when they are not at the machine.
func (h *Hub) fireAlarmTrigger() {
	h.mu.RLock()
	cb := h.onAlarmTrigger
	h.mu.RUnlock()
	if cb != nil {
		safe.Do("alarm-trigger", cb)
	}
}

func (h *Hub) fireAlarmDismiss() {
	h.mu.RLock()
	cb := h.onAlarmDismiss
	h.mu.RUnlock()
	if cb != nil {
		safe.Do("alarm-dismiss", cb)
	}
}

func (h *Hub) handleMessage(client *Client, msg ClientMessage) {
	// Anything an authenticated client sends is activity, and activity is what
	// the idle timeout measures. Pairing itself is excluded: there is no
	// session to touch yet.
	if client.authenticated && msg.Type != MsgTypeAuth {
		if !h.authManager.TouchSession(client.token) {
			log.Info("Session expired mid-connection, refusing the message")
			client.send(NewAuthFail("session expired, pair again", h.authManager.MaxAttempts()))
			h.logEvent(eventlog.Event{Type: eventlog.EventDisconnect, Message: "Session expired"})
			client.close()
			h.removeClient(client)
			return
		}
	}

	switch msg.Type {
	case MsgTypeAuth:
		h.handleAuth(client, msg)
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
			pinRequired := h.pinEnabled && h.pinHash != ""
			h.mu.RUnlock()
			if pinRequired {
				client.send(ServerMessage{Type: MsgTypePinRequired})
				return
			}
			h.Disarm()
		case MsgTypeDisarmPin:
			if err := h.DisarmWithPin(client.remoteAddr, msg.Pin); err != nil {
				client.send(ServerMessage{Type: MsgTypeAuthFail, Reason: err.Error()})
				return
			}
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
		case MsgTypeLocationAnchor:
			h.handleLocationAnchor(msg)
		case MsgTypeGetLocation:
			client.send(NewLocation(h.LocationPayload()))
		case MsgTypeUpdateConfig:
			h.handleUpdateConfig(msg, client)
		case MsgTypeResetConfig:
			h.handleResetConfig(msg, client)
		case MsgTypeDismissAlarm:
			triggered := h.clearAlarm()
			if triggered == "input" {
				h.mu.Lock()
				h.suppressedSensors["input"] = time.Now().Add(5 * time.Second)
				h.mu.Unlock()
			}
			log.Info("Alarm dismissed from client")

		// Pausing and disabling a sensor are answers to an alarm that just
		// fired, and the phone only offers them from the alert overlay. Which
		// sensor they act on is the one the hub recorded as having triggered,
		// never the name the message carried: taking the client's word for it
		// let a paired phone switch the sensors off one at a time while the
		// machine was armed and no alarm was sounding — the effect of
		// disarming, without the PIN that disarming asks for.
		case MsgTypeDismissAlarmPause:
			sensor := h.clearAlarm()
			if sensor == "" {
				log.Info("Ignoring a sensor pause that answers no alarm")
				break
			}
			duration := clampPauseSeconds(msg.Duration)
			h.sensorMgr.Disable(sensor)
			name, d := sensor, duration
			safe.Go("sensor-unpause:"+name, func() {
				time.Sleep(time.Duration(d) * time.Second)
				h.sensorMgr.Enable(name)
				if h.IsArmed() {
					h.sensorMgr.StartEnabled()
				}
				h.broadcastStatus()
			})
			h.broadcastStatus()
			log.WithField("sensor", sensor).Infof("Alarm dismissed, sensor paused for %ds", duration)

		case MsgTypeDismissAlarmDisable:
			sensor := h.clearAlarm()
			if sensor == "" {
				log.Info("Ignoring a sensor disable that answers no alarm")
				break
			}
			h.sensorMgr.Disable(sensor)
			h.broadcastStatus()
			log.WithField("sensor", sensor).Info("Alarm dismissed, sensor permanently disabled")
		}
	}
}

// How long "pause this sensor" may switch a sensor off for. The phone offers
// five seconds; the ceiling is here because the duration arrives over the wire,
// and an unbounded one would leave a sensor off for the rest of the armed
// session while the panel still showed the machine as watched.
const (
	defaultPauseSeconds = 5
	maxPauseSeconds     = 300
)

// clampPauseSeconds keeps a client-supplied pause inside that range.
func clampPauseSeconds(d int) int {
	switch {
	case d <= 0:
		return defaultPauseSeconds
	case d > maxPauseSeconds:
		return maxPauseSeconds
	default:
		return d
	}
}

// clearAlarm resets the alarm state and fires the dismiss callback.
// Returns the sensor that triggered the alarm.
func (h *Hub) clearAlarm() string {
	h.mu.Lock()
	sensor := h.alarmSensor
	h.alarmActive = false
	h.alarmSensor = ""
	h.mu.Unlock()
	h.fireAlarmDismiss()
	return sensor
}

// DismissAlarm stops the alarm everywhere: the laptop's own siren through the
// dismiss callback, and every paired phone through a broadcast.
//
// The message matters because the phone-initiated path never needed one — a
// phone that dismissed knows it did. Anything dismissing from elsewhere has to
// say so out loud.
func (h *Hub) DismissAlarm() {
	h.clearAlarm()

	h.mu.RLock()
	targets := make([]*Client, 0, len(h.clients))
	for client := range h.clients {
		if client.authenticated {
			targets = append(targets, client)
		}
	}
	h.mu.RUnlock()

	// Writes happen with the lock released; see PushAlert.
	msg := ServerMessage{Type: MsgTypeAlarmCleared, Timestamp: time.Now().Unix()}
	for _, client := range targets {
		client.send(msg)
	}
}

// DisarmWithPin verifies the PIN, if one is configured, and disarms.
//
// source names the rate-limit bucket the guesses are counted against: a phone's
// remote address, or "console" for the terminal. Returning an error leaves the
// machine armed.
func (h *Hub) DisarmWithPin(source, pin string) error {
	h.mu.RLock()
	pinEnabled := h.pinEnabled
	pinHash := h.pinHash
	h.mu.RUnlock()

	if pinEnabled && pinHash != "" {
		// The PIN guards the alarm from whoever is holding the device, so
		// guesses are rate-limited like pairing-key attempts.
		if err := h.authManager.CheckPin(source, pin, pinHash); err != nil {
			h.logEvent(eventlog.Event{Type: eventlog.EventPinFail, Message: "Disarm refused: " + err.Error()})
			return err
		}
		// A correct PIN is the only moment the digits are in hand, and
		// therefore the only moment an old hash can be upgraded.
		h.upgradePinHash(pin, pinHash)
	}

	h.Disarm()
	return nil
}

// PinRequired reports whether disarming asks for a PIN.
func (h *Hub) PinRequired() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.pinEnabled && h.pinHash != ""
}

func (h *Hub) handleAuth(client *Client, msg ClientMessage) {
	token, remaining, err := h.authManager.Authenticate(client.remoteAddr, msg.Key)
	if err != nil {
		client.send(NewAuthFail(err.Error(), remaining))
		h.logEvent(eventlog.Event{Type: eventlog.EventAuthFail, Message: "Pairing refused: " + err.Error()})
		return
	}

	// Pairing again on a socket that is already paired replaces the session,
	// so the one being replaced has to be given back. Overwriting the token and
	// leaving the old session in the table let one socket hold as many sessions
	// as it cared to ask for — and since the cap is checked before the key is
	// compared, filling it locks every other phone out with "maximum
	// connections reached", the owner's included.
	if client.token != "" && client.token != token {
		h.authManager.RemoveSession(client.token)
	}

	client.authenticated = true
	client.token = token
	// Paired clients are accounted for by the session cap from here on, so the
	// unpaired slot goes back to whoever is waiting for one.
	h.releasePending(client)

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
	client.send(NewAuthOK(token, infos, h.version, h.IsArmed(), h.ArmedAt()))

	// A phone can pair hours after the check ran, so the known result is told to
	// it on arrival rather than only broadcast at the moment it was found.
	h.mu.RLock()
	pending := h.updateAvailable
	h.mu.RUnlock()
	if pending != nil {
		client.send(ServerMessage{Type: MsgTypeUpdateAvailable, Update: pending})
	}

	h.logEvent(eventlog.Event{Type: eventlog.EventConnect, Message: "Client authenticated"})
}

// SetUpdateAvailable records that a newer release exists and tells every
// authenticated client.
//
// The result is kept so a phone pairing later still hears about it. Calling this
// with the same version twice is harmless but pointless — the caller suppresses
// repeats, because being told once per release is information and once per check
// is nagging.
func (h *Hub) SetUpdateAvailable(p UpdatePayload) {
	h.mu.Lock()
	h.updateAvailable = &p
	targets := make([]*Client, 0, len(h.clients))
	for client := range h.clients {
		if client.authenticated {
			targets = append(targets, client)
		}
	}
	h.mu.Unlock()

	// Writes happen with the lock released; see PushAlert.
	msg := ServerMessage{Type: MsgTypeUpdateAvailable, Update: &p}
	for _, client := range targets {
		client.send(msg)
	}
}

func (h *Hub) handleConfigure(msg ClientMessage) {
	if msg.Sensors == nil {
		return
	}
	if h.IsArmed() {
		h.PushAlert(NewAlert("system", "warning", "Cannot change sensors while armed — disarm first"))
		return
	}
	for name, enabled := range msg.Sensors {
		if enabled {
			h.sensorMgr.Enable(name)
		} else {
			h.sensorMgr.Disable(name)
		}
	}

	// Persist sensor preferences to config.
	h.mu.Lock()
	cfg := h.cfg
	if cfg != nil {
		if cfg.EnabledSensors == nil {
			cfg.EnabledSensors = make(map[string]bool)
		}
		for name, enabled := range msg.Sensors {
			cfg.EnabledSensors[name] = enabled
		}
		snapshot := *cfg
		h.mu.Unlock()
		if err := config.Save(&snapshot); err != nil {
			log.Errorf("Failed to save sensor config: %v", err)
		}
	} else {
		h.mu.Unlock()
	}

	h.broadcastStatus()
}

// handleLocationAnchor records the phone's own position as a stand-in for the
// laptop's. The two are in the same place when the user arms the system, which
// makes this the most precise statement available about where the laptop is.
func (h *Hub) handleLocationAnchor(msg ClientMessage) {
	h.mu.RLock()
	tracker := h.tracker
	anchorEnabled := h.cfg == nil || h.cfg.Location.PhoneAnchor
	h.mu.RUnlock()

	if tracker == nil || !anchorEnabled {
		return
	}

	fix := anchorFromWire(msg.Location)
	if fix == nil {
		log.Warn("Ignoring location anchor with coordinates outside the valid range")
		return
	}
	tracker.SetAnchor(fix)
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

func (h *Hub) handleUpdateConfig(msg ClientMessage, client *Client) {
	if msg.Config == nil {
		return
	}
	p := msg.Config

	h.mu.Lock()
	cfg := h.cfg
	if cfg == nil {
		h.mu.Unlock()
		return
	}

	// If PIN protection is currently enabled, require the current PIN
	// to modify PIN settings (disable, change PIN).
	pinActive := cfg.PinProtection.Enabled && cfg.PinProtection.PinHash != ""
	changingPin := !p.PinProtection.Enabled != !cfg.PinProtection.Enabled || p.PinProtection.Pin != ""
	if pinActive && changingPin {
		if err := h.authManager.CheckPin(client.remoteAddr, msg.Pin, cfg.PinProtection.PinHash); err != nil {
			h.mu.Unlock()
			h.logEvent(eventlog.Event{Type: eventlog.EventPinFail, Message: "PIN settings change refused: " + err.Error()})
			client.send(ServerMessage{Type: MsgTypePinRequired})
			return
		}
	}

	oldRemote := false
	if cfg.RemoteAccess != nil {
		oldRemote = *cfg.RemoteAccess
	}
	needsRestart := p.Port != cfg.Port || (p.ConnectionMode != "" && p.ConnectionMode != cfg.ConnectionMode) ||
		p.RemoteAccess != oldRemote || (p.RemotePort != 0 && p.RemotePort != cfg.RemotePort)

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

	// The update check and its channel take effect on the next scheduled check
	// rather than forcing one, so switching to beta from the phone is a quiet
	// change. An unrecognized channel is dropped rather than stored: Validate
	// would only reset it on the next start, and until then the phone would be
	// showing a channel that is not in force.
	uc := p.UpdateCheck
	cfg.UpdateCheck = &uc
	switch p.UpdateChannel {
	case "stable", "beta":
		cfg.UpdateChannel = p.UpdateChannel
	}

	if p.PinProtection.Pin != "" {
		// Only the hash is kept. The PIN itself exists in memory for the
		// duration of this call and never touches the disk.
		if hash, err := auth.HashPin(p.PinProtection.Pin); err != nil {
			log.Errorf("Failed to hash PIN: %v", err)
		} else {
			cfg.PinProtection.PinHash = hash
			cfg.PinProtection.Pin = ""
		}
	}
	cfg.PinProtection.Enabled = p.PinProtection.Enabled
	pinEnabled := cfg.PinProtection.Enabled
	pinHash := cfg.PinProtection.PinHash
	autoArm := cfg.AutoArmOnLock

	ra := p.RemoteAccess
	cfg.RemoteAccess = &ra
	if p.RemotePort > 0 {
		cfg.RemotePort = p.RemotePort
	}

	// Sensors are left alone while the machine is armed, exactly as the
	// dedicated `configure` message already refuses them. Without this, the
	// settings screen was a way around the rule: a client could switch every
	// sensor off mid-watch and reach the silence that disarming gives, without
	// presenting the PIN that disarming asks for.
	sensorsRefused := false
	if p.EnabledSensors != nil {
		if h.armed && h.sensorsWouldChange(p.EnabledSensors) {
			sensorsRefused = true
		} else {
			cfg.EnabledSensors = p.EnabledSensors
		}
	}
	applySensors := p.EnabledSensors != nil && !sensorsRefused

	// Location changes take effect on the next arm, except for the API key,
	// which the client never receives: an empty key here means "leave it
	// alone", not "clear it". Clearing is done by turning Wi-Fi off.
	locationChanged := p.Location.Enabled != cfg.Location.Enabled ||
		p.Location.WiFiEnabled != cfg.Location.WiFiEnabled ||
		p.Location.IPFallback != cfg.Location.IPFallback ||
		p.Location.PollSeconds != cfg.Location.PollSeconds
	cfg.Location.Enabled = p.Location.Enabled
	cfg.Location.PhoneAnchor = p.Location.PhoneAnchor
	cfg.Location.IPFallback = p.Location.IPFallback
	cfg.Location.WiFiEnabled = p.Location.WiFiEnabled
	if p.Location.PollSeconds > 0 {
		cfg.Location.PollSeconds = p.Location.PollSeconds
	}
	geoURLRejected := false
	if p.Location.GeolocateURL != "" && p.Location.GeolocateURL != cfg.Location.GeolocateURL {
		if strings.HasPrefix(p.Location.GeolocateURL, "https://") {
			// A new endpoint must not inherit the stored API key: a client
			// could otherwise point the laptop at a server it controls and
			// collect the key on the next Wi-Fi resolve. Changing the endpoint
			// means supplying the key that goes with it.
			if p.Location.GeolocateKey == "" {
				cfg.Location.GeolocateKey = ""
			}
			cfg.Location.GeolocateURL = p.Location.GeolocateURL
		} else {
			// The API key travels in this URL's query string, so plain HTTP
			// would put it on the wire in cleartext.
			geoURLRejected = true
		}
	}
	if p.Location.GeolocateKey != "" {
		cfg.Location.GeolocateKey = p.Location.GeolocateKey
	}

	// The phone is a client like any other and its numbers are not trusted any
	// further than a hand-edited file's would be. A zero heartbeat here would
	// be a ticker panic on the next restart; a million-second lockout would be
	// the owner locked out of their own alarm.
	adjustments := cfg.Validate()

	snapshot := *cfg
	h.mu.Unlock()

	for _, note := range adjustments {
		log.Warnf("Config from client adjusted: %s", note)
	}

	if err := config.Save(&snapshot); err != nil {
		log.Errorf("Failed to save config: %v", err)
	}

	h.SetPinProtection(pinEnabled, pinHash)
	h.SetAutoArmOnLock(autoArm)

	if applySensors {
		for name, enabled := range p.EnabledSensors {
			if enabled {
				h.sensorMgr.Enable(name)
			} else {
				h.sensorMgr.Disable(name)
			}
		}
	}

	h.broadcastStatus()

	if sensorsRefused {
		h.PushAlert(NewAlert("system", "warning", "Cannot change sensors while armed — disarm first"))
	}
	if needsRestart {
		h.PushAlert(NewAlert("system", "warning", "Port changed — restart required to take effect"))
	}
	if geoURLRejected {
		h.PushAlert(NewAlert("system", "warning", "Geolocation endpoint must use https:// — change ignored"))
	}
	if locationChanged {
		// The tracker's providers are built once at startup from this config,
		// so a source being switched on or off does not take effect until then.
		h.PushAlert(NewAlert("system", "warning", "Location settings changed — restart required to take effect"))
	}

	log.Info("Configuration updated from client")
}

func (h *Hub) handleResetConfig(msg ClientMessage, client *Client) {
	defaults := config.Default()

	h.mu.Lock()
	cfg := h.cfg
	if cfg == nil {
		h.mu.Unlock()
		return
	}

	// Require current PIN to reset config when PIN protection is active.
	if cfg.PinProtection.Enabled && cfg.PinProtection.PinHash != "" {
		if err := h.authManager.CheckPin(client.remoteAddr, msg.Pin, cfg.PinProtection.PinHash); err != nil {
			h.mu.Unlock()
			h.logEvent(eventlog.Event{Type: eventlog.EventPinFail, Message: "Config reset refused: " + err.Error()})
			client.send(ServerMessage{Type: MsgTypePinRequired})
			return
		}
	}

	oldPort := cfg.Port
	oldMode := cfg.ConnectionMode

	*cfg = *defaults
	cfg.EnabledSensors = nil

	snapshot := *cfg
	h.mu.Unlock()

	if err := config.Save(&snapshot); err != nil {
		log.Errorf("Failed to save config: %v", err)
	}

	h.SetPinProtection(defaults.PinProtection.Enabled, defaults.PinProtection.PinHash)
	h.SetAutoArmOnLock(defaults.AutoArmOnLock)

	payload := configToPayload(cfg)
	client.send(ServerMessage{
		Type:   MsgTypeConfigData,
		Config: &payload,
	})
	h.broadcastStatus()

	if oldPort != cfg.Port || oldMode != cfg.ConnectionMode {
		h.PushAlert(NewAlert("system", "warning", "Port changed — restart required to take effect"))
	}

	log.Info("Configuration reset to defaults")
}

// sensorsWouldChange reports whether want asks for a sensor state the machine
// does not already have.
//
// The settings screen sends the whole configuration back on every save, so the
// sensor map arrives unchanged far more often than not. Comparing rather than
// refusing on sight keeps saving an unrelated setting from being rejected —
// and from raising a warning about sensors nobody touched — while armed.
//
// The comparison is against the sensor manager rather than against the stored
// config, because the manager is what is actually watching. A config that has
// never recorded a preference holds an empty map, and measured against that a
// request to switch a running sensor off reads as no change at all — which is
// the whole thing this guard exists to stop.
func (h *Hub) sensorsWouldChange(want map[string]bool) bool {
	for name, enabled := range want {
		if h.sensorMgr.IsEnabled(name) != enabled {
			return true
		}
	}
	return false
}

func configToPayload(cfg *config.Config) ConfigPayload {
	remoteAccess := false
	if cfg.RemoteAccess != nil {
		remoteAccess = *cfg.RemoteAccess
	}
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
		UpdateCheck:            cfg.UpdateCheckEnabled(),
		UpdateChannel:          cfg.UpdateChannel,
		UpdateCheckHours:       cfg.UpdateCheckHours,
		Alarm:                  cfg.Alarm,
		PinProtection: PinProtectionPayload{
			Enabled: cfg.PinProtection.Enabled,
			HasPin:  cfg.PinProtection.PinHash != "",
		},
		EnabledSensors: cfg.EnabledSensors,
		RemoteAccess:   remoteAccess,
		RemotePort:     cfg.RemotePort,
		Location: LocationConfigPayload{
			Enabled:      cfg.Location.Enabled,
			PollSeconds:  cfg.Location.PollSeconds,
			PhoneAnchor:  cfg.Location.PhoneAnchor,
			IPFallback:   cfg.Location.IPFallback,
			WiFiEnabled:  cfg.Location.WiFiEnabled,
			GeolocateURL: cfg.Location.GeolocateURL,
			// The key itself never goes out, only whether one is set. Same
			// treatment as the PIN.
			HasKey: cfg.Location.GeolocateKey != "",
		},
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
	goneCb := h.onAllDisconnected
	changeCb := h.onClientChange
	grace := h.disconnectGracePeriod
	h.mu.Unlock()

	if changeCb != nil {
		changeCb(clientCount, armed)
	}

	h.logEvent(eventlog.Event{Type: eventlog.EventDisconnect, Message: "Client disconnected"})

	if armed && clientCount == 0 && goneCb != nil {
		// The grace period is a debounce, not a countdown to anything. A phone
		// that reconnects inside it never really left, and saying so would turn
		// every reload into a line in the log.
		safe.Go("all-disconnected", func() {
			time.Sleep(grace)
			h.mu.RLock()
			count := len(h.clients)
			isArmed := h.armed
			h.mu.RUnlock()
			if count == 0 && isArmed {
				h.logEvent(eventlog.Event{
					Type:    eventlog.EventDisconnect,
					Message: "Every phone disconnected while armed; monitoring continues",
				})
				goneCb()
			}
		})
	}
}

func (h *Hub) broadcastStatus() {
	h.mu.RLock()
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
	targets := make([]*Client, 0, len(h.clients))
	for client := range h.clients {
		if client.authenticated {
			targets = append(targets, client)
		}
	}
	h.mu.RUnlock()

	// Writes happen with the lock released; see PushAlert.
	for _, client := range targets {
		client.send(msg)
	}
}
