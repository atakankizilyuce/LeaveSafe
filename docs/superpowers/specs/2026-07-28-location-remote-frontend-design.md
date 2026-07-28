# Location Tracking, Remote Access Hardening and Frontend Rewrite — Design

Date: 2026-07-28
Status: approved

## Goal

Three pieces of work that share no code but were requested together:

1. **Location** — while armed, the phone can see where the monitored laptop is,
   at the best precision the machine can honestly produce.
2. **Remote access** — the mobile-data path already exists. Audit it and fix what
   the audit found.
3. **Frontend** — replace the phone UI with something people want to look at.

Each ships as its own pull request. They are ordered so that every PR is
independently complete and mergeable: remote access first (pure fixes, no new
surface), then location (new protocol), then the frontend rewrite (consumes that
protocol).

## Non-goals

- Tracking the *phone*. LeaveSafe monitors a laptop; the phone is the remote
  control. The phone's GPS is used only as a proxy for where the laptop was.
- Turning LeaveSafe into a recovery-after-theft service. There is no server-side
  history, no "find my device" account, no location upload to anyone.
- Location on a machine with no Wi-Fi radio and no internet. That case reports
  "unavailable" rather than inventing a coordinate.

## Guiding principle

**The UI never claims more precision than the underlying fix has.** Every
position carries the source that produced it and an accuracy radius, and both are
rendered. A 40 km IP-derived guess is drawn as a 40 km circle, not a pin.

---

## Workstream B — Remote access hardening

The remote-access path works, but reading it end to end turned up six defects.

### B1 — Silent downgrade to cleartext

`cmd/leavesafe/main.go:240`: when remote access is enabled and TLS certificate
generation fails, the code logs a warning and continues **without TLS**. The
server then binds to all interfaces, a UPnP mapping is opened, and the QR code
hands out an `http://` URL pointing at the public internet. The 16-digit pairing
key — the only thing standing between a stranger and the alarm — travels in
cleartext.

Fix: TLS failure while remote is enabled means remote does not come up. The
process stays alive and LAN-only, and the dashboard states why. Availability is
preserved; the downgrade is not.

### B2 — The certificate fingerprint is computed and thrown away

`internal/server/server.go:131` exposes `CertFingerprint()`. Nothing calls it.
The certificate is self-signed, so every phone shows a security warning, and the
user has no way to tell a genuine warning from an interception.

Fix: render the fingerprint in the TUI status box, and add a `cert` console
command that prints it in full for comparison.

### B3 — Five settings that do nothing

`max_sessions`, `max_auth_attempts` and `lockout_seconds` are editable in the
settings screen and persisted to `config.json`. `internal/auth/auth.go:12-17`
hardcodes 3, 5 and 60s and never reads them. `heartbeat_seconds` and
`disconnect_grace_seconds` are hardcoded the same way in `internal/ws/hub.go:20-22`.

A user who lowers `max_auth_attempts` to 3 to harden an internet-exposed
instance gets no such thing, and no indication of it.

Fix: `auth.NewManager` takes an options struct; `ws.NewHub` takes the heartbeat
and grace durations. The settings screen becomes truthful.

### B4 — Unauthenticated lockout denial of service

`internal/auth/auth.go:74-78`: failed attempts are counted in a single global
counter. Five wrong keys lock out **every** client for 60 seconds. On a LAN this
is a reasonable brute-force defence. Exposed to the internet by remote access, it
becomes a remote kill switch: anyone who finds the port can loop five wrong keys
forever and the owner can never pair.

Fix: attempts and lockout are tracked per remote address, in a map bounded to a
fixed number of tracked addresses with LRU eviction so the map cannot be grown
without limit. A stranger hammering the port locks out only themselves. The
global session cap (`max_sessions`) stays global, because that one is protecting
a real resource.

WebSocket connections must therefore carry the peer address into the hub;
`Hub.HandleConnection` gains it, and the BLE transport supplies a stable
synthetic address since it has no IP.

### B5 — The QR code stops working at home

`cmd/leavesafe/main.go:389-401`: with remote access on, the public URL is placed
first in the list and the QR encodes `urls[0]`. Scanning it from a phone on the
same Wi-Fi requires the router to support NAT hairpinning — routing a LAN client
back in through its own public IP. Many consumer routers do not. The common case
(sitting in the same room as the laptop) silently fails.

Fix: the dashboard lists every reachable URL, numbered. A `qr <n>` console
command re-renders the QR for any of them. Remote stays the default so that
enabling remote access does what it says, but the local URL is one command away.

### B6 — Documentation

The README describes remote access as a feature but not its requirements. Add
the manual port-forward fallback for routers without UPnP, the certificate
warning the user will see and how to verify it, and an explicit statement of what
exposing the port means.

### Testing

- Per-IP lockout: table tests covering isolation between addresses, expiry,
  and eviction under more addresses than the bound.
- Config plumbing: assert a non-default `max_auth_attempts` actually changes the
  attempt count before lockout.
- E2E: remote enabled with certificate generation forced to fail must not serve
  plaintext.

---

## Workstream A — Location

### Why a layered provider

A laptop has no GPS. Three sources are available, and none is sufficient alone:

| Source | Accuracy | Needs | Weakness |
|---|---|---|---|
| Phone GPS anchor | 5–10 m | nothing | Records where the laptop *was* at arm time |
| Wi-Fi BSSID scan | 20–50 m | API key, internet | Costs money, needs a provider |
| Public IP | 1–50 km | internet | Often points at the ISP, not the device |

The tracker runs all enabled providers and picks the best fix, reporting which
one it used.

### Package layout

Mirrors the convention `internal/monitor` already establishes: platform command
execution is separated from output parsing, and the parsers are unit-tested
against captured fixtures.

```
internal/location/
  location.go              Fix, Source, Provider interface
  tracker.go               poll loop, fix selection, movement detection
  distance.go              haversine
  ip.go                    public IP -> coordinates
  wifi.go                  BSSID scan -> geolocation API
  geolocate.go             Google-Geolocation-compatible client
  scan_windows.go          netsh wlan show networks mode=bssid
  scan_linux.go            nmcli dev wifi list / iw dev scan
  scan_darwin.go           system_profiler SPAirPortDataType
  parse_windows.go         + parse_windows_test.go
  parse_linux.go           + parse_linux_test.go
  parse_darwin.go          + parse_darwin_test.go
  testdata/{windows,linux,darwin}/
```

`airport` is not used on macOS: it was removed in macOS 14.4. `system_profiler
SPAirPortDataType` is the supported replacement and is what the parser targets.

### The Fix type

```go
type Source string

const (
    SourcePhone Source = "phone"
    SourceWiFi  Source = "wifi"
    SourceIP    Source = "ip"
)

type Fix struct {
    Latitude  float64   `json:"lat"`
    Longitude float64   `json:"lon"`
    AccuracyM float64   `json:"accuracy_m"`
    Source    Source    `json:"source"`
    Timestamp time.Time `json:"ts"`
    Label     string    `json:"label,omitempty"` // "Ankara, TR" for IP fixes
}
```

### Fix selection

Ranking by accuracy alone is wrong, because the most accurate source is also the
one that goes stale. The phone anchor is captured when the laptop and the phone
are in the same place; the moment the laptop is carried away, that 8 m circle is
precisely wrong.

Selection therefore runs in two steps:

1. Discard fixes older than `maxAge` (10 minutes).
2. If a live fix (Wi-Fi or IP) is further from the anchor than the sum of both
   accuracy radii, the anchor and the live fix **cannot** describe the same
   place. The anchor is known to be wrong, so the live fix wins regardless of
   its larger error, and the response is flagged `moved: true`.
3. Otherwise the smallest accuracy radius wins.

`moved_m` — the haversine distance from the anchor to the current best fix — is
reported alongside, so the phone can show "moved 340 m since armed" rather than
two coordinates the user has to compare by eye.

### Lifecycle

The tracker starts on `Arm()` and stops on `Disarm()`, matching the requested
behaviour of live location in arm mode. The last fix is retained after disarm so
the panel can show a last-known position instead of going blank.

### Privacy

`location.enabled` defaults to **false**. With it off, no scan runs and no
request leaves the machine — the project's no-cloud promise is unchanged for
anyone who does not opt in. With it on:

- every outbound geolocation request is logged to the dashboard,
- `geolocate_key` is stripped from the config payload sent to the phone and
  replaced by `has_geolocate_key: true`, exactly as the PIN is handled today.

### Protocol

Client to server:

- `location_anchor` — `{location: {lat, lon, accuracy_m}}`, sent by the phone
  after arming and whenever the user refreshes.
- `get_location` — request the current fix.

Server to client:

- `location` — `{location, anchor, moved_m, moved, enabled, available}`.

Pushed on arm, on every poll while armed, on request, and attached to `alert`
and `alarm_active` messages so an alarm notification carries a position.

### Configuration

```go
type LocationConfig struct {
    Enabled      bool   `json:"enabled"`        // default false
    PollSeconds  int    `json:"poll_seconds"`   // default 60, floor 15
    PhoneAnchor  bool   `json:"phone_anchor"`   // default true
    IPFallback   bool   `json:"ip_fallback"`    // default true
    WiFiEnabled  bool   `json:"wifi_enabled"`   // default false
    GeolocateURL string `json:"geolocate_url"`  // default Google endpoint
    GeolocateKey string `json:"geolocate_key"`  // never sent to the client
}
```

### Presentation

The default panel is offline-safe and shows coordinates, the accuracy radius, a
source badge, fix age, and distance moved since arming, with copy and
open-in-maps actions. When the phone has internet it additionally renders an
OpenStreetMap tile view. Tiles are a progressive enhancement: on a purely local
network the panel is fully functional without them.

### Testing

- Parser fixture tests per platform, following `internal/monitor/testdata`.
- Haversine against known city-pair distances.
- Fix selection as a table test, including the anchor-is-wrong branch.
- Tracker driven by a fake provider, so the loop is tested without network.
- E2E: arm with location enabled and a stub provider, assert a `location`
  message arrives.

---

## Workstream C — Frontend rewrite

### Stack

Vite + TypeScript + Preact + `@preact/signals`.

Preact rather than Svelte because Biome — already the CI gate for `web/` — lints
`.ts` and `.tsx` natively and does not understand `.svelte`. Choosing Svelte
would mean either dropping the existing frontend CI job or adding a second
toolchain to preserve it. Preact keeps one linter and costs about 12 KB gzipped.

### The build artifact question

The binary embeds `web/` via `go:embed`, so a build step changes what `go build`
means. `web/dist` is **committed**: a fresh clone builds a working binary with no
Node installed, and `go install` keeps working for anyone who has never heard of
Vite.

The obvious failure mode of a committed artifact is drift — someone edits source
and forgets to rebuild. CI closes it: the frontend job rebuilds from source and
fails if the result differs from the committed `dist`.

### Layout

```
web/
  embed.go            embeds dist/
  dist/               committed build output
  index.html
  package.json  tsconfig.json  vite.config.ts
  src/
    main.tsx  app.tsx
    lib/       transport.ts (WebSocket + BLE), protocol.ts, store.ts
    components/ ArmButton, SensorCard, AlertFeed, LocationPanel,
                SettingsSheet, PinDialog, AlarmOverlay, Toast
    styles/    tokens.css, global.css
```

`transport.ts` is the piece worth isolating: today the WebSocket and BLE paths
are interleaved through `app.js` with `sendMsg` branching on a global. Behind one
interface, the rest of the UI stops caring which is connected.

### Design direction

Dark, high-contrast security-console aesthetic. The motion budget goes to the
things that carry meaning:

- arm button: spring press, radial hold-to-disarm ring replacing today's linear bar
- sensor list: staggered entrance, state changes cross-fade rather than snap
- alarm overlay: pulsing red field, not a static box
- auth to dashboard: view transition
- settings: bottom sheet instead of the current inline expansion

All motion is gated on `prefers-reduced-motion`.

### Preserved behaviour

Nothing currently reachable in the UI is dropped: pairing including `?key=`
auto-connect, BLE connection, sensor toggles and per-sensor test, the alert feed
with filter, sort and `localStorage` history, the arm countdown, hold-to-disarm,
the PIN dialog, the alarm overlay with all three dismiss modes, the full settings
form including remote access, and the service worker notification path. The
location panel is added.

### CI

The `frontend` job becomes: `npm ci`, `npm run build`, `npx biome ci`, then a
diff of the freshly built output against committed `dist`. Biome's `files.includes`
is widened to `web/src/**/*.{ts,tsx}` and excludes `web/dist`.

---

## Pull request order

| # | Branch | Contents |
|---|---|---|
| 1 | `fix/remote-access-hardening` | Workstream B. No new surface, no protocol change. |
| 2 | `feature/location-tracking` | Workstream A, including a location panel added to the existing vanilla UI so the PR is complete on its own. |
| 3 | `feature/frontend-vite-ts` | Workstream C, stacked on 2, reimplementing every panel including location. |
