# LeaveSafe

**Leave your laptop. Stay safe.**

A lightweight, cross-platform device security monitor that turns your phone into a remote alarm system — no cloud, no accounts, just a QR code scan.

[![CI](https://github.com/atakankizilyuce/LeaveSafe/actions/workflows/ci.yml/badge.svg)](https://github.com/atakankizilyuce/LeaveSafe/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.25.0-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![Platforms](https://img.shields.io/badge/platform-windows%20%7C%20linux%20%7C%20macOS-lightgrey)](#platform-support)

</div>

---

## What is LeaveSafe?

LeaveSafe runs on your laptop as a terminal dashboard and lets you **arm a security monitor** from your phone by scanning a QR code. Once armed, it watches the laptop's sensors (charger, lid, USB, screen lock, network) and sends instant alerts to your phone if anything changes while you're away.

No internet connection required. Communication is local-only over WebSocket, secured with a 16-digit Luhn-validated pairing key.

---

## Features

- **QR Code Pairing** — Scan once from your phone's browser, no app required
- **Multi-Sensor Monitoring** — Power/charger, lid, USB, screen lock, and network changes
- **Arm / Disarm Remotely** — Control the alarm from your phone in real time
- **Live TUI Dashboard** — ASCII terminal UI with live logs and system status
- **Rate Limiting & Lockout** — 5 failed auth attempts triggers a 60-second lockout
- **Session Management** — Maximum 3 concurrent authenticated clients
- **Graceful Alarm** — Triggers automatically if all clients disconnect while armed
- **Cross-Platform** — Native sensor implementations for Windows, Linux, and macOS
- **Docker Support** — Run in a container with privileged hardware access

---

## Build Matrix

| Target | Status |
|--------|--------|
| Windows / amd64 | [![Build](https://github.com/atakankizilyuce/LeaveSafe/actions/workflows/ci.yml/badge.svg?label=windows)](https://github.com/atakankizilyuce/LeaveSafe/actions/workflows/ci.yml) |
| Linux / amd64 | [![Build](https://github.com/atakankizilyuce/LeaveSafe/actions/workflows/ci.yml/badge.svg?label=linux)](https://github.com/atakankizilyuce/LeaveSafe/actions/workflows/ci.yml) |
| macOS / amd64 | [![Build](https://github.com/atakankizilyuce/LeaveSafe/actions/workflows/ci.yml/badge.svg?label=darwin-amd64)](https://github.com/atakankizilyuce/LeaveSafe/actions/workflows/ci.yml) |
| macOS / arm64 | [![Build](https://github.com/atakankizilyuce/LeaveSafe/actions/workflows/ci.yml/badge.svg?label=darwin-arm64)](https://github.com/atakankizilyuce/LeaveSafe/actions/workflows/ci.yml) |

---

## How It Works

```
┌─────────────────────────────┐        ┌──────────────────────┐
│   Laptop  (leavesafe)       │        │   Phone / Tablet     │
│                             │        │                      │
│  ┌───────────┐              │        │  Open browser URL    │
│  │  TUI      │  QR Code ───────────▶ │  Scan QR code        │
│  │ Dashboard │              │        │  Authenticate        │
│  └───────────┘              │        │                      │
│       │                     │◀──────────── Arm / Disarm     │
│  ┌────▼────────────────┐    │        │                      │
│  │  Sensor Monitor     │    │        │  Receive Alerts ◀──  │
│  │  · Power/Charger    │────┼────────▶  (lid opened,       │
│  │  · Lid open/close   │    │        │   USB plugged, etc.) │
│  │  · USB connect      │    │        └──────────────────────┘
│  │  · Screen lock      │    │
│  │  · Network change   │    │
│  └─────────────────────┘    │
└─────────────────────────────┘
```

**Pairing Flow:**
1. Run `leavesafe` on your laptop — a QR code and pairing URL appear in the terminal
2. Scan the QR code with your phone's camera or open the URL in a browser
3. The app authenticates using the embedded 16-digit pairing key
4. Tap **Arm** on your phone — the laptop is now being monitored
5. Any sensor event triggers an alert card on your phone instantly

---

## Getting Started

### Download Binary

Grab the latest pre-built binary from the [Releases page](https://github.com/atakankizilyuce/LeaveSafe/releases).

| Platform | File |
|----------|------|
| Windows 64-bit | `leavesafe-windows-amd64.exe` |
| Linux 64-bit | `leavesafe-linux-amd64` |
| macOS Intel | `leavesafe-darwin-amd64` |
| macOS Apple Silicon | `leavesafe-darwin-arm64` |

### Build from Source

```bash
# Requires Go 1.25+
git clone https://github.com/atakankizilyuce/LeaveSafe.git
cd LeaveSafe

# Build for your current platform
go build -o leavesafe ./cmd/leavesafe

# Or use the Makefile to build all platforms
make all
```

### Docker

```bash
docker-compose up
```

> **Note:** The container runs with `privileged: true` and mounts `/sys` and `/proc` for full sensor access on Linux.

---

## Usage

Run the binary:

```bash
./leavesafe
```

The terminal dashboard opens with:
- A **QR code** on the left — scan it with your phone
- A **status panel** on the right showing armed state, connected clients, active sensors, and the pairing key
- A **live log** area at the bottom

**Interactive Commands** (type at the prompt):

| Command | Description |
|---------|-------------|
| `test` | Send a test alert to all connected clients |
| `help` | Show available commands |
| `Ctrl+C` | Graceful shutdown |

**On your phone:** Open the URL shown in the terminal (or scan the QR code), authenticate, and use the **Arm** / **Disarm** buttons.

---

## Platform Support

| Sensor | Windows | Linux | macOS |
|--------|:-------:|:-----:|:-----:|
| Power / Charger | ✅ | ✅ | ✅ |
| Lid open / close | ✅ | ✅ | ✅ |
| USB connect / disconnect | ✅ | ✅ | ✅ |
| Screen lock / unlock | ✅ | ✅ | ✅ |
| Network / IP change | ✅ | ✅ | ✅ |

---

## Tech Stack

| Technology | Purpose |
|------------|---------|
| [Go](https://go.dev) 1.25 | Core application language |
| [nhooyr.io/websocket](https://github.com/nhooyr/websocket) | Real-time server ↔ client communication |
| [go-qrcode](https://github.com/skip2/go-qrcode) | QR code generation in the terminal |
| `golang.org/x/term` | TUI / terminal control |
| `golang.org/x/sys` | Platform-specific syscalls for sensor reads |
| Embedded HTML/JS/CSS | Phone web UI served directly from the binary |
| Docker + docker-compose | Container deployment |
| GitHub Actions | CI: test, cross-platform build, Docker build |

---

## Security Model

- **Pairing key** is 16 digits with a Luhn check digit, generated fresh each run
- **Session tokens** are 256-bit random hex strings, never reused
- **Rate limiting** locks out clients for 60 seconds after 5 failed auth attempts
- **Max 3 concurrent sessions** — extra connections are rejected
- **Alarm on disconnect** — if the system is armed and all clients drop, the alarm triggers after a 30-second grace period
- **Local-only** — no data ever leaves your LAN

---

## Contributing

Contributions are welcome. Please open an issue first to discuss what you'd like to change.

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/my-feature`)
3. Commit your changes
4. Open a Pull Request

Run tests before submitting:

```bash
go test ./... -v -race
```

---

## License

Distributed under the [Apache License 2.0](LICENSE).
