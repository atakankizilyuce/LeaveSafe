# Phone session resilience and laptop-side control

**Date:** 2026-07-31
**Status:** Approved design, ready for planning

## The problem

A phone cannot be relied on to receive an alert. When the screen locks or the
browser is backgrounded, the operating system freezes the page, throttles its
timers and drops the WebSocket. Nothing in the page keeps running, so the alarm
message from the laptop arrives at no one.

LeaveSafe's current design does not account for this. Three things go wrong.

**A locked phone produces a false alarm.** `Hub.removeClient` fires
`onAllDisconnect` when the last authenticated client drops while armed, and
`cmd/leavesafe/main.go` wires that callback to `localAlarm.Start()`. The grace
period is 30 seconds. So a user who arms the laptop, pockets the phone and lets
the screen lock hears the laptop scream half a minute later, with nobody having
touched it. The behaviour was meant to catch "someone cut the network and took
the laptop"; it cannot tell that case apart from "the phone went to sleep", and
the second case is far more common.

**Reopening the page loses the session.** The pairing key lives only in a
closure in `app.tsx`. Only the event log is persisted (`web/src/lib/store.ts`).
When the phone discards the backgrounded page — routine on Android, near-certain
on iOS — reopening the URL lands on the pairing screen and the user must rescan
the QR code. Even when the in-memory reconnect does run, `auth_ok` carries no
armed state (`internal/ws/messages.go`), so the UI shows "disarmed" until the
next 15-second heartbeat and the armed-duration counter restarts from zero.

**The terminal cannot fully control the alarm.** `runConsole` has a `stop`
command, but it calls `localAlarm.Stop()` directly and never tells the hub. The
server keeps `alarmActive` set and every connected phone keeps showing the alarm
overlay and sounding its own siren. There is no way to arm, disarm or inspect
state from the terminal at all.

## What we are not doing

We are not adding Web Push, a third-party notification service, or a native
companion app. Each of those would reach a locked phone, and each would break
the product's central promise: no cloud, no accounts, no internet. The decision
is to stop treating the phone as a guaranteed alert channel and make the laptop
correct and self-sufficient instead — and to say so plainly in the interface
rather than fail silently.

## Design

### 1 · Disconnection stops being an alarm

A dropped phone connection no longer sounds the alarm. The laptop keeps
monitoring every enabled sensor and still sounds the alarm on a real event —
charger, lid, USB, screen, network, input. Losing the phone becomes an observable
fact rather than an accusation.

In `internal/ws/hub.go`, `removeClient` keeps its grace-period debounce but the
work it schedules changes: instead of invoking an alarm callback it records an
event and notifies the remaining surface (status bar, event log). The callback
`SetDisconnectCallback` is renamed to reflect that it reports rather than
triggers, and in `cmd/leavesafe/main.go` it writes a status line instead of
calling `localAlarm.Start()`.

The 30-second `disconnect_grace_seconds` config value keeps its meaning as the
debounce before the notice, so a phone that reconnects within a few seconds
produces no noise. Existing config files need no migration.

This is a deliberate loss of coverage: the "attacker jams the network, then walks
off with the laptop" case no longer triggers on its own. It still triggers the
moment the laptop is actually touched, which is what the sensors are for.

### 2 · The session survives the phone sleeping

**On the phone.** A new `web/src/lib/session.ts` persists the pairing key and
the expected certificate fingerprint to `localStorage` under a versioned key. On
mount, `app.tsx` reads it; if a session is present it skips the pairing screen
and connects directly. An `auth_fail` clears the stored session, so a rotated key
returns the user to the pairing screen instead of a reconnect loop.

This puts the pairing key on the phone's disk. That is a real trade-off and the
design accepts it: the key is already on screen as a QR code, the phone is the
trusted device, and `rotate-key` already invalidates every session. The settings
sheet gains a **Forget this phone** action that clears the stored session.

**On the laptop.** `auth_ok` starts carrying the armed state and the moment
arming began. The hub gains an `armedAt` field, set in `Arm()` and restored in
`RestoreArmed()` from the existing `state.State.ChangedAt`. `NewAuthOK` gains
`armed` and `armed_since` fields, and `app.tsx`'s `auth_ok` handler applies them.
A reopened page then shows the correct state immediately and the armed-duration
counter continues from where it was rather than restarting.

### 3 · The terminal is a full control surface

`runConsole` in `cmd/leavesafe/main.go` gains:

- `stop` routed through a new `Hub.DismissAlarm()` that calls the existing
  `clearAlarm()` and broadcasts a new `alarm_cleared` server message, so
  silencing from the terminal also silences every connected phone. Today it
  silences only the laptop.
- `arm` — arms via `hub.Arm()`, so state, persistence, location tracking and
  the event log all follow the same path as arming from the phone.
- `disarm` — disarms via `hub.Disarm()`. When PIN protection is enabled the
  console prompts for the PIN and verifies it through `auth`, matching what the
  phone requires.
- `status` — prints armed state, how long it has been armed, connected client
  count and which sensors are enabled.

`MsgTypeAlarmCleared = "alarm_cleared"` is added to `internal/ws/messages.go`
and handled in `app.tsx` by stopping the siren and dismissing the overlay.

The help text on `main.go:1139` and the command hint in the status bar
(`main.go:1000`) list the new commands.

### 4 · The interface says what it cannot do

The arm control and the settings sheet state the limit in plain language: when
the phone's screen is off or the browser is in the background, an alert may not
arrive; the laptop keeps monitoring and sounds its own alarm regardless. The
existing `link === 'lost'` state stays visible so the user can see when the phone
is not currently connected.

The README's monitoring and security sections are updated to match: disconnection
no longer alarms, and the phone is documented as a convenience channel rather
than a guaranteed one.

Separately, `app.tsx:250` swallows service worker registration failures with
`.catch(() => {})`. Because the server uses a self-signed certificate
(`internal/server/selfsigned.go`), Chrome refuses service worker registration on
that origin, so this path may never have worked. The failure is surfaced in the
log rather than discarded, so the state is knowable.

## Testing

- `removeClient` with the last client dropping while armed fires the notice
  callback and does not fire an alarm.
- A client reconnecting within the grace period produces no notice.
- `auth_ok` carries `armed` and `armed_since` for a hub that is armed, and
  `armed: false` for one that is not.
- `RestoreArmed` populates `armedAt` from the persisted state.
- `DismissAlarm` clears `alarmActive` and broadcasts `alarm_cleared` to every
  authenticated client.
- Console `disarm` with PIN protection enabled refuses a wrong PIN and leaves
  the system armed.
- Front end: a stored session skips the pairing screen; `auth_fail` clears it;
  **Forget this phone** clears it.

## Files touched

| File | Change |
|------|--------|
| `internal/ws/hub.go` | Disconnect notice instead of alarm; `armedAt`; `DismissAlarm()` |
| `internal/ws/messages.go` | `armed`/`armed_since` in `auth_ok`; `alarm_cleared` type |
| `cmd/leavesafe/main.go` | Disconnect callback rewired; `arm`/`disarm`/`status` commands; `stop` via hub |
| `web/src/lib/session.ts` | New — persist and clear the pairing session |
| `web/src/app.tsx` | Auto-reconnect from stored session; apply armed state from `auth_ok`; handle `alarm_cleared`; log SW registration failure |
| `web/src/components/SettingsSheet.tsx` | Forget this phone; the honest limitation notice |
| `web/src/components/ArmControl.tsx` | The honest limitation notice |
| `README.md` | Document the new disconnect behaviour and the phone's role |
