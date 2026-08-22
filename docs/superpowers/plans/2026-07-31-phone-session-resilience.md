# Phone Session Resilience Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop treating the phone as a guaranteed alert channel — a sleeping phone must not cause a false alarm, a returning phone must resume its session, and the laptop's terminal must be a full control surface.

**Architecture:** Four independent changes to an existing Go + Preact codebase. The Go side stops firing the alarm when the last client drops, starts remembering when it was armed, sends that state in `auth_ok`, and gains `arm`/`disarm`/`status` console commands plus a broadcast-aware alarm dismissal. The Preact side persists the pairing key so a discarded page can reconnect without rescanning, applies the armed state on reconnect, and states the limitation in the interface.

**Tech Stack:** Go 1.25 (stdlib + `nhooyr.io/websocket` + `logrus`), Preact with `@preact/signals`, Vite, Biome.

## Global Constraints

- **Commit message style:** sentence-form, imperative, capitalised, no prefix. Match the repo: `Write down the order a release goes out in`, `Cap the first check's delay so a fresh start hears about a fix`. **Never** use conventional-commit prefixes (`feat:`, `fix:`) — this repo does not use them.
- **No AI attribution.** Never add `Co-Authored-By`, `Generated with Claude`, or any similar trailer to a commit, branch, or PR.
- **`web/dist` is committed and embedded in the binary.** Any change under `web/src` must be followed by `make web-build` and the rebuilt `web/dist` committed in the same commit. `make web-verify` fails CI otherwise.
- **There is no JavaScript test runner in this repo** (no vitest, no jest — `web/package.json` has only `dev`, `build`, `preview`, `typecheck`). Front-end tasks are verified with `make web-lint` (Biome + `tsc --noEmit`), `make web-build`, and the explicit manual browser checks written into each task. Do not add a test framework as part of this plan.
- **Go tests** run with `go test ./internal/ws/ -run <Name> -v`. Follow the existing style in `internal/ws/hub_conn_test.go`: a doc comment on each test saying *why* the behaviour matters, `t.Fatalf` for setup failures, `t.Errorf` for assertions.
- **Lint:** `make vet` and `make lint` must pass before every commit.
- **Comments** explain why, not what, and are written in full sentences. Match the surrounding prose density — this codebase comments heavily and deliberately.

---

## File Structure

| File | Responsibility after this plan |
|------|-------------------------------|
| `internal/ws/hub.go` | Adds `armedAt`, renames the all-disconnected callback to a reporting one, adds `ArmedAt()`, `DismissAlarm()`, `DisarmWithPin()` |
| `internal/ws/messages.go` | Adds `armed_since` to `ServerMessage`, `MsgTypeAlarmCleared`, extends `NewAuthOK` |
| `internal/ws/hub_disconnect_test.go` | **New** — proves a dropped phone does not alarm |
| `internal/ws/hub_session_test.go` | **New** — proves `auth_ok` carries armed state and `DismissAlarm` reaches every phone |
| `cmd/leavesafe/main.go` | Rewires the disconnect callback; adds `arm`, `disarm`, `status` console commands; routes `stop` through the hub |
| `web/src/lib/session.ts` | **New** — persist, load and clear the pairing session |
| `web/src/lib/protocol.ts` | Adds `armed_since` to `ServerMessage` |
| `web/src/app.tsx` | Reconnects from a stored session; applies armed state from `auth_ok`; handles `alarm_cleared`; logs SW registration failure |
| `web/src/components/SettingsSheet.tsx` | "When your phone is asleep" group; "Forget this phone" action |
| `README.md` | Documents the new disconnect behaviour and the phone's role |

---

## Task 1: A dropped phone no longer sounds the alarm

**Files:**
- Modify: `internal/ws/hub.go:41` (field), `internal/ws/hub.go:211-217` (setter), `internal/ws/hub.go:1185-1215` (`removeClient`)
- Modify: `cmd/leavesafe/main.go:590-593`
- Test: `internal/ws/hub_disconnect_test.go` (new)

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `func (h *Hub) SetAllDisconnectedCallback(fn func())` — replaces `SetDisconnectCallback`. The callback now *reports* that every phone has gone; it must not start an alarm.

- [ ] **Step 1: Write the failing test**

Create `internal/ws/hub_disconnect_test.go`:

```go
package ws

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/leavesafe/leavesafe/internal/auth"
	"nhooyr.io/websocket"
)

// dialAndAuth opens a socket, gets past the greeting and pairs it, returning a
// connection the caller closes to simulate the phone going away.
func dialAndAuth(t *testing.T, ctx context.Context, srv string, hub *Hub) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.Dial(ctx, srv, nil) //nolint:bodyclose // response body is not used
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	readHello(t, ctx, conn)

	authMsg := `{"type":"auth","key":"` + hub.authManager.RawPairingKey() + `"}`
	if err := conn.Write(ctx, websocket.MessageText, []byte(authMsg)); err != nil {
		t.Fatalf("write auth: %v", err)
	}
	if _, _, err := conn.Read(ctx); err != nil {
		t.Fatalf("expected auth_ok: %v", err)
	}
	return conn
}

// A phone whose screen locks drops the socket. That is the ordinary case, not an
// intrusion, and treating it as one means the laptop screams at a user who did
// nothing but put their phone in their pocket.
func TestDisconnectWhileArmedDoesNotAlarm(t *testing.T) {
	const grace = 50 * time.Millisecond

	hub := testHub(t)
	hub.SetTimings(0, grace)

	var alarmed, reported atomic.Bool
	hub.SetAlarmTriggerCallback(func() { alarmed.Store(true) })
	hub.SetAllDisconnectedCallback(func() { reported.Store(true) })

	srv := hubServer(t, hub)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn := dialAndAuth(t, ctx, wsURL(srv), hub)
	hub.Arm()

	if err := conn.Close(websocket.StatusNormalClosure, ""); err != nil {
		t.Fatalf("close: %v", err)
	}

	time.Sleep(grace * 6)

	if alarmed.Load() {
		t.Error("losing the phone started the alarm; only a real sensor event should")
	}
	if !reported.Load() {
		t.Error("losing the phone was never reported")
	}
}

// A phone that reconnects within the grace period never really left, so it must
// produce no notice at all. Otherwise every brief network blip writes a line.
func TestReconnectWithinGraceIsNotReported(t *testing.T) {
	const grace = 300 * time.Millisecond

	hub := testHub(t)
	hub.SetTimings(0, grace)

	var reported atomic.Bool
	hub.SetAllDisconnectedCallback(func() { reported.Store(true) })

	srv := hubServer(t, hub)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn := dialAndAuth(t, ctx, wsURL(srv), hub)
	hub.Arm()

	if err := conn.Close(websocket.StatusNormalClosure, ""); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Back before the grace period is up, the way a page that reloaded is.
	again := dialAndAuth(t, ctx, wsURL(srv), hub)
	defer again.Close(websocket.StatusNormalClosure, "")

	time.Sleep(grace * 3)

	if reported.Load() {
		t.Error("a phone that came straight back was reported as gone")
	}
}

// The alarm still has to fire for the thing it is actually for.
func TestArmedHubStillAlarmsOnASensorEvent(t *testing.T) {
	hub := testHub(t)

	var alarmed atomic.Bool
	hub.SetAlarmTriggerCallback(func() { alarmed.Store(true) })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go hub.RunAlertDispatcher(ctx)

	hub.Arm()
	hub.PushAlert(NewAlert("power", "critical", "Charger disconnected"))

	deadline := time.Now().Add(2 * time.Second)
	for !alarmed.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !alarmed.Load() {
		t.Error("a real sensor event did not start the alarm")
	}
}
```

The import block for this file is exactly:

```go
import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"nhooyr.io/websocket"
)
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/ws/ -run 'TestDisconnect|TestReconnectWithinGrace|TestArmedHubStill' -v`
Expected: compile failure — `hub.SetAllDisconnectedCallback undefined`.

- [ ] **Step 3: Rename the callback and stop it alarming**

In `internal/ws/hub.go`, rename the struct field on line 41:

```go
	onAllDisconnected func()
```

Replace the setter at lines 211-217:

```go
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
```

In `removeClient`, replace the closing block (currently lines 1192-1214):

```go
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
```

- [ ] **Step 4: Rewire the callback in main.go**

Replace `cmd/leavesafe/main.go:590-593`:

```go
	hub.SetAllDisconnectedCallback(func() {
		log.Warn("Every phone disconnected while armed — monitoring continues")
		sb.writeLine("  %s[LINK]%s No phone is connected. Monitoring continues; "+
			"an alert may not reach you until one reconnects.", cYellow, cReset)
	})
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/ws/ -v`
Expected: PASS, including the three new tests.

- [ ] **Step 6: Build and lint**

Run: `make vet && make lint && go build ./...`
Expected: no findings, binary builds.

- [ ] **Step 7: Commit**

```bash
git add internal/ws/hub.go internal/ws/hub_disconnect_test.go cmd/leavesafe/main.go
git commit -m "Stop treating a sleeping phone as an intruder"
```

---

## Task 2: The hub remembers when it was armed

**Files:**
- Modify: `internal/ws/hub.go` (struct fields ~line 39, `Arm`, `Disarm`, `RestoreArmed`)
- Modify: `cmd/leavesafe/main.go:866`
- Test: `internal/ws/hub_session_test.go` (new)

**Interfaces:**
- Consumes: nothing from Task 1.
- Produces:
  - `func (h *Hub) ArmedAt() time.Time` — zero value when not armed.
  - `func (h *Hub) RestoreArmed(since time.Time)` — **signature change**; pass `time.Time{}` when the moment is unknown and the hub uses `time.Now()`.

- [ ] **Step 1: Write the failing test**

Create `internal/ws/hub_session_test.go`:

```go
package ws

import (
	"testing"
	"time"
)

// The phone shows how long the machine has been armed. That counter has to come
// from the laptop, because the page holding it is discarded every time the phone
// locks its screen.
func TestArmRecordsWhenItHappened(t *testing.T) {
	hub := testHub(t)

	if got := hub.ArmedAt(); !got.IsZero() {
		t.Errorf("a disarmed hub reported an arm time of %v", got)
	}

	before := time.Now()
	hub.Arm()
	got := hub.ArmedAt()

	if got.IsZero() {
		t.Fatal("arming recorded no time")
	}
	if got.Before(before.Add(-time.Second)) || got.After(time.Now().Add(time.Second)) {
		t.Errorf("arm time %v is not around now (%v)", got, before)
	}

	hub.Disarm()
	if got := hub.ArmedAt(); !got.IsZero() {
		t.Errorf("disarming left an arm time of %v", got)
	}
}

// A restart that re-arms did not start a new watch — it resumed the one the
// previous run was keeping, and the counter has to say so.
func TestRestoreArmedKeepsTheOriginalTime(t *testing.T) {
	hub := testHub(t)
	original := time.Now().Add(-2 * time.Hour)

	hub.RestoreArmed(original)

	if !hub.IsArmed() {
		t.Fatal("RestoreArmed did not arm the hub")
	}
	if got := hub.ArmedAt(); !got.Equal(original) {
		t.Errorf("arm time is %v, want the original %v", got, original)
	}
}

// Nothing recorded the moment on an older state file, and inventing one two
// hours in the past would be worse than admitting it started now.
func TestRestoreArmedWithoutATimeUsesNow(t *testing.T) {
	hub := testHub(t)

	hub.RestoreArmed(time.Time{})

	if got := hub.ArmedAt(); got.IsZero() {
		t.Error("RestoreArmed with an unknown time left no arm time at all")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/ws/ -run 'TestArmRecords|TestRestoreArmed' -v`
Expected: compile failure — `hub.ArmedAt undefined`.

- [ ] **Step 3: Add the field, the accessor, and set it**

In `internal/ws/hub.go`, add after the `armed bool` field (line 39):

```go
	armed bool
	// armedAt is when armed last became true, zero when disarmed. The phone's
	// "armed for 12 minutes" counter reads from here rather than keeping its
	// own, because the page holding it is thrown away every time the screen
	// locks and would otherwise restart from zero on every reconnect.
	armedAt time.Time
```

Add the accessor next to `IsArmed` (after line 253):

```go
// ArmedAt returns when the system was armed, or the zero time when it is not.
func (h *Hub) ArmedAt() time.Time {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.armedAt
}
```

In `Arm()`, inside the existing lock:

```go
func (h *Hub) Arm() {
	h.mu.Lock()
	h.armed = true
	h.armedAt = time.Now()
	tracker := h.tracker
	trackerCtx := h.trackerCtx
	h.mu.Unlock()
```

In `Disarm()`, inside the existing lock:

```go
func (h *Hub) Disarm() {
	h.mu.Lock()
	h.armed = false
	h.armedAt = time.Time{}
	h.alarmActive = false
	h.alarmSensor = ""
	tracker := h.tracker
	h.mu.Unlock()
```

Replace `RestoreArmed` (lines 273-290):

```go
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
```

- [ ] **Step 4: Update the single caller**

In `cmd/leavesafe/main.go`, line 866 (the last line of `reportInterruptedMonitoring`):

```go
	hub.RestoreArmed(prev.ChangedAt)
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/ws/ -v && go build ./...`
Expected: PASS, binary builds.

- [ ] **Step 6: Commit**

```bash
git add internal/ws/hub.go internal/ws/hub_session_test.go cmd/leavesafe/main.go
git commit -m "Remember when the machine was armed, not just that it is"
```

---

## Task 3: `auth_ok` carries the armed state

**Files:**
- Modify: `internal/ws/messages.go:104-121` (`ServerMessage`), `internal/ws/messages.go:213-221` (`NewAuthOK`)
- Modify: `internal/ws/hub.go` (`handleAuth`, ~line 812)
- Test: `internal/ws/hub_session_test.go` (append)

**Interfaces:**
- Consumes: `Hub.ArmedAt()` from Task 2.
- Produces:
  - `ServerMessage.ArmedSince *int64` with JSON tag `armed_since` — Unix **seconds**, omitted when not armed.
  - `func NewAuthOK(token string, sensors []SensorInfo, version string, armed bool, armedAt time.Time) ServerMessage`

- [ ] **Step 1: Write the failing test**

Append to `internal/ws/hub_session_test.go`:

```go
// A phone that locked its screen and came back has to learn the state on
// arrival. Waiting for the next heartbeat means up to fifteen seconds showing
// "disarmed" for a machine that is armed — the one thing the panel must never
// get wrong.
func TestAuthOKCarriesTheArmedState(t *testing.T) {
	hub := testHub(t)
	hub.Arm()

	msg := NewAuthOK("tok", nil, "test", hub.IsArmed(), hub.ArmedAt())

	if msg.Armed == nil || !*msg.Armed {
		t.Fatal("auth_ok did not say the machine was armed")
	}
	if msg.ArmedSince == nil {
		t.Fatal("auth_ok carried no arm time")
	}
	if want := hub.ArmedAt().Unix(); *msg.ArmedSince != want {
		t.Errorf("auth_ok said armed since %d, want %d", *msg.ArmedSince, want)
	}
}

// Claiming an arm time for a disarmed machine would give the phone a counter to
// render for a watch that is not running.
func TestAuthOKOmitsTheArmTimeWhenDisarmed(t *testing.T) {
	hub := testHub(t)

	msg := NewAuthOK("tok", nil, "test", hub.IsArmed(), hub.ArmedAt())

	if msg.Armed == nil || *msg.Armed {
		t.Error("auth_ok claimed a disarmed machine was armed")
	}
	if msg.ArmedSince != nil {
		t.Errorf("auth_ok carried an arm time of %d while disarmed", *msg.ArmedSince)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/ws/ -run TestAuthOK -v`
Expected: compile failure — too many arguments to `NewAuthOK`.

- [ ] **Step 3: Extend the message and the constructor**

In `internal/ws/messages.go`, add to `ServerMessage` after the `Armed` field (line 112):

```go
	Armed             *bool                   `json:"armed,omitempty"`
	// ArmedSince is when arming happened, in Unix seconds. Sent with auth_ok so
	// a phone that reconnected resumes its counter instead of restarting it.
	// Omitted when the machine is not armed.
	ArmedSince *int64 `json:"armed_since,omitempty"`
```

Replace `NewAuthOK` (lines 213-221):

```go
// NewAuthOK creates an auth success response.
//
// It carries the armed state because a reconnecting phone would otherwise show
// the machine as disarmed until the next status broadcast — and the commonest
// reason to reconnect is that the screen locked, which is exactly when the
// machine is most likely to be armed.
func NewAuthOK(token string, sensors []SensorInfo, version string, armed bool, armedAt time.Time) ServerMessage {
	msg := ServerMessage{
		Type:    MsgTypeAuthOK,
		Token:   token,
		Version: version,
		Sensors: sensors,
		Armed:   &armed,
	}
	if armed && !armedAt.IsZero() {
		since := armedAt.Unix()
		msg.ArmedSince = &since
	}
	return msg
}
```

- [ ] **Step 4: Update the call site**

In `internal/ws/hub.go`, inside `handleAuth`, replace the `client.send(NewAuthOK(...))` line:

```go
	infos := h.GetSensorInfos()
	client.send(NewAuthOK(token, infos, h.version, h.IsArmed(), h.ArmedAt()))
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/ws/ -v && go build ./...`
Expected: PASS.

Note: `TestHelloRevealsNothingBeforeAuthentication` asserts `msg.Armed == nil` on the *hello* message, which this change does not touch. If it fails, the armed state leaked into `NewHello` — revert that.

- [ ] **Step 6: Commit**

```bash
git add internal/ws/messages.go internal/ws/hub.go internal/ws/hub_session_test.go
git commit -m "Tell a pairing phone whether the machine is already armed"
```

---

## Task 4: The terminal can arm, disarm, silence and report

**Files:**
- Modify: `internal/ws/messages.go` (message type constant), `internal/ws/hub.go` (`DismissAlarm`, `DisarmWithPin`, `MsgTypeDisarmPin` handler)
- Modify: `cmd/leavesafe/main.go:1000` (hint), `cmd/leavesafe/main.go:1045-1145` (`runConsole`)
- Test: `internal/ws/hub_session_test.go` (append)

**Interfaces:**
- Consumes: `Hub.ArmedAt()` from Task 2.
- Produces:
  - `MsgTypeAlarmCleared = "alarm_cleared"`
  - `func (h *Hub) DismissAlarm()` — clears alarm state, fires the dismiss callback, and tells every authenticated client.
  - `func (h *Hub) DisarmWithPin(source, pin string) error` — verifies the PIN when protection is on, then disarms. `source` is the rate-limit bucket (`client.remoteAddr` for a phone, `"console"` for the terminal).

- [ ] **Step 1: Write the failing test**

Append to `internal/ws/hub_session_test.go`:

```go
// Silencing from the terminal has to silence the phone too. Stopping only the
// laptop leaves the phone wailing in a pocket with no way to know it is over.
func TestDismissAlarmTellsEveryPhone(t *testing.T) {
	hub := testHub(t)

	var dismissed atomic.Bool
	hub.SetAlarmDismissCallback(func() { dismissed.Store(true) })

	srv := hubServer(t, hub)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn := dialAndAuth(t, ctx, wsURL(srv), hub)
	defer conn.Close(websocket.StatusNormalClosure, "")

	hub.DismissAlarm()

	if !dismissed.Load() {
		t.Error("DismissAlarm did not stop the laptop's own alarm")
	}

	readCtx, readCancel := context.WithTimeout(ctx, 2*time.Second)
	defer readCancel()
	for {
		_, data, err := conn.Read(readCtx)
		if err != nil {
			t.Fatal("no alarm_cleared reached the phone")
		}
		var msg ServerMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if msg.Type == MsgTypeAlarmCleared {
			return
		}
	}
}

// The PIN guards the alarm from whoever is holding the device. A terminal is a
// device too, and a wrong PIN there must leave the machine armed.
func TestConsoleDisarmRefusesAWrongPin(t *testing.T) {
	hash, err := auth.HashPin("1234")
	if err != nil {
		t.Fatalf("hash pin: %v", err)
	}

	hub := testHub(t)
	hub.SetPinProtection(true, hash)
	hub.Arm()

	if err := hub.DisarmWithPin("console", "9999"); err == nil {
		t.Error("a wrong PIN was accepted")
	}
	if !hub.IsArmed() {
		t.Error("a refused PIN disarmed the machine anyway")
	}

	if err := hub.DisarmWithPin("console", "1234"); err != nil {
		t.Errorf("the correct PIN was refused: %v", err)
	}
	if hub.IsArmed() {
		t.Error("the correct PIN did not disarm the machine")
	}
}

// With no PIN configured, disarming must not invent a barrier.
func TestConsoleDisarmWithoutAPinJustDisarms(t *testing.T) {
	hub := testHub(t)
	hub.Arm()

	if err := hub.DisarmWithPin("console", ""); err != nil {
		t.Errorf("disarm was refused with no PIN protection on: %v", err)
	}
	if hub.IsArmed() {
		t.Error("the machine stayed armed")
	}
}
```

Add `"encoding/json"`, `"sync/atomic"`, `"github.com/leavesafe/leavesafe/internal/auth"` and `"nhooyr.io/websocket"` to the imports of `hub_session_test.go`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/ws/ -run 'TestDismissAlarmTells|TestConsoleDisarm' -v`
Expected: compile failure — `hub.DismissAlarm undefined`, `hub.DisarmWithPin undefined`.

- [ ] **Step 3: Add the message type**

In `internal/ws/messages.go`, add to the server-message const block (after `MsgTypeAlarmActive`):

```go
	// MsgTypeAlarmCleared says the alarm has been dismissed by someone other
	// than this phone — from the laptop's own terminal, or by another paired
	// phone. Without it a console `stop` silences the laptop and leaves every
	// phone sounding.
	MsgTypeAlarmCleared = "alarm_cleared"
```

- [ ] **Step 4: Add `DismissAlarm` and `DisarmWithPin` to the hub**

In `internal/ws/hub.go`, add after `clearAlarm`:

```go
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
```

Replace the `MsgTypeDisarmPin` case in `handleMessage` (lines 694-711) so the verification lives in one place:

```go
		case MsgTypeDisarmPin:
			if err := h.DisarmWithPin(client.remoteAddr, msg.Pin); err != nil {
				client.send(ServerMessage{Type: MsgTypeAuthFail, Reason: err.Error()})
				return
			}
```

- [ ] **Step 5: Run the hub tests to verify they pass**

Run: `go test ./internal/ws/ -v`
Expected: PASS.

- [ ] **Step 6: Add the console commands**

In `cmd/leavesafe/main.go`, replace the `stop` case in `runConsole` (lines 1062-1068):

```go
		case line == "stop" || line == "silence":
			if localAlarm.IsPlaying() || hub.ClientCount() > 0 {
				// Through the hub rather than the alarm directly, so every
				// paired phone stops sounding too.
				hub.DismissAlarm()
				sb.writeLine("  %s[ALARM]%s Alarm dismissed from console", cYellow, cReset)
			} else {
				sb.writeLine("  No alarm is currently active")
			}

		case line == "arm":
			if hub.IsArmed() {
				sb.writeLine("  Already armed since %s", hub.ArmedAt().Format("15:04:05"))
				break
			}
			hub.Arm()
			sb.writeLine("  %s[ARM]%s Armed from console", cGreen, cReset)

		case line == "disarm":
			if !hub.IsArmed() {
				sb.writeLine("  Already disarmed")
				break
			}
			pin := ""
			if hub.PinRequired() {
				sb.writeLine("  PIN required to disarm. Type it and press enter:")
				if !scanner.Scan() {
					break
				}
				pin = strings.TrimSpace(scanner.Text())
			}
			if err := hub.DisarmWithPin("console", pin); err != nil {
				sb.writeLine("  %s[PIN]%s Refused: %v", cRed, cReset, err)
				break
			}
			sb.writeLine("  %s[DISARM]%s Disarmed from console", cGreen, cReset)

		case line == "status":
			if hub.IsArmed() {
				sb.writeLine("  %sARMED%s since %s (%s ago)", cGreen, cReset,
					hub.ArmedAt().Format("15:04:05"),
					time.Since(hub.ArmedAt()).Round(time.Second))
			} else {
				sb.writeLine("  %sDISARMED%s", cDim, cReset)
			}
			sb.writeLine("  Phones connected: %d", hub.ClientCount())
			for _, s := range hub.GetSensorInfos() {
				mark := "off"
				switch {
				case !s.Available:
					mark = "unavailable"
				case s.Enabled:
					mark = "on"
				}
				sb.writeLine("    %-10s %s", s.Name, mark)
			}
```

Add `PinRequired` to `internal/ws/hub.go`, next to `SetPinProtection`:

```go
// PinRequired reports whether disarming asks for a PIN.
func (h *Hub) PinRequired() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.pinEnabled && h.pinHash != ""
}
```

Update the help line (`main.go:1139`):

```go
		case line == "help":
			sb.writeLine("  Commands: arm, disarm, status, stop, test, trigger <sensor>, history, urls, qr <n>, cert, update, rotate-key, help")
```

Update the hint printed at startup (`main.go:1000-1001`):

```go
	fmt.Fprintf(out, "\033[%d;1H  %sCommands:%s arm, disarm, status, stop, test, trigger <sensor>, history, urls, qr <n>, cert, rotate-key, help  %s│%s  %sCtrl+C to quit%s\n",
		row, cDim, cReset, cDim, cReset, cDim, cReset)
```

Confirm `time` and `strings` are already imported in `main.go` — they are.

- [ ] **Step 7: Verify the whole build and run the console by hand**

Run: `make vet && make lint && go test ./... && go build -o leavesafe.exe ./cmd/leavesafe`

Then start it (`./leavesafe.exe`) and, in the terminal:
1. Type `status` — expect `DISARMED`, `Phones connected: 0`, and the sensor list.
2. Type `arm` — expect `Armed from console`; `status` now shows `ARMED since …`.
3. Type `trigger power` — the laptop alarm starts.
4. Type `stop` — the alarm stops.
5. Type `disarm` — expect `Disarmed from console`.
6. Type `arm` then `disarm` again with `"pin_protection": {"enabled": true}` configured — expect the PIN prompt, a refusal on a wrong PIN, and the machine still armed.

- [ ] **Step 8: Commit**

```bash
git add internal/ws/hub.go internal/ws/messages.go internal/ws/hub_session_test.go cmd/leavesafe/main.go
git commit -m "Let the terminal arm, disarm and silence every phone at once"
```

---

## Task 5: The phone remembers its pairing

**Files:**
- Create: `web/src/lib/session.ts`
- Modify: `web/src/lib/protocol.ts:98-114`, `web/src/app.tsx`, `web/src/components/SettingsSheet.tsx`
- Rebuild: `web/dist` (committed)

**Interfaces:**
- Consumes: `armed_since` from Task 3, `alarm_cleared` from Task 4.
- Produces:
  - `loadSession(): StoredSession | null`
  - `saveSession(key: string, fingerprint: string | null): void`
  - `clearSession(): void`
  - `interface StoredSession { key: string; fingerprint: string | null }`

- [ ] **Step 1: Write the session store**

Create `web/src/lib/session.ts`:

```ts
// The pairing key, kept so a phone that locked its screen does not have to walk
// back to the laptop and rescan the QR code.
//
// A backgrounded page is discarded by the phone's operating system — routinely
// on Android, almost always on iOS — and everything the page was holding goes
// with it. Without this the user reopens the tab to a pairing screen while their
// laptop sits armed on a café table.
//
// This does put the key on the phone's disk. That is a deliberate trade: the key
// is already printed on screen as a QR code, the phone is the trusted device by
// design, and `rotate-key` on the laptop invalidates it. Settings offers
// "Forget this phone" for anyone who would rather not keep it.

const SESSION_KEY = 'leavesafe_session_v1';

export interface StoredSession {
    key: string;
    /** The certificate fingerprint this key was paired against, if there was one. */
    fingerprint: string | null;
}

export function loadSession(): StoredSession | null {
    try {
        const raw = window.localStorage.getItem(SESSION_KEY);
        if (!raw) return null;
        const parsed = JSON.parse(raw) as Partial<StoredSession>;
        if (typeof parsed.key !== 'string' || parsed.key === '') return null;
        return {
            key: parsed.key,
            fingerprint: typeof parsed.fingerprint === 'string' ? parsed.fingerprint : null,
        };
    } catch {
        // A corrupt entry means pairing by hand, which is where a phone that
        // never had one starts anyway.
        return null;
    }
}

export function saveSession(key: string, fingerprint: string | null) {
    try {
        window.localStorage.setItem(SESSION_KEY, JSON.stringify({ key, fingerprint }));
    } catch {
        // Private browsing refuses writes. The session still works until the
        // page goes away, which is the behaviour this file exists to improve on
        // rather than depend on.
    }
}

export function clearSession() {
    try {
        window.localStorage.removeItem(SESSION_KEY);
    } catch {
        // Nothing to do: a storage that refuses removal refused the write too.
    }
}
```

- [ ] **Step 2: Add the wire field**

In `web/src/lib/protocol.ts`, add to `ServerMessage` after `armed?: boolean;`:

```ts
    armed?: boolean;
    /** When arming happened, in Unix seconds. Sent with `auth_ok`. */
    armed_since?: number;
```

- [ ] **Step 3: Use the session in the app**

In `web/src/app.tsx`:

Add the imports:

```ts
import { clearSession, loadSession, saveSession } from './lib/session';
import { startSiren, stopSiren, warnDisconnected } from './lib/siren';
```

Add a module-level variable next to `pendingKey` (after line 68):

```ts
/** The key the current connection is pairing with, kept so it can be stored. */
let activeKey: string | null = null;
```

Replace the mount effect body (lines 98-116):

```ts
    useEffect(() => {
        loadLog();

        // A scanned QR code lands here with the key in the query string, and —
        // when the laptop is serving HTTPS — the fingerprint of the certificate
        // it is serving. Take both, then scrub them out of the address bar so
        // they do not sit in history.
        const params = new URLSearchParams(window.location.search);
        const fromQr = params.get('key');
        const fp = params.get('fp');
        if (fp) expectedFingerprint = normalizeFingerprint(fp);
        if (fromQr || fp) {
            window.history.replaceState({}, document.title, '/');
        }
        if (fromQr) {
            setAutoKey(fromQr);
            window.setTimeout(() => pair(fromQr.replace(/\D/g, ''), 'websocket'), 400);
            return;
        }

        // No code in the address bar means this is a return visit — most often
        // because the phone locked its screen and threw the page away. Pick the
        // pairing back up rather than asking for the QR code again.
        const stored = loadSession();
        if (stored) {
            expectedFingerprint = stored.fingerprint;
            window.setTimeout(() => pair(stored.key, 'websocket'), 100);
        }
    }, []);
```

Replace the `auth_ok` case (lines 145-154):

```ts
            case 'auth_ok':
                setToken(msg.token ?? null);
                serverVersion.value = msg.version ?? null;
                sensors.value = msg.sensors ?? [];
                if (typeof msg.armed === 'boolean') {
                    armed.value = msg.armed;
                    // The laptop's own clock, so a page that was discarded and
                    // reopened resumes the counter instead of restarting it. An
                    // older laptop sends no time, and now is the only honest
                    // answer left.
                    if (!msg.armed) {
                        armedSince.value = null;
                    } else if (msg.armed_since) {
                        armedSince.value = msg.armed_since * 1000;
                    } else {
                        armedSince.value = Date.now();
                    }
                }
                if (activeKey) saveSession(activeKey, expectedFingerprint);
                pairing.value = false;
                pairError.value = null;
                screen.value = 'panel';
                link.value = 'live';
                afterPairing();
                break;
```

Replace the `auth_fail` case (lines 156-166):

```ts
            case 'auth_fail':
                pairing.value = false;
                if (!hasToken()) {
                    // The stored key no longer opens this laptop — rotated, or
                    // from a different one. Keeping it would retry forever.
                    clearSession();
                    const left = msg.remaining_attempts;
                    pairError.value = `${msg.reason ?? 'That key was refused.'}${
                        left ? ` ${left} attempt${left === 1 ? '' : 's'} left.` : ''
                    }`;
                } else {
                    showToast(msg.reason ?? 'Refused');
                }
                break;
```

Add a case after `alarm_active` (after line 207):

```ts
            case 'alarm_cleared':
                alarm.value = null;
                stopSiren();
                break;
```

In `pair()`, record the key (after line 257):

```ts
    function pair(key: string, over: 'websocket' | 'bluetooth') {
        pairing.value = true;
        pairError.value = null;
        pendingKey = key;
        activeKey = key;
```

- [ ] **Step 4: Add "Forget this phone" to settings**

In `web/src/components/SettingsSheet.tsx`, replace the import block at lines 1-4 with:

```ts
import { useEffect, useState } from 'preact/hooks';
import type { AppConfig, ClientMessage } from '../lib/protocol';
import { clearSession } from '../lib/session';
import {
    closeTransport,
    config,
    screen,
    send,
    serverVersion,
    setToken,
    settingsOpen,
    showToast,
    updateAvailable,
} from '../lib/store';
import { Scrim } from './Scrim';
```

Replace the `sheet-actions` block (lines 298-305):

```tsx
                        <div class="sheet-actions">
                            <button type="button" class="alarm-primary" onClick={save}>
                                {saved ? 'Saved' : 'Save settings'}
                            </button>
                            <button
                                type="button"
                                class="sheet-reset"
                                onClick={() => {
                                    clearSession();
                                    setToken(null);
                                    closeTransport();
                                    settingsOpen.value = false;
                                    screen.value = 'pair';
                                }}
                            >
                                Forget this phone
                            </button>
                            <button type="button" class="sheet-reset" onClick={reset}>
                                Reset everything
                            </button>
                        </div>
```

- [ ] **Step 5: Typecheck, lint and build**

Run: `make web-lint && make web-build`
Expected: no Biome findings, no TypeScript errors, `web/dist` rebuilt.

- [ ] **Step 6: Verify in a browser by hand**

Build and start the laptop binary (`go build -o leavesafe.exe ./cmd/leavesafe && ./leavesafe.exe`), then from a phone or a mobile-emulated browser tab:

1. Scan/open the QR URL and pair. Confirm the panel appears.
2. Arm from the phone.
3. **Close the tab entirely** and reopen the laptop's URL with **no** `?key=` in it.
   Expected: the pairing screen is skipped, the panel appears, and it says **armed** immediately with the counter continuing — not restarting from zero.
4. In the laptop terminal type `trigger power`, then `stop`.
   Expected: the phone's siren and overlay clear on their own, without touching the phone.
5. In the laptop terminal type `rotate-key`, then reload the phone page.
   Expected: the stored key is refused once, the session is cleared, and the pairing screen appears rather than a reconnect loop.
6. Open Settings → **Forget this phone**.
   Expected: back to the pairing screen; reloading does not auto-reconnect.

- [ ] **Step 7: Commit**

```bash
git add web/src/lib/session.ts web/src/lib/protocol.ts web/src/app.tsx web/src/components/SettingsSheet.tsx web/dist
git commit -m "Pick the pairing back up when a phone wakes and returns"
```

---

## Task 6: The interface states what it cannot do

**Files:**
- Modify: `web/src/app.tsx` (`afterPairing`), `web/src/components/ArmControl.tsx`, `web/src/components/SettingsSheet.tsx`
- Modify: `README.md`
- Rebuild: `web/dist` (committed)

**Interfaces:**
- Consumes: `showToast` from `web/src/lib/store.ts` (already exported).
- Produces: nothing later tasks depend on.

- [ ] **Step 1: Say it at the moment it matters**

In `web/src/components/ArmControl.tsx`, add `showToast` to the store import and say it when arming completes — the one moment the user is deciding to walk away:

```ts
import { armed, send, showToast } from '../lib/store';
```

In `startArming`, inside the branch that fires the arm:

```ts
            if (left <= 0) {
                window.clearInterval(countdown.current);
                onCountdown(null);
                send({ type: 'arm' });
                captureAnchor();
                send({ type: 'get_location' });
                // Said here rather than pinned to the panel: it is true all the
                // time, but it only changes what someone does at the moment
                // they are about to put the phone away.
                showToast('Armed. If your phone sleeps the alert may not arrive — the laptop still sounds its own alarm.');
            } else {
```

- [ ] **Step 2: Explain it where someone can go back and read it**

In `web/src/components/SettingsSheet.tsx`, add a group directly above the `sheet-actions` block:

```tsx
                        <Group
                            title="When your phone is asleep"
                            note="LeaveSafe talks straight to your laptop, with no cloud in between. That is also the limit: when your phone locks its screen or you switch apps, the operating system freezes this page and closes the connection, so an alert may not reach you until you open it again."
                        >
                            <p class="group-note">
                                The laptop does not depend on your phone. It keeps watching every
                                sensor you enabled and sounds its own alarm whether or not a phone is
                                connected — and losing the connection is not itself treated as an
                                intrusion, so a phone in your pocket will not set it off.
                            </p>
                        </Group>
```

- [ ] **Step 3: Stop discarding the service worker failure**

In `web/src/app.tsx`, replace the registration in `afterPairing` (lines 249-251):

```ts
        if ('serviceWorker' in navigator) {
            navigator.serviceWorker.register('/sw.js').catch((err: Error) => {
                // Browsers refuse to register a worker on an origin with a
                // certificate error, which is exactly what the laptop's
                // self-signed certificate produces. Swallowing this made a
                // notification path that never ran look like one that did.
                console.warn('LeaveSafe: notifications are unavailable —', err.message);
            });
        }
```

- [ ] **Step 4: Update the README**

Three exact edits in `README.md`:

**1. Line 35**, the pitch paragraph. Append a sentence to the end of it:

```markdown
**LeaveSafe is the third option.** Run it, scan the QR code with your phone, tap *Arm*, and walk away. The laptop watches its own charger, lid, USB ports, screen lock, network and input. The moment any of them changes, your phone goes off in your pocket — and the laptop starts screaming too. The phone is the convenience, not the alarm: when its screen locks the operating system freezes the page, so an alert may not reach it until you open it again — and the laptop sounds regardless.
```

**2. The console command table** (around lines 236-247). Add three rows immediately above the `` `stop` / `silence` `` row:

```markdown
| `arm` | Arm monitoring from the terminal |
| `disarm` | Disarm from the terminal; asks for the PIN if one is set |
| `status` | Armed state, how long for, phones connected and every sensor |
```

and change the `` `stop` / `silence` `` row's description to name the broadcast:

```markdown
| `stop` / `silence` | Stop an alarm that is going off, on the laptop and on every paired phone |
```

**3. Line 510**, the `disconnect_grace_seconds` row of the config table. The delay no longer leads to an alarm:

```markdown
| `disconnect_grace_seconds` | `30` | How long a phone can be gone before the laptop reports it. Not an alarm — only the sensors do that |
```

- [ ] **Step 5: Typecheck, lint and build**

Run: `make web-lint && make web-build`
Expected: clean.

Also run `make typos` if the `typos` binary is on PATH — it is a Rust tool the Makefile expects you to install separately, so skip it if it is missing rather than installing it for this task. CI runs it either way.

- [ ] **Step 6: Verify by hand**

1. Arm from the phone. Expected: the toast appears with the sleep warning.
2. Open Settings and scroll to **When your phone is asleep**. Expected: the explanation reads correctly and the group matches the others visually.
3. With the phone's developer console attached over a self-signed origin, confirm the `LeaveSafe: notifications are unavailable` warning appears if registration fails — and does not appear when it succeeds.

- [ ] **Step 7: Commit**

```bash
git add web/src/app.tsx web/src/components/ArmControl.tsx web/src/components/SettingsSheet.tsx web/dist README.md
git commit -m "Say plainly that a sleeping phone may miss the alert"
```

---

## Task 7: Full verification

**Files:** none modified.

- [ ] **Step 1: Run the complete check suite**

Run: `make check`

This runs `fmt`, `vet`, `lint`, `web-lint`, `web-verify`, `vuln` and `test`. Every one must pass. `web-verify` in particular fails if `web/dist` was not rebuilt and committed after a `web/src` change — if it fails, run `make web-build` and commit the result.

- [ ] **Step 2: Run the end-to-end suite**

Run: `make test-e2e`
Expected: PASS. If a test asserts the old disconnect-alarm behaviour, it is asserting behaviour this plan deliberately removed — update it to assert the new behaviour and say so in the commit.

- [ ] **Step 3: Walk the whole scenario once**

With the binary running and a real phone:

1. Pair by QR, arm from the phone.
2. Lock the phone's screen and leave it for two minutes.
   Expected: **no alarm**. The laptop terminal shows the `[LINK] No phone is connected` line.
3. Unlock the phone and reopen the tab.
   Expected: the panel returns armed, with the counter continuing from when you armed.
4. Unplug the laptop's charger.
   Expected: the laptop alarm sounds. The phone shows the alert if it is awake.
5. Type `stop` in the laptop terminal.
   Expected: both the laptop and the phone go quiet.
6. Type `status`.
   Expected: still `ARMED`, with the correct duration and phone count.
7. Type `disarm`.
   Expected: disarmed on the laptop and on the phone.

- [ ] **Step 4: Commit anything the verification changed**

If nothing changed, there is nothing to commit — say so rather than creating an empty commit.

---

## Notes for the implementer

- **Do not add Web Push, a notification relay, or any third-party service.** That was considered and deliberately rejected during design: every mechanism that reaches a locked phone routes through Google or Apple, which the product's no-cloud promise rules out. See `docs/superpowers/specs/2026-07-31-phone-session-resilience-design.md`.
- **The coverage loss in Task 1 is intentional.** "Someone jams the network and walks off with the laptop" no longer triggers on disconnection alone. It still triggers the moment the laptop is touched — which is what the six sensors are for. Do not re-add a disconnect alarm behind a config flag unless asked.
- **`docs/superpowers/` is gitignored** (`.gitignore:429`). The spec and this plan are working artifacts and must not be committed.
