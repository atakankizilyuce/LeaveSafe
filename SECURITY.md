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

**The listener is plain HTTP**, and there is no TLS anywhere: a certificate for
a LAN address cannot be vouched for by any authority, and the self-signed one
that used to guard the internet-facing listener went with that listener.

What crosses it is no longer plain. A paired connection is sealed with a key
derived from the pairing key and the two handshake nonces — one key per
direction, ChaCha20-Poly1305, a counter that must strictly increase — so the
status, the alarms, the position and the PIN are unreadable to anything on the
network, and a frame nobody could seal does not open. A machine on the path can
still relay the handshake itself; it cannot compute the key that handshake
produces, so the conversation after it is closed to it.

**Sealing is not optional, and cannot be talked out of.** It used to be: an app
that did not ask for it paired and carried on in the clear, which was a kindness
to old apps and was also a way in. The field naming the construction sat outside
both proofs, so a machine on the path deleted it and the laptop obligingly held
a plaintext conversation with a phone that had asked for an encrypted one —
neither end any the wiser, and a `disarm` to inject and an alarm frame to drop
for whoever had done it. The field is inside both proofs now, so it cannot be
edited unnoticed, and there is no longer a value of it that means "do not seal":
an app that will not is refused, and told which end is out of date.

One thing is still readable there: that a connection exists, and how much
traffic it carries.

**Nothing is reachable from outside your network.** LeaveSafe binds to the local
interfaces and asks nothing of your router. A phone that is not on the same
network cannot reach the laptop and is not told anything.

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

## What pairing proves, and what it does not

The pairing key rides in the URL **fragment**, which is never put on the wire:
it reaches the page's own JavaScript and no server sees it. Older builds put it
in the query string, so the first request line already carried it to whatever
answered. A QR code containing `?key=` is from such a build; treat that key as
disclosed and run `rotate-key`.

**Both ends prove they hold the key.** The greeting carries a random challenge,
the app answers it with an HMAC over the key and a challenge of its own, and the
acceptance carries the laptop's answer to that one. The key is in none of it,
and there is no longer a field for it to arrive in — an app old enough to send
one is refused with a message naming which end is out of date. So a machine that
took the laptop's address after a reboot, or one interposing on it, is not handed
the key by a phone that scanned a code printed for the real one — it cannot
answer the challenge, and the app refuses it.

**The proofs cost something to guess at.** They are HMACs under a key stretched
out of the sixteen digits with Argon2id — 32 MiB, three passes, under a salt
this machine minted with its key — rather than under the digits themselves. It matters because a proof is guessable *offline*:
anything that watched one pairing has both nonces and both proofs, and can work
through every possible key at home for as long as it likes. There are 10^15 of
them, which under a bare HMAC is about a day and a half of a few graphics cards.
Memory-hard, each guess needs its own 32 MiB, and the same search is measured in
millennia. The owner pays a fifth of a second, once per key, because the result
is cached on both ends.

The salt is not a secret — it travels in the greeting, before anything has been
proved — and it does not need to be. What it buys is that the work is specific
to one machine: a table built against one installation is worth nothing against
the next, and rotating the key mints a new salt with it, so whatever was built
against the old key goes with it.

The alternative was a longer key, and it is a worse one: these digits are read
off a screen and typed into a phone.

**What follows the pairing is sealed**, which is a separate claim. The
handshake produces a session key as well as a verdict — HKDF-SHA256 over that
same stretched key with both nonces as the salt, a separate key for each
direction — and every message after `auth_ok` is ChaCha20-Poly1305 under it,
with a counter that must strictly increase so a recorded frame is worth nothing
played again. The stretch is in front of this too: HKDF is fast by design, so
deriving straight from the digits would have left a recorded conversation open
to the very same offline search.

That is what closes the relay: an attacker on the path can forward both proofs
unchanged and be believed by both ends, but it cannot derive the key those
proofs were made with, so it can neither read what follows nor write anything
that opens. A frame that does not open closes the connection rather than being
skipped.

**What none of it proves is who scanned the code.** The pairing key is shown on
a screen, and a phone that photographs it over your shoulder holds exactly what
a real one does — the handshake cannot tell two holders of the same secret
apart, and neither can the session it derives. That is a property of the design
rather than a gap in it: the sixteen digits on the laptop are the whole identity
either end has. Scan on a network you trust, and `rotate-key` if a code was seen.

## Dependencies

Dependabot updates them and every push runs `govulncheck`. If you see an advisory
CI has not caught, open a normal issue — a public advisory is public already.
