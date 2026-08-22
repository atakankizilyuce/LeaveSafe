# Connection mode: ask every start, apply without a restart

Date: 2026-08-01
Status: approved, ready for planning

## The problem

LeaveSafe supports reaching the laptop over mobile data — `remote_access` in the
config, and a "Reach it from anywhere" toggle on the phone. Two things make it
effectively invisible and unusable:

1. **The startup question asks once and then never again.** `promptRemoteAccess`
   runs only when `cfg.RemoteAccess == nil` (`cmd/leavesafe/main.go:379`).
   Worse, the phone answers it by accident: `remote_access` is a required
   `boolean` in the client payload (`web/src/lib/protocol.ts:101`) and every
   `update_config` writes it (`internal/ws/hub.go:1245`), so the first time a
   user saves any setting from the phone, `nil` becomes `false` and the terminal
   goes silent forever. A start with no usable stdin does the same thing.

2. **Changing it requires a restart, and the restart is disruptive.** The server
   has a single listener. Turning remote access on moves the port to
   `remote_port` and switches the whole server to TLS — the local network
   included (`cmd/leavesafe/main.go:494-524`). So the change cannot be applied
   live without dropping every connected phone, including the one that flipped
   the switch, and forcing a re-pair from the QR code.

There is a third, smaller gap: the phone never learns whether remote access
actually worked. The public URL, the UPnP result and the certificate
fingerprint exist only on the terminal dashboard.

## What this changes

- The terminal asks the connection mode on **every** interactive start, with the
  saved value as the default.
- Changing the mode — from the phone, from the terminal, or at startup — takes
  effect **immediately**, and never disturbs the local-network connection.
- The phone can see whether remote access is actually reachable, and is told
  plainly when it is not and why.

## What this deliberately does not promise

Two failure modes cannot be automated away:

- **UPnP disabled on the router.** The port must be forwarded by hand.
- **CGNAT.** When the ISP does not give the subscriber a routable address, no
  amount of port mapping helps.

The design's obligation in both cases is to *detect and name* the condition
rather than fail silently.

## Architecture

### Two listeners (`internal/server`)

`Server` today assumes one port and one optional TLS certificate. It gains a
second listener. Both serve the same hub over the same mux; only the handler
chain differs, since `securityHeaders` and `socketOrigins` already take a
`tls bool`.

| | Local listener | Remote listener |
|---|---|---|
| Port | `cfg.Port` (0 = OS-assigned), unchanged | `cfg.RemotePort`, default 9443 |
| Scheme | http | https, self-signed |
| Lifetime | opens at start, **never closes** | starts and stops at runtime |
| UPnP | none | mapped, renewed every 30 min |

The local listener never closing is the load-bearing property: it is what lets
the phone that flips the switch stay connected.

**Accepted trade-off.** With remote access on, local traffic is now plain HTTP
where it used to be TLS. This is the same posture as the default Wi-Fi-only
mode, so it is not a new exposure — but it is a change, and it is made
deliberately: the alternative is a self-signed certificate warning on the local
network, which the user must click through by hand. `SECURITY.md` must be
updated to state this.

### New package `internal/remote`

The certificate, UPnP mapping, renewal loop and public-IP discovery currently
sit inline in `main.go:488-556`. With three callers instead of one they move
into a package whose single responsibility is the remote listener's lifecycle.

```go
type State struct {
    Enabled     bool
    PublicURL   string   // "" when the address is unknown
    CertFP      string
    UPnP        UPnPState // ok | failed | cgnat
    ManualPort  int      // the port to forward by hand, 0 when not needed
    Reason      string   // why it is not fully working, "" when it is
}

func (c *Controller) Enable(ctx context.Context) (State, error) // idempotent
func (c *Controller) Disable()                                  // idempotent
func (c *Controller) State() State
```

### Startup prompt (`cmd/leavesafe/main.go`)

`promptRemoteAccess` loses its `RemoteAccess == nil` guard and runs on every
interactive start. The saved value is shown as the bracketed default and a bare
Enter keeps it. It is skipped entirely when `-headless` is set or stdin is not
readable; those starts use the stored value and log which mode they came up in.

Every autostart path already passes `-headless` (`service_windows.go:26`,
`service_linux.go:27`, `service_darwin.go:33`), so no unattended start can block
on this prompt.

### One code path, three entry points

- **Phone**: `update_config` where `remote_access` differs → `Enable`/`Disable`
- **Terminal**: new `mode` console command → same
- **Startup**: same

Each then persists the config, broadcasts the new state to every phone, and
redraws the dashboard's URL list and QR codes. `statusBar` gains
`setURLs([]string)`; URLs and QR codes are currently built once inside
`buildDashboard`.

The `needsRestart` alert drops `remote_access` and `remote_port` from its
condition (`internal/ws/hub.go:1202`). Its message — "Port changed — restart
required to take effect" (`hub.go:1336`) — is also wrong today for the
connection-mode case and is corrected to name whichever setting actually
changed.

### What the phone sees

The status payload carries `State`. The settings sheet renders it under the
toggle:

- the public URL, or that no address has been found yet
- UPnP: working / **router UPnP is off — forward port 9443 by hand** / **your
  ISP uses CGNAT, remote access cannot work on this connection**
- the certificate fingerprint

CGNAT is detected when UPnP succeeds but the router's external address falls in
`100.64.0.0/10`, or when the address STUN reports differs from the one the
router reports.

## Error handling

The stored `remote_access` value records what the **user asked for** and is
never silently rewritten by a failure. Whether remote access is actually
running is `State.Enabled`, which is what the phone and the dashboard display.
Keeping the two separate is what lets a failure be reported instead of
disguised as the user having changed their mind.

| Failure | Behaviour |
|---|---|
| Certificate cannot be created | Remote listener does not start; `State.Enabled` false with the reason; reported to phone and dashboard |
| UPnP fails | Remote listener stays up, `State.Enabled` true; the port to forward by hand is named |
| Public IP unknown | Remote listener stays up; no public URL; reason reported |
| CGNAT detected | Remote listener is stopped — keeping it open serves nothing — `State.Enabled` false with the reason stated plainly |

In every case the local listener is untouched. A remote-access failure never
disconnects the phone.

## Testing

**`internal/remote`**
- `Enable` twice is a no-op the second time; `Disable` likewise
- certificate failure leaves it disabled and reports the reason
- UPnP failure leaves it enabled and reports the manual port
- a router external IP in `100.64.0.0/10` stops it and reports CGNAT

**`internal/server`**
- both listeners serve the same hub concurrently
- **a WebSocket on the local listener survives the remote listener starting and
  stopping** — this is the central claim of the design
- the remote listener refuses plain HTTP

**`cmd/leavesafe`**
- the prompt defaults to the saved value and a bare Enter preserves it
- `-headless` does not prompt
- unreadable stdin does not prompt and does not hang

**`test/e2e`**
- toggling `remote_access` at runtime from a client leaves that client's socket
  connected
