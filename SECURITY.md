# Security policy

LeaveSafe guards a laptop somebody walked away from. A flaw here does not leak
data — it lets a machine be taken while its owner believes it is watched.

## Reporting a vulnerability

**Do not open a public issue.** Use GitHub's
[Report a vulnerability](https://github.com/atakankizilyuce/LeaveSafe/security/advisories/new)
form. If it is unavailable to you, open a normal issue saying only that you have
found a security problem and would like a private channel — no details.

Include what an attacker gains in terms of the laptop and the alarm, how to
reproduce it, the platform, `leavesafe -version`, and which sensors were
on, since that changes what is reachable.

Expect acknowledgement within 5 days, an assessment within 14, and a fix as soon
as it is ready — a release of its own for anything that suppresses an alarm or
pairs without the key. You get credit unless you would rather stay anonymous. We
will not take legal action against anyone who reports in good faith, tests only
machines they own, and gives us a chance to fix it before going public.

## Supported versions

One line: the latest release. Check the problem is still present in it before
reporting.

Because there is one line, LeaveSafe asks GitHub once a day whether a newer
release exists. That discloses one request a day to `api.github.com`, from which
GitHub sees your IP and — from the `User-Agent` — that it is LeaveSafe and which
version. Nothing else: no identifier, no configuration, no sensor data, no record
of whether you are armed. Nothing is downloaded and nothing is replaced, and
everything the endpoint returns is treated as untrusted. `"update_check": false`
switches it off.

## In scope

Anything that lets someone:

- Pair without the 16-digit key, or work around the per-address lockout.
- Arm, disarm, dismiss an alarm, or read the machine's position without a valid
  session.
- Stop a real sensor event from reaching a paired phone.
- Read the pairing key, the disarm PIN, a session token, or a geolocation API key
  out of the running program, its config directory, or its network traffic.
- Reach the filesystem or run code through the HTTP server, the WebSocket
  protocol, or the BLE transport.
- Escalate from the phone UI to anything the phone should not control.

A release workflow that could be made to publish a binary not built from `main`
counts too.

## Not in scope

Known properties of the design, not bugs. A way to *improve* one is very welcome
as an issue or a pull request.

**Administrator access to the running machine wins.** They can stop the process,
read its memory, or edit its config. This defends a laptop against someone
walking past it, not against someone who already owns it.

**A four-digit PIN is guessable in ten thousand tries.** scrypt-hashed so it is
not cleartext in `config.json`, and rate-limited to five guesses per address per
minute. A speed bump against someone holding an unlocked paired phone, not a
second factor.

**The listener is plain HTTP**, so a pairing key sent over the LAN can be read by
a hostile machine on the same Wi-Fi. There is no TLS anywhere: a certificate for
a LAN address cannot be vouched for by any authority, and the self-signed one
that used to guard the internet-facing listener went with that listener. Treat
an untrusted network as a place where the key is visible, and `rotate-key`
afterwards.

**Nothing is reachable from outside your network.** LeaveSafe binds to the local
interfaces and asks nothing of your router. A phone that is not on the same
network cannot reach the laptop and is not told anything.

**Bluetooth pairing runs on macOS only.** The Windows and Linux stacks do not
report which device performed a write, so every device in radio range collapses
into one client and a single phone pairing would authenticate all of them without
the key. LeaveSafe refuses to advertise there rather than offer that. Wi-Fi
pairing is unaffected everywhere.

**The event log records when the machine was left alone.** Owner-readable only,
so anyone who can read your home directory can read it.

**LeaveSafe answers only to its IP address**; a `Host` header carrying a DNS name
is refused with 421. That is the DNS rebinding defense, and the WebSocket's
Origin check cannot provide one — a rebound page sends the attacker's own domain
as both Origin and Host, so they match. Every address LeaveSafe hands out is an
address literal, so this costs the documented flow nothing.

**Sensor changes are refused while armed.** A toggle is one tap; disarming is a
deliberate hold plus an optional PIN. Allowing toggles while armed would let
anyone holding the paired phone switch every sensor off without passing the
disarm check, while the panel still read ARMED.

## What pairing does not prove

The pairing key rides in the URL **fragment**, which is never put on the wire:
it reaches the page's own JavaScript and no server sees it. Older builds put it
in the query string, so the first request line already carried it to whatever
answered. A QR code containing `?key=` is from such a build; treat that key as
disclosed and run `rotate-key`.

**What the phone cannot check is who answered.** The connection is plain HTTP,
so there is no certificate to compare and nothing identifies the far end before
the key is offered. Anything that can answer the laptop's address on your network
— a machine that took the address after a reboot, or one interposing on it — is
handed the key by a phone that scanned a code printed for the real one.

Closing that needs a password-authenticated key exchange, so the key never leaves
the phone at all. Worth doing; not done yet. Until then, scan on a network you
trust.

## Dependencies

Dependabot updates them and every push runs `govulncheck`. If you see an advisory
CI has not caught, open a normal issue — a public advisory is public already.
