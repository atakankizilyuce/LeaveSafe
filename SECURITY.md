# Security policy

LeaveSafe guards a laptop somebody walked away from. A flaw here does not leak
data — it lets a machine be taken while its owner believes it is watched.

## Reporting a vulnerability

**Do not open a public issue.** Use GitHub's
[Report a vulnerability](https://github.com/atakankizilyuce/LeaveSafe/security/advisories/new)
form. If it is unavailable to you, open a normal issue saying only that you have
found a security problem and would like a private channel — no details.

Include what an attacker gains in terms of the laptop and the alarm, how to
reproduce it, the platform, `leavesafe -version`, and whether remote access was
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

**The TLS certificate is self-signed** — no authority can vouch for a LAN
address — so your phone warns on first connection. Comparing the fingerprint,
shown by `cert`, on the pairing screen and in the QR code, is what tells that
warning apart from an interception. Its limit is below.

**A four-digit PIN is guessable in ten thousand tries.** scrypt-hashed so it is
not cleartext in `config.json`, and rate-limited to five guesses per address per
minute. A speed bump against someone holding an unlocked paired phone, not a
second factor.

**Remote access publishes a port to the internet.** That is what it is for. Off
by default, opt-in per install, and with it on the pairing key is the only thing
between the internet and the alarm.

**The local listener is plain HTTP, remote access or not,** so a pairing key sent
over the LAN can be read by a hostile machine on the same Wi-Fi. Remote access
runs on a second, TLS-only listener rather than converting this one — which is
what lets the setting change without disturbing a connected phone.

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

The certificate fingerprint travels in the QR code and the phone refuses to send
the key if the server reports a different one. That catches a misdirected
connection — wrong host, stale port forward, a certificate changed since the code
was printed — and puts the value on the phone's own screen for comparison against
the browser warning.

Both the key and the fingerprint ride in the URL **fragment**, which is never put
on the wire: the key reaches the page's own JavaScript and no server sees it.
Older builds put it in the query string, so the first request line already
carried it to whatever answered. A QR code containing `?key=` is from such a
build; treat that key as disclosed and run `rotate-key`.

**It is not proof against a determined interceptor.** A browser gives page
JavaScript no way to see the certificate of its own connection, so the comparison
relies on what the server says about itself — and a proxy terminating TLS with
the user's accepted certificate, relaying honestly to the real server, would pass
it. Closing that needs a password-authenticated key exchange so the key never
leaves the phone. Worth doing; not done yet.

Until then: **compare the fingerprint on the pairing screen against the one in
your browser's warning before accepting it**, especially over remote access.

## Dependencies

Dependabot updates them and every push runs `govulncheck`. If you see an advisory
CI has not caught, open a normal issue — a public advisory is public already.
