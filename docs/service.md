# Starting at login, and surviving a restart

## Start it when you log in

A theft monitor that does not survive a reboot has a hole in it: the machine
restarts — a flat battery, an update, someone holding the power button — and
comes back watching nothing.

```bash
leavesafe install-service     # start automatically at login
leavesafe service-status      # is it registered, is it running
leavesafe uninstall-service   # stop doing that
```

It registers a systemd user unit on Linux, a LaunchAgent on macOS, and a
Scheduled Task on Windows. No administrator rights are needed on any of them,
and nothing runs as root — LeaveSafe needs your session to read the screen lock
and input state, and has no reason to be more privileged than you.

The background copy runs with `-headless`: no dashboard, no QR code, and its
output goes to `leavesafe.log` in the config directory. Since there is no screen
to read a fresh key from, **it stores its pairing key** in `pairing.key`,
readable only by you. That is what lets your phone reconnect after the restart
the whole feature exists to cover. Delete the file to force a new key on the
next start.

> **On Linux**, a user unit is torn down at logout and will not start on an
> unattended boot until you allow lingering: `sudo loginctl enable-linger $USER`.
> `install-service` prints this.

## When it stops watching

LeaveSafe records whether it was armed. If a run ends while armed — a crash, a
flat battery, a reboot, or someone closing the window precisely because it was
watching them — the next start says so, and when:

```
[!] LeaveSafe was ARMED when it last stopped (armed at 2026-07-28 14:02:11).
    This machine has not been monitored since then.
```

It starts disarmed by default. A freshly booted laptop opens its own lid and
accepts its owner's keystrokes, so re-arming automatically would mean screaming
at the person who just turned it on. Set `"restore_armed_state": true` if you
would rather it re-arm anyway.

A panic in a sensor used to take the whole process down with it. Those loops are
supervised now: the failure is logged, written to the event history, shown on the
dashboard, and the loop restarts. The rest of the sensors keep watching while it
does — and the station for the failed one reads `fault` on the phone rather than
`ready`, because claiming cover it has not got is the one lie an alarm cannot
afford.
