# Where the machine is

While armed, LeaveSafe can report where the monitored machine is. **This is off
by default.** With it off, nothing is scanned and no request leaves the machine.
Turn it on in the phone's settings screen.

A laptop has no GPS receiver, so there is no single source of truth. Three are
combined, and every position is shown with the source that produced it and an
honest accuracy radius:

| Source | Accuracy | Needs | Weakness |
|---|---|---|---|
| **Phone GPS on arm** | 5–10 m | nothing | Records where the laptop *was* when you armed it |
| **Wi-Fi positioning** | 20–50 m | API key, internet | Costs money and involves a third party |
| **IP lookup** | ~25 km | internet | Often points at your ISP rather than at you |

## Why the phone's position counts

When you arm the system, your phone is next to the laptop. Its GPS is therefore
the most precise statement available about where the laptop is — for free, with
no third party involved. The catch is that it stops being true the moment the
laptop is carried off, which is exactly when you care.

So LeaveSafe checks the two against each other. When a live fix lands further
from the anchor than both error radii can jointly explain, the two cannot be
describing the same place: the anchor is known to be wrong, the live fix wins
even though it is less precise, and the phone shows **how far the machine has
moved since you armed it**.

A coarse fix never overrules a precise one on its own. An IP lookup 3 km away
that admits to 25 km of error is not evidence of anything.

## Wi-Fi positioning

Set a Google Geolocation API key in settings, or point `geolocate_url` at any
service implementing the same API. Only the access points' MAC addresses and
signal strengths are sent, capped at 24, and never your IP.

## Platform differences

| | Windows | Linux | macOS |
|---|:---:|:---:|:---:|
| Phone GPS on arm | ✅ | ✅ | ✅ |
| IP lookup | ✅ | ✅ | ✅ |
| Wi-Fi positioning | ✅ `netsh` | ✅ `nmcli` / `iw` | ❌ |

**macOS cannot do Wi-Fi positioning, and this will not be fixed here.** Apple
does not give access point BSSIDs to an unentitled process: `airport -s` was
removed in macOS 14.4, `system_profiler SPAirPortDataType` reports neighboring
networks with no BSSID at all, and CoreWLAN requires a Location Services
authorization that a single self-contained binary cannot obtain. macOS is served
by the phone anchor and the IP lookup.

## The map

The panel shows coordinates, an accuracy radius, the source, and the distance
moved — all without touching the network. A map is one tap away and stays
opt-in, because loading it fetches tiles from openstreetmap.org.
