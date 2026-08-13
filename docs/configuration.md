# Configuration

Settings are stored in a JSON file and persist across sessions:

| OS | Path |
|----|------|
| Windows | `%APPDATA%\LeaveSafe\config.json` |
| Linux / macOS | `~/.leavesafe/config.json` |

Alongside it: `events.jsonl` (the security event history), `leavesafe.log` (the
application log), `state.json` (whether the machine was armed), and `tls/` (the
self-signed certificate). Both logs rotate on size and keep a couple of
generations, so nothing here grows without bound.

You can change everything from the phone UI, or edit the file directly. A file
that does not parse is moved aside as `config.json.corrupt-<timestamp>` rather
than silently overwritten, and values that would break the program — a zero
heartbeat, a year-long lockout — are clamped with a line in the log saying so.

## All options

| Setting | Default | Description |
|---------|---------|-------------|
| `port` | `0` (auto) | HTTP server port |
| `max_sessions` | `3` | Maximum concurrent clients |
| `max_auth_attempts` | `5` | Failed attempts before lockout |
| `lockout_seconds` | `60` | Lockout duration |
| `heartbeat_seconds` | `15` | Status broadcast interval |
| `disconnect_grace_seconds` | `30` | How long a phone can be gone before the laptop reports it. Not an alarm — only the sensors do that |
| `auto_arm_on_lock` | `false` | Arm automatically when the screen locks |
| `restore_armed_state` | `false` | Re-arm on startup if the last run ended while armed |
| `session_ttl_minutes` | `1440` | How long a paired session lasts; `0` means forever |
| `session_idle_minutes` | `480` | Drop a session idle this long; `0` means never |
| `update_check` | `true` | Ask GitHub whether a newer release exists, and say so on the dashboard and the phone |
| `update_channel` | `stable` | Which releases count: `stable`, or `beta` for prereleases as well |
| `update_check_hours` | `24` | How often to ask; clamped to between 6 hours and a week |
| `connection_mode` | `wifi` | Transport mode (`wifi`, `bluetooth`, or `both`) |
| `location.enabled` | `false` | Report where this machine is while armed |
| `location.phone_anchor` | `true` | Use the paired phone's position when arming |
| `location.ip_fallback` | `true` | Look up the public IP for a city-level position |
| `location.wifi_enabled` | `false` | Resolve a Wi-Fi scan through a geolocation service |
| `location.geolocate_key` | none | API key for that service; never sent to a client |
| `location.poll_seconds` | `60` | How often to refresh the position while armed |
| `pin_protection.enabled` | `false` | Require a PIN to disarm |
| `alarm.escalation_enabled` | `false` | Enable volume escalation levels |
| `enabled_sensors.*` | `true` | Toggle individual sensors on/off. Every sensor watches unless this says otherwise |
