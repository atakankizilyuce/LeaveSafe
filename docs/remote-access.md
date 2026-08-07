# Remote access

By default LeaveSafe only accepts connections from your local network. **Remote
access** publishes the port so your phone can reach the laptop over mobile data
or from another network.

You are asked which mode you want **every time you start LeaveSafe in a
terminal**, with your current setting as the default — pressing enter keeps it.
You can also change it at any time from the phone's settings screen, or with the
`mode` command in the terminal.

The very first start asks one question before that: Turkish or English. It is
asked once, remembered, and reaches those startup questions and nothing else —
the dashboard, the log and the phone are in English. `lang` changes it for the
next start.

**The change takes effect immediately.** Nothing restarts, and a phone connected
over Wi-Fi stays connected while it happens: remote access runs on a second
listener of its own, and the local one is never touched. Turning it off drops
phones connected *through* it, which is the point.

Understand what you are turning on: remote access makes the port reachable from
the internet, and the 16-digit pairing key becomes the only thing between a
stranger and your alarm.

## What happens when you enable it

1. A self-signed TLS certificate is generated in the config directory, so the
   connection is HTTPS and the pairing key is encrypted in transit.
2. The internet-facing listener comes up, and LeaveSafe carries on starting.
   Steps 3 and 4 happen in the background — a network with no UPnP gateway takes
   the better part of a minute to say so, and none of it is worth waiting at a
   blank screen for.
3. A UPnP port mapping is requested from your router and renewed every 30
   minutes.
4. The public IP is discovered over STUN, falling back to an HTTPS lookup.
5. The dashboard lists the public URL alongside the local ones, and says whether
   it can actually be reached. **The QR code stays on a local address until the
   port mapping succeeds** — an address nothing will carry a connection to is
   worse than no address at all, because a phone scans it and waits.

**If the certificate cannot be created, remote access does not start.**
LeaveSafe will not serve an internet-facing port over plain HTTP, because that
would put your pairing key on the wire in cleartext. It stays available on the
local network and tells you what went wrong.

## The certificate warning

The certificate is self-signed, so your phone will warn you the first time. That
warning is expected — and it looks exactly the same as a real interception.

The QR code carries the certificate's fingerprint, so the pairing screen shows
you the value to check without walking back to the laptop. Compare it against
the one in your browser's warning before accepting. The phone also refuses to
send the pairing key at all if the server reports a fingerprint other than the
one the code named.

That catches a connection that landed somewhere unintended. It is not proof
against a determined interceptor — a browser gives page JavaScript no way to see
the certificate of its own connection, so the automatic check is on what the
server says about itself. [`SECURITY.md`](../SECURITY.md) sets out the limit in
full. `cert` in the terminal still prints the fingerprint if you want to check
from the other end.

## If your router has no UPnP

UPnP is off by default on many routers. When it fails, LeaveSafe keeps running
and names the port on the dashboard and in the phone's settings screen. Forward
that TCP port to your laptop manually in your router's admin page, and the
public URL works as normal.

Until you have, the public address is listed but is **not** the code on screen:
the QR stays on a local address, because one pointing outside would not connect.
Run `urls` and `qr <n>` to switch to it once the port is forwarded.

## If your ISP uses carrier-grade NAT

This one no amount of port forwarding fixes. Some providers — mobile networks
especially, and plenty of home connections in some countries — hand out an
address in `100.64.0.0/10` and keep the routable one for themselves. Your router
cannot forward a port it does not own, so the laptop simply cannot be reached
from outside.

LeaveSafe detects this and says so plainly rather than leaving you to guess why
nothing connects: remote access is stopped, and the phone's settings screen
explains why. Your local network is unaffected. Getting past it means asking
your provider for a public address, or putting both devices on a VPN or mesh
network — neither of which LeaveSafe does for you.

## Pairing at home while remote access is on

Scanning the public URL from a phone on the same Wi-Fi requires your router to
support NAT hairpinning, and plenty do not. If the QR code will not connect
while you are sitting next to the laptop, run `urls` to see the local address
and `qr <n>` to show its code instead.
