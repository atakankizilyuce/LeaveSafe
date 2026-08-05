package ws

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	log "github.com/sirupsen/logrus"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/leavesafe/leavesafe/internal/auth"
	"github.com/leavesafe/leavesafe/internal/config"
	"github.com/leavesafe/leavesafe/internal/eventlog"
	"github.com/leavesafe/leavesafe/internal/location"
	"github.com/leavesafe/leavesafe/internal/monitor"
	"github.com/leavesafe/leavesafe/internal/remote"
	"github.com/leavesafe/leavesafe/internal/safe"
	"github.com/leavesafe/leavesafe/internal/state"
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

	tracker *location.Tracker
	// startTracking begins location tracking, and is nil until a tracker is
	// installed.
	//
	// The hub held the context this runs under instead, which meant it was
	// asking the wrong question. What the hub needs on arming is "start
	// tracking"; whose lifetime that belongs to is the caller's business, and
	// it is not the hub's to store. Passing a context into Arm instead would
	// have been worse than either: Arm is called from a phone's socket, from
	// the auto-arm path and from the console, so the tracker would have
	// inherited whichever of those happened to arm — and a phone locking its
	// screen would have stopped the laptop tracking itself.
	startTracking func()

	cfg *config.Config

	// Alarm state tracking to prevent re-trigger loops
	alarmActive bool
	alarmSensor string
	// alarmMessage is what the alarm said, kept beside the sensor so a phone
	// that pairs while one is sounding can be shown the same words the phones
	// already connected were.
	alarmMessage      string
	suppressedSensors map[string]time.Time

	// updateAvailable is the newest release the update check found, kept so a
	// phone that pairs after the check still hears about it. Nil until one is
	// found, which is also the state of a copy with checking switched off.
	updateAvailable *UpdatePayload

	// remoteState is what remote access is actually doing, as opposed to what
	// the config says was asked for. The phone shows both: the toggle reflects
	// the request, this reflects reality — which is the only way a router that
	// refused a port mapping can be told apart from a setting nobody enabled.
	remoteState remote.State
	// onRemoteToggle actually starts or stops remote access. Nil in tests and
	// in any embedding with no listener to publish.
	onRemoteToggle func(enable bool)
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

// acquirePending takes an unpaired-socket slot for this client, reporting
// whether one was available.
//
// The address is normalized here and again in releasePending, through the same
// call, because the two sides naming a peer differently is not a bug that shows
// up in the total — it shows up only in the per-address share, months later. It
// happened: acquire counted the bare host and release named the host:port the
// socket arrived with, so the per-address count only ever went up. Four
// reconnects from one phone — a screen locking four times — and the owner was
// refused by their own laptop until the process restarted, while every address
// that had ever connected stayed in the table forever.
func (h *Hub) acquirePending(c *Client) bool {
	if !h.pending.acquire(auth.NormalizeAddr(c.remoteAddr)) {
		return false
	}
	c.pendingHeld = true
	return true
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
	h.pending.release(auth.NormalizeAddr(c.remoteAddr))
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
	// The context is bound to the one thing it scopes rather than kept. It
	// belongs to whoever built the tracker — the program's own lifetime — and
	// arming must not narrow it to whatever asked for the arm.
	h.startTracking = nil
	if tracker != nil {
		h.startTracking = func() { tracker.Start(ctx) }
	}
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

// logAuthFailure records a refused secret, unless the refusal was the lockout
// itself doing its job.
//
// The lockout is written once, when it starts. Every attempt made behind it is
// the same fact restated, and the event log is size-rotated — so writing one
// record per attempt is how a flood erases the history it is supposed to
// preserve: the arm, the sensor alert, the disconnect. Suppressing them keeps
// the log a record of what happened rather than of how loudly someone knocked.
func (h *Hub) logAuthFailure(kind eventlog.EventType, prefix string, err error) {
	if errors.Is(err, auth.ErrLockedOut) {
		return
	}
	h.logEvent(eventlog.Event{Type: kind, Message: prefix + err.Error()})
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

// SetRemoteState records what remote access is currently doing so it can be
// reported to every phone.
func (h *Hub) SetRemoteState(st remote.State) {
	h.mu.Lock()
	h.remoteState = st
	h.mu.Unlock()
	h.broadcastStatus()
}

// SetRemoteToggle installs the callback that actually starts or stops remote
// access. Without one, a change from the phone is recorded and nothing else —
// which is what tests and embeddings with no listener to publish want.
func (h *Hub) SetRemoteToggle(fn func(enable bool)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onRemoteToggle = fn
}

// applyRemoteAccessChange starts or stops remote access when want differs from
// what the config already held.
//
// The settings screen sends the whole config on every save, so the comparison
// is what keeps an unrelated change from bouncing the listener and dropping
// whoever is connected through it.
func (h *Hub) applyRemoteAccessChange(want bool) {
	h.mu.RLock()
	current := h.cfg != nil && h.cfg.RemoteAccess != nil && *h.cfg.RemoteAccess
	fn := h.onRemoteToggle
	h.mu.RUnlock()

	if fn == nil || want == current {
		return
	}
	fn(want)
}

// configPayloadWithRemoteState is configToPayload plus what remote access is
// actually doing.
func (h *Hub) configPayloadWithRemoteState(cfg *config.Config) ConfigPayload {
	payload := configToPayload(cfg)
	h.mu.RLock()
	st := h.remoteState
	h.mu.RUnlock()
	payload.RemoteState = &st
	return payload
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
	start := h.startTracking
	h.mu.Unlock()

	h.sensorMgr.StartEnabled()
	if start != nil {
		start()
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
	start := h.startTracking
	h.mu.Unlock()

	h.persistArmed(true)

	h.sensorMgr.StartEnabled()

	// Location tracking runs only while armed. There is no reason to scan for
	// Wi-Fi or hit a geolocation service while the user is sitting at the
	// machine, and every reason not to.
	if start != nil {
		start()
	}

	h.broadcastStatus()
	h.logEvent(eventlog.Event{Type: eventlog.EventArm, Message: "System armed"})
}

// Disarm deactivates monitoring and stops any active alarm.
func (h *Hub) Disarm() {
	h.mu.Lock()
	h.armed = false
	h.armedAt = time.Time{}
	tracker := h.tracker
	h.mu.Unlock()

	h.persistArmed(false)

	h.sensorMgr.StopAll()
	if tracker != nil {
		// Stop keeps the last fix, so the phone can still show where the
		// machine was rather than an empty panel.
		tracker.Stop()
	}
	// Through clearAlarm rather than by resetting the fields here, so the phones
	// are told. Disarming used to silence the laptop and leave every phone
	// sounding at an alarm the machine had already stopped having.
	h.clearAlarm()
	h.broadcastStatus()
	h.logEvent(eventlog.Event{Type: eventlog.EventDisarm, Message: "System disarmed"})
}

// RegisterExternalClient creates and registers a client using a non-WebSocket
// transport. Such transports have no network address, so they share a single
// rate-limit bucket — which is right for BLE, where every peer is within radio
// range anyway.
//
// onRemove, if not nil, is called once when the hub lets this client go, for
// whatever reason: the transport reporting a disconnect, an expired session, a
// rotated pairing key. A transport that keeps its own table of clients needs it
// to stay in step with the hub — otherwise the hub can retire a client while
// the transport keeps handing the same one back, and a peer that reconnects
// inherits the authentication the previous one had.
func (h *Hub) RegisterExternalClient(transport Transport, onRemove func()) *Client {
	return &Client{
		hub:        h,
		transport:  transport,
		remoteAddr: auth.UnknownAddr,
		onRemove:   onRemove,
	}
}

// HandleExternalMessage processes a message from an external (non-WebSocket)
// client, one message at a time.
//
// A WebSocket client is handled by the goroutine that reads its socket, so its
// messages are already serialized and the state on Client needs no lock. A
// transport with no read loop of its own has no such order: the BLE backend
// hands each incoming write to a fresh goroutine, so several of one phone's
// messages could be inside handleMessage at once.
//
// That is not a crash — it is the pairing-attempt bucket being created twice
// and losing whichever copy had counted the attempts. The bucket is what keeps
// refused attempts from flooding the size-rotated security log, and over BLE it
// is reachable by anything in radio range without a key. The lock is here
// rather than in the transport so the guarantee holds for the next one too.
func (h *Hub) HandleExternalMessage(client *Client, msg ClientMessage) {
	client.handling.Lock()
	defer client.handling.Unlock()
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
	if !h.acquirePending(client) {
		log.Warnf("Refusing a connection from %s: too many unpaired sockets waiting", remoteAddr)
		client.send(NewAuthFail("the laptop is busy, try again in a moment", 0))
		_ = conn.Close(websocket.StatusTryAgainLater, "too many unpaired connections")
		return
	}

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

	// An alarm raised by hand is an alarm. Recording which sensor raised it is
	// what makes the answers to it work: the phone's overlay offers "pause this
	// sensor" and "stop using this sensor", and both act on the sensor the hub
	// recorded rather than on the name the message carried. Without the record
	// they answered no alarm at all — the buttons did nothing, and the only sign
	// was a debug line on the laptop nobody was standing next to.
	armed := false
	h.mu.Lock()
	if h.armed && !h.alarmActive {
		h.alarmActive = true
		h.alarmSensor = sensorName
		h.alarmMessage = message
		armed = true
	}
	h.mu.Unlock()

	if armed {
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
			h.dispatchAlert(alert)
		}
	}
}

// dispatchAlert decides what one alert from a sensor means.
//
// Three questions in order, and each one can end it: was this the screen
// locking, which arms the machine rather than alarming it; is the machine
// watching at all; and is this alert the alarm, or something arriving behind an
// alarm that is already sounding.
func (h *Hub) dispatchAlert(alert monitor.Alert) {
	if h.autoArmed(alert) {
		return
	}
	if !h.IsArmed() {
		return
	}
	if !h.claimAlarm(alert) {
		return
	}
	h.raiseAlarm(alert)
}

// autoArmed arms or disarms on the screen locking, and says whether it did.
//
// Only with a phone still paired. Arming a laptop that nothing can disarm it
// from would leave the owner with a machine that screams at them and no way to
// answer it.
func (h *Hub) autoArmed(alert monitor.Alert) bool {
	h.mu.RLock()
	autoArm := h.autoArmOnLock
	h.mu.RUnlock()

	if !autoArm || alert.Sensor != "screen" || h.ClientCount() == 0 {
		return false
	}
	if strings.Contains(alert.Message, "off") && !h.IsArmed() {
		log.Info("Auto-arming: screen locked")
		h.Arm()
		return true
	}
	if strings.Contains(alert.Message, "on") && h.IsArmed() {
		log.Info("Auto-disarming: screen unlocked")
		h.Disarm()
		return true
	}
	return false
}

// claimAlarm records this alert as the alarm now sounding, and says whether it
// got there first.
//
// Two ways to lose: the sensor is inside the grace period the user bought by
// dismissing its last alarm, or an alarm is already sounding. The second is not
// a nicety — every alert re-fires the siren and the trigger callback, so
// without it one sensor that keeps reporting drives the alarm in a loop.
func (h *Hub) claimAlarm(alert monitor.Alert) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	if until, ok := h.suppressedSensors[alert.Sensor]; ok {
		if time.Now().Before(until) {
			return false
		}
		delete(h.suppressedSensors, alert.Sensor)
	}
	if h.alarmActive {
		return false
	}
	h.alarmActive = true
	h.alarmSensor = alert.Sensor
	h.alarmMessage = alert.Message
	return true
}

// raiseAlarm tells everything that needs to know: the log, every paired phone,
// the siren, and the event log the owner reads afterwards.
func (h *Hub) raiseAlarm(alert monitor.Alert) {
	log.WithFields(log.Fields{"sensor": strings.ToUpper(alert.Sensor)}).Warn(alert.Message)
	h.PushAlert(NewAlert(alert.Sensor, string(alert.Level), alert.Message))
	h.fireAlarmTrigger()
	h.PushAlert(NewAlarmActive(alert.Sensor, alert.Message))
	h.logEvent(eventlog.Event{Type: eventlog.EventAlert, Sensor: alert.Sensor, Message: alert.Message})

	// An alarm is exactly when the position matters, so it goes out with the
	// alert rather than waiting for the next poll.
	if payload := h.LocationPayload(); payload.Enabled {
		h.PushAlert(NewLocation(payload))
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
		// AvailableNow rather than Available: this list goes out with the reply
		// to a pairing key, and on Windows asking the lid sensor directly starts
		// a WMI query allowed twenty seconds. The phone gives up after ten.
		available, _ := h.sensorMgr.AvailableNow(s)
		infos = append(infos, SensorInfo{
			Name:        s.Name(),
			DisplayName: s.DisplayName(),
			Available:   available,
			Enabled:     h.sensorMgr.IsEnabled(s.Name()),
			Failure:     h.sensorMgr.Failure(s.Name()),
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
	if !h.sessionStillLive(client, msg) {
		return
	}
	if !withinRateLimit(client, msg) {
		return
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
		h.handlePairedMessage(client, msg)
	}
}

// sessionStillLive touches the session this message arrived on and says whether
// it is still good.
//
// Anything an authenticated client sends is activity, and activity is what the
// idle timeout measures. Pairing itself is excluded: there is no session to
// touch yet.
func (h *Hub) sessionStillLive(client *Client, msg ClientMessage) bool {
	if !client.authenticated || msg.Type == MsgTypeAuth {
		return true
	}
	if h.authManager.TouchSession(client.token) {
		return true
	}

	log.Info("Session expired mid-connection, refusing the message")
	client.send(NewAuthFail("session expired, pair again", h.authManager.MaxAttempts()))
	h.logEvent(eventlog.Event{Type: eventlog.EventDisconnect, Message: "Session expired"})
	client.close()
	h.removeClient(client)
	return false
}

// withinRateLimit meters how fast one client may send.
//
// Past this point a message costs something — a disk write, a broadcast to
// every paired phone, the siren — so how fast they can arrive is bounded.
//
// Pairing is metered separately rather than not at all. It cannot share the
// other bucket: a flood from a paired client would eat into the allowance a
// stranger's guesses are counted against. But it needs a bucket of its own,
// because every refused attempt writes a line to the security event log, and
// that log is size-rotated — so an unpaired peer sending attempts at network
// speed could push every genuine record out of it and erase the history of
// whatever it had just done. The per-address lockout does not stop this: a
// locked-out address is still refused once per message, and the refusal is what
// gets written.
//
// Over the limit the message is dropped rather than the client: a phone whose
// script has run away is still the owner's phone, and the alarm is the last
// thing that should be disconnected for it.
func withinRateLimit(client *Client, msg ClientMessage) bool {
	if msg.Type == MsgTypeAuth {
		if client.allowAuth() {
			return true
		}
		log.Debug("Dropping a pairing attempt from a client that is sending too fast")
		return false
	}
	if client.allowMessage() {
		return true
	}
	log.WithField("type", msg.Type).Debug("Dropping a message from a client that is sending too fast")
	return false
}

// handlePairedMessage acts on a message from a client that has proved itself.
func (h *Hub) handlePairedMessage(client *Client, msg ClientMessage) {
	switch msg.Type {
	case MsgTypeArm:
		h.Arm()
	case MsgTypeDisarm:
		h.handleDisarm(client)
	case MsgTypeDisarmPin:
		if err := h.DisarmWithPin(client.remoteAddr, msg.Pin); err != nil {
			client.send(ServerMessage{Type: MsgTypeAuthFail, Reason: err.Error()})
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
		h.dismissAlarm()
	case MsgTypeDismissAlarmPause:
		h.pauseAlarmSensor(msg)
	case MsgTypeDismissAlarmDisable:
		h.disableAlarmSensor()
	}
}

// handleDisarm switches the watch off, unless a PIN stands in the way.
func (h *Hub) handleDisarm(client *Client) {
	h.mu.RLock()
	pinRequired := h.pinEnabled && h.pinHash != ""
	h.mu.RUnlock()

	if pinRequired {
		client.send(ServerMessage{Type: MsgTypePinRequired})
		return
	}
	h.Disarm()
}

// dismissAlarm answers a sounding alarm.
//
// Dismissing it from the phone means picking the laptop up, and on the input
// sensor that is itself input — so the sensor that raised this alarm is held
// quiet for a moment rather than re-raising it on the movement that cleared it.
func (h *Hub) dismissAlarm() {
	if h.clearAlarm() == "input" {
		h.mu.Lock()
		h.suppressedSensors["input"] = time.Now().Add(5 * time.Second)
		h.mu.Unlock()
	}
	log.Info("Alarm dismissed from client")
}

// Pausing and disabling a sensor are answers to an alarm that just fired, and
// the phone only offers them from the alert overlay. Which sensor they act on
// is the one the hub recorded as having triggered, never the name the message
// carried: taking the client's word for it let a paired phone switch the
// sensors off one at a time while the machine was armed and no alarm was
// sounding — the effect of disarming, without the PIN that disarming asks for.
func (h *Hub) pauseAlarmSensor(msg ClientMessage) {
	sensor := h.clearAlarm()
	if sensor == "" {
		log.Info("Ignoring a sensor pause that answers no alarm")
		return
	}

	duration := clampPauseSeconds(msg.Duration)
	h.sensorMgr.Disable(sensor)
	safe.Go("sensor-unpause:"+sensor, func() { h.unpauseSensor(sensor, duration) })
	h.broadcastStatus()
	log.WithField("sensor", sensor).Infof("Alarm dismissed, sensor paused for %ds", duration)
}

// unpauseSensor puts a paused sensor back to work once its time is up, and
// starts it again if the machine is still being watched.
func (h *Hub) unpauseSensor(sensor string, seconds int) {
	time.Sleep(time.Duration(seconds) * time.Second)
	h.sensorMgr.Enable(sensor)
	if h.IsArmed() {
		h.sensorMgr.StartEnabled()
	}
	h.broadcastStatus()
}

// disableAlarmSensor stops using the sensor that raised the alarm, for good.
// See pauseAlarmSensor for why the sensor is the hub's rather than the
// client's.
func (h *Hub) disableAlarmSensor() {
	sensor := h.clearAlarm()
	if sensor == "" {
		log.Info("Ignoring a sensor disable that answers no alarm")
		return
	}

	h.sensorMgr.Disable(sensor)
	h.broadcastStatus()
	log.WithField("sensor", sensor).Info("Alarm dismissed, sensor permanently disabled")
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

// clearAlarm resets the alarm state, stops the laptop's own siren and tells
// every paired phone. Returns the sensor that triggered the alarm.
//
// The broadcast is here rather than at one call site because an alarm is one
// event on several devices, and only one of them knows it has been called off.
// It used to be sent only when the console dismissed, on the reasoning that a
// phone that dismissed knows it did — which is true of that phone and of
// nothing else. A second paired phone went on sounding at an alarm that had
// been answered, its overlay offering to pause a sensor that was no longer
// alarming; and disarming while the siren ran silenced the laptop and left
// every phone screaming, because Disarm cleared the state without saying so.
func (h *Hub) clearAlarm() string {
	h.mu.Lock()
	sensor := h.alarmSensor
	h.alarmActive = false
	h.alarmSensor = ""
	h.alarmMessage = ""
	h.mu.Unlock()

	h.fireAlarmDismiss()
	h.PushAlert(ServerMessage{Type: MsgTypeAlarmCleared, Timestamp: time.Now().Unix()})
	return sensor
}

// DismissAlarm stops the alarm everywhere: the laptop's own siren and every
// paired phone. Used by the console's `stop` command.
func (h *Hub) DismissAlarm() {
	h.clearAlarm()
}

// activeAlarm reports the alarm currently sounding, if one is.
//
// It exists for a phone that has just paired. A phone drops its socket every
// time its screen locks and the page behind it is thrown away, so the phone
// that reconnects into a sounding alarm is the ordinary case, not the odd one —
// and it used to arrive at a calm panel with no overlay and no siren while the
// laptop screamed on the table.
//
// That was worse than a missed notification. The hub suppresses further events
// while an alarm is active, so the alarm nobody could see was also the reason
// the next one never fired: the user, seeing nothing to dismiss, dismissed
// nothing, and the machine stayed silent from then on.
func (h *Hub) activeAlarm() (sensor, message string, ok bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.alarmSensor, h.alarmMessage, h.alarmActive
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
			h.logAuthFailure(eventlog.EventPinFail, "Disarm refused: ", err)
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
		// An attempt made against an address that is already locked out is not
		// worth a record. The lockout was written when it started, and the
		// attempts behind it are the flood — writing one per attempt is how the
		// history gets pushed out of a size-rotated log. The client is still
		// told, so a phone whose owner is waiting out a lockout still sees why.
		h.logAuthFailure(eventlog.EventAuthFail, "Pairing refused: ", err)
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

	// An alarm already sounding is the first thing this phone needs, before the
	// update notice and before anything else. A phone reconnects every time its
	// screen unlocks, so arriving in the middle of an alarm is the ordinary
	// case — and arriving to a calm panel meant the one screen the user is
	// holding showed nothing wrong while the laptop screamed on the table.
	if sensor, message, active := h.activeAlarm(); active {
		client.send(NewAlarmActive(sensor, message))
	}

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
		h.PushAlert(NewAlert(SensorSystem, "warning", "Cannot change sensors while armed — disarm first"))
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
	payload := h.configPayloadWithRemoteState(cfg)
	client.send(ServerMessage{
		Type:   MsgTypeConfigData,
		Config: &payload,
	})
}

// configOutcome is what a settings update did.
//
// It exists because almost nothing the update decides can be acted on where it
// is decided: writing the file, telling the sensor manager, broadcasting to
// every phone and bringing an internet-facing listener up all have to happen
// with the hub's lock released. So the decisions are gathered under the lock
// and carried out of it.
type configOutcome struct {
	snapshot    config.Config
	adjustments []string

	pinEnabled bool
	pinHash    string
	autoArm    bool

	// sensors is what to tell the sensor manager, and is nil when the update
	// asked for no sensor change or when the change was refused.
	sensors        map[string]bool
	sensorsRefused bool

	needsRestart    bool
	locationChanged bool
	geoURLRejected  bool
	remoteChanged   bool
	remoteWanted    bool
}

func (h *Hub) handleUpdateConfig(msg ClientMessage, client *Client) {
	if msg.Config == nil {
		return
	}

	out, err := h.applyConfigUpdate(msg, client)
	if err != nil {
		h.logAuthFailure(eventlog.EventPinFail, "PIN settings change refused: ", err)
		client.send(ServerMessage{Type: MsgTypePinRequired})
		return
	}
	if out == nil {
		return
	}

	for _, note := range out.adjustments {
		log.Warnf("Config from client adjusted: %s", note)
	}
	if err := config.Save(&out.snapshot); err != nil {
		log.Errorf("Failed to save config: %v", err)
	}

	h.SetPinProtection(out.pinEnabled, out.pinHash)
	h.SetAutoArmOnLock(out.autoArm)
	for name, enabled := range out.sensors {
		if enabled {
			h.sensorMgr.Enable(name)
		} else {
			h.sensorMgr.Disable(name)
		}
	}
	h.broadcastStatus()
	h.announceConfigChanges(out)

	log.Info("Configuration updated from client")
}

// applyConfigUpdate writes the client's settings into the live config and
// reports what changed.
//
// Everything here happens under the lock, and so nothing here may touch the
// disk, the network, the sensors or the event log — logEvent takes the same
// lock, and it is not reentrant. A refused PIN is returned rather than
// answered, so the refusal is written and sent from outside.
//
// A nil outcome with no error means there was no config to update.
func (h *Hub) applyConfigUpdate(msg ClientMessage, client *Client) (*configOutcome, error) {
	p := msg.Config

	h.mu.Lock()
	defer h.mu.Unlock()

	cfg := h.cfg
	if cfg == nil {
		return nil, nil
	}

	// If PIN protection is currently enabled, require the current PIN to modify
	// PIN settings (disable, change PIN).
	pinActive := cfg.PinProtection.Enabled && cfg.PinProtection.PinHash != ""
	changingPin := !p.PinProtection.Enabled != !cfg.PinProtection.Enabled || p.PinProtection.Pin != ""
	if pinActive && changingPin {
		if err := h.authManager.CheckPin(client.remoteAddr, msg.Pin, cfg.PinProtection.PinHash); err != nil {
			return nil, err
		}
	}

	out := &configOutcome{}
	out.needsRestart = applyServerSettings(cfg, p)
	out.remoteChanged, out.remoteWanted = applyRemoteSettings(cfg, p)
	applyPinSettings(cfg, p)
	out.sensors, out.sensorsRefused = h.applySensorSettings(cfg, p)
	out.locationChanged, out.geoURLRejected = applyLocationSettings(cfg, p)

	// The phone is a client like any other and its numbers are not trusted any
	// further than a hand-edited file's would be. A zero heartbeat here would
	// be a ticker panic on the next restart; a million-second lockout would be
	// the owner locked out of their own alarm.
	out.adjustments = cfg.Validate()

	out.snapshot = *cfg
	out.pinEnabled = cfg.PinProtection.Enabled
	out.pinHash = cfg.PinProtection.PinHash
	out.autoArm = cfg.AutoArmOnLock
	return out, nil
}

// applyServerSettings copies the plain settings across and says whether any of
// them will only take effect on the next start.
func applyServerSettings(cfg *config.Config, p *ConfigPayload) bool {
	// Remote access and its port are not on this list any more: they are applied
	// while the program runs, on a listener of their own, so asking the user to
	// restart for them would be asking for something that achieves nothing.
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

	return needsRestart
}

// applyRemoteSettings records what the client asked of the internet-facing
// listener, and says whether that is a change and what was wanted.
//
// It reads the old values before it writes the new ones, so it has to run
// before anything else touches them.
func applyRemoteSettings(cfg *config.Config, p *ConfigPayload) (changed, wanted bool) {
	old := cfg.RemoteAccess != nil && *cfg.RemoteAccess
	wanted = p.RemoteAccess
	changed = wanted != old || (p.RemotePort > 0 && p.RemotePort != cfg.RemotePort)

	ra := wanted
	cfg.RemoteAccess = &ra
	if p.RemotePort > 0 {
		cfg.RemotePort = p.RemotePort
	}
	return changed, wanted
}

// applyPinSettings stores a new PIN as a hash, and never as itself.
func applyPinSettings(cfg *config.Config, p *ConfigPayload) {
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
}

// applySensorSettings stores which sensors the client wants watching, and
// returns what to tell the sensor manager once the lock is released.
//
// Sensors are left alone while the machine is armed, exactly as the dedicated
// `configure` message already refuses them. Without this, the settings screen
// was a way around the rule: a client could switch every sensor off mid-watch
// and reach the silence that disarming gives, without presenting the PIN that
// disarming asks for.
func (h *Hub) applySensorSettings(cfg *config.Config, p *ConfigPayload) (apply map[string]bool, refused bool) {
	if p.EnabledSensors == nil {
		return nil, false
	}
	if h.armed && h.sensorsWouldChange(p.EnabledSensors) {
		return nil, true
	}
	cfg.EnabledSensors = p.EnabledSensors
	return p.EnabledSensors, false
}

// applyLocationSettings stores where the laptop may look itself up, and says
// whether that needs a restart and whether an endpoint was refused.
//
// Location changes take effect on the next arm, except for the API key, which
// the client never receives: an empty key here means "leave it alone", not
// "clear it". Clearing is done by turning Wi-Fi off.
func applyLocationSettings(cfg *config.Config, p *ConfigPayload) (changed, urlRejected bool) {
	changed = p.Location.Enabled != cfg.Location.Enabled ||
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

	urlRejected = !applyGeolocateURL(cfg, p)
	if p.Location.GeolocateKey != "" {
		cfg.Location.GeolocateKey = p.Location.GeolocateKey
	}
	return changed, urlRejected
}

// applyGeolocateURL takes a new lookup endpoint, and says whether it was
// acceptable. An unchanged or absent endpoint is acceptable by definition.
func applyGeolocateURL(cfg *config.Config, p *ConfigPayload) bool {
	if p.Location.GeolocateURL == "" || p.Location.GeolocateURL == cfg.Location.GeolocateURL {
		return true
	}
	if !strings.HasPrefix(p.Location.GeolocateURL, "https://") {
		// The API key travels in this URL's query string, so plain HTTP would
		// put it on the wire in cleartext.
		return false
	}
	// A new endpoint must not inherit the stored API key: a client could
	// otherwise point the laptop at a server it controls and collect the key on
	// the next Wi-Fi resolve. Changing the endpoint means supplying the key that
	// goes with it.
	if p.Location.GeolocateKey == "" {
		cfg.Location.GeolocateKey = ""
	}
	cfg.Location.GeolocateURL = p.Location.GeolocateURL
	return true
}

// announceConfigChanges tells the phones what saving the settings did not: what
// was refused, and what will not take effect until the laptop is restarted.
func (h *Hub) announceConfigChanges(out *configOutcome) {
	if out.sensorsRefused {
		h.PushAlert(NewAlert(SensorSystem, "warning", "Cannot change sensors while armed — disarm first"))
	}
	if out.needsRestart {
		h.PushAlert(NewAlert(SensorSystem, "warning",
			"The port or the Bluetooth mode changed — restart required to take effect"))
	}
	if out.remoteChanged {
		h.toggleRemoteAccess(out.remoteWanted)
	}
	if out.geoURLRejected {
		h.PushAlert(NewAlert(SensorSystem, "warning", "Geolocation endpoint must use https:// — change ignored"))
	}
	if out.locationChanged {
		// The tracker's providers are built once at startup from this config,
		// so a source being switched on or off does not take effect until then.
		h.PushAlert(NewAlert(SensorSystem, "warning", "Location settings changed — restart required to take effect"))
	}
}

// toggleRemoteAccess hands the change to whoever owns the internet-facing
// listener, on its own goroutine: enabling asks the router for a port mapping
// and the internet for an address, which can take seconds, and the phone that
// sent this is waiting for its settings to be saved.
func (h *Hub) toggleRemoteAccess(wanted bool) {
	h.mu.RLock()
	fn := h.onRemoteToggle
	h.mu.RUnlock()

	if fn == nil {
		return
	}
	safe.Go("remote-toggle", func() { fn(wanted) })
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
			h.logAuthFailure(eventlog.EventPinFail, "Config reset refused: ", err)
			client.send(ServerMessage{Type: MsgTypePinRequired})
			return
		}
	}

	oldPort := cfg.Port
	oldMode := cfg.ConnectionMode
	// Defaults leave remote access unset, which means off. Without applying
	// that, "reset everything" would leave an internet-facing port open while
	// the config it just wrote asks for nothing of the sort.
	oldRemote := cfg.RemoteAccess != nil && *cfg.RemoteAccess

	*cfg = *defaults
	cfg.EnabledSensors = nil

	snapshot := *cfg
	h.mu.Unlock()

	if err := config.Save(&snapshot); err != nil {
		log.Errorf("Failed to save config: %v", err)
	}

	h.SetPinProtection(defaults.PinProtection.Enabled, defaults.PinProtection.PinHash)
	h.SetAutoArmOnLock(defaults.AutoArmOnLock)

	// An empty sensor map means every sensor watches, which is what the defaults
	// this just wrote describe. The manager has to be told, or the reset would
	// hold a sensor switched off while the config it produced says nothing of
	// the sort — a disagreement that lasts until the next restart reads the file
	// and quietly switches it back on.
	for _, s := range h.sensorMgr.Sensors() {
		h.sensorMgr.Enable(s.Name())
	}
	if h.IsArmed() {
		h.sensorMgr.StartEnabled()
	}

	payload := h.configPayloadWithRemoteState(cfg)
	client.send(ServerMessage{
		Type:   MsgTypeConfigData,
		Config: &payload,
	})
	h.broadcastStatus()

	if oldPort != cfg.Port || oldMode != cfg.ConnectionMode {
		h.PushAlert(NewAlert(SensorSystem, "warning",
			"The port or the Bluetooth mode changed — restart required to take effect"))
	}
	if oldRemote {
		h.mu.RLock()
		fn := h.onRemoteToggle
		h.mu.RUnlock()
		if fn != nil {
			safe.Go("remote-toggle", func() { fn(false) })
		}
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

	// Told once, under the same lock that retires the client, so a transport
	// keeping its own table cannot be handed a client the hub has let go.
	// removeClient is reachable from the connection's goroutine and from the
	// heartbeat sweep, and both may arrive here for the same client.
	var notifyRemoved func()
	if !client.removed {
		client.removed = true
		notifyRemoved = client.onRemove
	}

	armed := h.armed
	clientCount := len(h.clients)
	goneCb := h.onAllDisconnected
	changeCb := h.onClientChange
	grace := h.disconnectGracePeriod
	h.mu.Unlock()

	if notifyRemoved != nil {
		// Outside the lock: the callback reaches into the transport, and
		// nothing it does should be able to stall the hub.
		safe.Do("client-removed", notifyRemoved)
	}

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
		failure := h.sensorMgr.Failure(s.Name())
		// This runs on the heartbeat, which also carries the alarm. It must not
		// stall behind a sensor working out whether it can run here; a sensor
		// still finding out reads as unavailable and the next beat corrects it.
		available, _ := h.sensorMgr.AvailableNow(s)
		status := "ok"
		switch {
		case !available:
			status = "unavailable"
		case failure != "":
			// Enabled, available, and not actually running. Said out loud rather
			// than folded into "ok", which is what let a dead sensor go on
			// counting towards the tally the user reads before walking away.
			status = "failed"
		}
		states[s.Name()] = &SensorState{
			Enabled: h.sensorMgr.IsEnabled(s.Name()),
			Status:  status,
			Failure: failure,
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
