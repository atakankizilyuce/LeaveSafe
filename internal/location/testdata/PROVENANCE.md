# Fixture provenance

These files are the input to the location parsers. A parser test is only worth
something if its input is what the service or the operating system really
produces, so every file here records where it came from.

Files are byte-exact: no header, no marker, nothing a parser would have to strip.
Provenance lives in these tables instead.

The same rule as `internal/monitor/testdata` applies — a fixture either came
from the real thing or says that it did not.

## Captured from a real source

| File | Source | Captured on |
| --- | --- | --- |
| `ipapi_co.json` | `GET https://ipapi.co/json/` | live request, 2026-07-28 |
| `windows/netsh_wlansvc_stopped.txt` | `netsh wlan show networks mode=bssid` | Windows 10 desktop with no Wi-Fi adapter |

`ipapi_co.json` is a real response with the identifying fields replaced: the
address became `203.0.113.7` (RFC 5737 documentation range), and the network,
ASN and organisation were replaced with placeholders. The coordinates, city and
country are untouched, because those are what the parser reads.

`netsh_wlansvc_stopped.txt` is what a machine with the WLAN AutoConfig service
stopped prints instead of a report. It is in here because it is the realistic
failure mode on a desktop, and the parser must return no radios from it rather
than inventing one.

## Written by hand from the documented format

No machine available to this project can produce these, so they were written
from the documented output format. Unlike the sensor fixtures, this gap cannot
be closed by the **Capture Fixtures** workflow: no GitHub-hosted runner has a
Wi-Fi radio, so none of them can scan for access points.

| File | Format source | Why it could not be captured |
| --- | --- | --- |
| `windows/netsh_networks_bssid.txt` | `netsh wlan show networks mode=bssid` | the development machine has no Wi-Fi adapter and hosted runners have none either |
| `linux/nmcli_wifi_terse.txt` | `nmcli-dev(1)`, terse output with `\:` escaping | no NetworkManager and no Wi-Fi radio on any available Linux machine |
| `linux/iw_scan.txt` | `iw(8)` scan output | as above; `iw` additionally requires root |

The MAC addresses in the hand-written fixtures are not real. They exercise the
shapes the parsers have to survive: mixed upper and lower case, a 5 GHz stanza
with no `DS Parameter set` line, the ` -- associated` suffix `iw` appends to the
connected BSS, and rate lines full of bare numbers that must not be mistaken for
a signal reading.

## macOS

There are no macOS fixtures because there is no macOS parser. Apple does not
expose access point BSSIDs to an unentitled process: `airport -s` was removed in
macOS 14.4, `system_profiler SPAirPortDataType` reports neighboring networks
without any BSSID, and CoreWLAN requires a Location Services authorization that
a single self-contained binary cannot obtain. `scan_darwin.go` says so and
reports the provider unavailable rather than returning something invented.
