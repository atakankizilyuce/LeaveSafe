# Security policy

LeaveSafe guards a laptop somebody walked away from. A flaw here does not leak
data — it lets a machine be taken while its owner believes it is watched. Please
report anything you find.

## Reporting a vulnerability

**Do not open a public issue for a security problem.**

Report it privately through GitHub's
[Report a vulnerability](https://github.com/atakankizilyuce/LeaveSafe/security/advisories/new)
form. It reaches the maintainers and nobody else, and it gives us a private
place to work on a fix with you.

If that form is unavailable to you, open a normal issue saying only that you
have found a security problem and would like a private channel — no details —
and a maintainer will get back to you.

Please include, as far as you can:

- What an attacker gains, in terms of the laptop and the alarm.
- The steps to reproduce it, and the platform you saw it on.
- The version: `leavesafe -version`.
- Whether remote access was on, since that changes what is reachable.

### What to expect

- **Acknowledgement within 5 days.** If you have heard nothing after a week,
  assume the report was lost and ping the issue tracker without details.
- **An assessment within 14 days**, saying whether we agree it is a
  vulnerability and what severity we think it carries.
- **A fix released as soon as it is ready.** For anything that lets an attacker
  suppress an alarm or pair without the key, that means a release of its own
  rather than waiting for the next batch of features.
- **Credit in the release notes and the advisory**, unless you would rather stay
  anonymous. Say which you prefer.

We will not take legal action against anyone who reports a flaw in good faith,
tests only against machines they own, and gives us a reasonable chance to fix it
before going public.

## Supported versions

| Version | Supported |
|---------|-----------|
| Latest release | ✅ Fixes are released against it |
| Anything older | ❌ Please upgrade before reporting |

There is one supported line. Before reporting, check that the problem is still
present in the latest release — and in `main` if you can build it.

## The update check, and what it tells GitHub

Because there is one supported line, a copy left running on an old one is a real
risk — so LeaveSafe asks GitHub whether a newer release exists and reports it on
the dashboard and to the paired phone. It asks **once a day** by default, not
once per start: a copy installed with `install-service` runs for weeks, and
checking only at startup means the installations most in need of a fix are the
least likely to hear about one.

What this discloses is one request a day to `api.github.com`, from which GitHub
can see your IP address and — from the `User-Agent` — that it is LeaveSafe and
which version. Nothing else is sent: no identifier, no configuration, no sensor
data, and no record of whether you are armed. There is no server of ours involved
and no telemetry of any kind.

**Nothing is downloaded and nothing is replaced.** The check reports; upgrading
stays a thing you do. A security program that silently rewrites itself from the
network is a larger trust decision than the one made by downloading a single file.

Everything the endpoint returns is treated as untrusted: a version tag that does
not look like a version is discarded, a link that does not point at `github.com`
is replaced with the releases page, and the upgrade command shown to you comes
from a fixed table in the binary rather than from anything GitHub sent.

To switch it off entirely, set `"update_check": false` in the config or turn it
off from the phone's settings screen. `"update_check_hours"` changes how often it
asks, and `"update_channel": "beta"` opts into prereleases.

## What is in scope

Anything that lets someone:

- Pair without the 16-digit key, or work around the per-address lockout.
- Arm or disarm, dismiss an alarm, or read the machine's position without a
  valid session.
- Stop a real sensor event from reaching a paired phone.
- Read the pairing key, the disarm PIN, a session token, or a geolocation API
  key out of the running program, its config directory, or its network traffic.
- Reach the machine's filesystem or run code on it through the HTTP server, the
  WebSocket protocol, or the BLE transport.
- Escalate from the phone UI to anything the phone should not control.

Reports about the release pipeline — a workflow that could be made to publish a
binary that was not built from `main` — are in scope too.

## What is not in scope, and why

These are known properties of the design rather than bugs. Please do not report
them as vulnerabilities; if you have a way to *improve* one, a normal issue or
pull request is very welcome.

**An attacker with administrator access to the running machine wins.** They can
stop the process, read its memory, or edit its config. LeaveSafe defends a
laptop against someone who walks past it, not against someone who already owns
it.

**The TLS certificate is self-signed.** There is no certificate authority that
could vouch for a laptop's LAN address. The phone will warn on first connection.
Comparing the fingerprint — shown by the `cert` command, on the pairing screen,
and carried in the QR code — is what tells that warning apart from an actual
interception. See "TLS and what pairing does not prove" below for the limit of
this.

**A four-digit disarm PIN is guessable in ten thousand tries.** It is hashed
with scrypt so it is not sitting in cleartext in `config.json`, and guesses are
rate-limited to five per address per minute. It is a speed bump against someone
holding an unlocked paired phone, not a second factor.

**Remote access publishes a port to the internet.** That is what it is for, and
it is off by default and opt-in per install. With it on, the pairing key is the
only thing between the internet and the alarm.

**The event log records when the machine was left alone.** It is written
owner-readable only. Anyone who can read your home directory can read it.

## TLS and what pairing does not prove

The certificate fingerprint travels in the QR code, and the phone refuses to
send the pairing key if the server reports a different one. That catches a
misdirected connection — wrong host, stale port forward, a certificate that
changed since the code was printed — and it means the fingerprint is on the
phone's own screen for comparison against the browser warning, rather than back
on the laptop where nobody looks.

It is not proof against a determined interceptor. A browser gives page
JavaScript no way to see the certificate of its own connection, so the
comparison relies on what the server says about itself, and a proxy that
terminates TLS with the user's accepted certificate and relays honestly to the
real server would pass it. Closing that properly needs a password-authenticated
key exchange over the socket, so the pairing key never leaves the phone at all.
That is a change worth making and it has not been made yet.

Until then: **compare the fingerprint on the pairing screen against the one in
your browser's certificate warning before you accept it**, especially over
remote access. They should match, and they should keep matching on every later
connection.

## Reporting a dependency vulnerability

Dependencies are updated by Dependabot, and every push runs `govulncheck`. If
you see an advisory that CI has not caught, open a normal issue — a public
advisory is public already.
