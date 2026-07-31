import { useEffect, useState } from 'preact/hooks';
import { AlarmOverlay } from './components/AlarmOverlay';
import { Annunciator } from './components/Annunciator';
import { ArmControl } from './components/ArmControl';
import { Log } from './components/Log';
import { PairScreen } from './components/PairScreen';
import { PinDialog } from './components/PinDialog';
import { Position } from './components/Position';
import { SettingsSheet } from './components/SettingsSheet';
import { StateHeader } from './components/StateHeader';
import { checkFingerprint, normalizeFingerprint } from './lib/fingerprint';
import { captureAnchor } from './lib/geo';
import type { ServerMessage } from './lib/protocol';
import { clearSession, loadSession, saveSession } from './lib/session';
import { startSiren, stopSiren, warnDisconnected } from './lib/siren';
import {
    alarm,
    appendLog,
    armed,
    armedSince,
    closeTransport,
    config,
    fingerprintVerdict,
    hasToken,
    link,
    loadLog,
    pairError,
    pairing,
    pinPrompt,
    location as position,
    screen,
    send,
    sensors,
    serverFingerprint,
    serverVersion,
    setToken,
    setTransport,
    settingsOpen,
    showToast,
    toast,
    tripSensor,
    updateAvailable,
} from './lib/store';
import { connectBluetooth, connectWebSocket } from './lib/transport';

const PING_MS = 15000;
const RECONNECT_MS = 3000;

/**
 * How long to wait for the server's greeting before giving up.
 *
 * Only applies when the QR code named a certificate to check. Until the
 * greeting arrives the pairing key is held back, so a server that never sends
 * one leaves the phone waiting rather than talking — which is the safe way
 * round, but it has to end in a message instead of a spinner.
 */
const HELLO_TIMEOUT_MS = 6000;

/** A SHA-256 fingerprint is 32 bytes, so 64 hex characters once separators go. */
const SHA256_HEX_LENGTH = 64;

let pingTimer: number | null = null;

/**
 * The fingerprint the QR code named, if it named one. Null for a key typed by
 * hand and for the plain-HTTP local path, where there is no certificate to
 * check and nothing is held back.
 */
let expectedFingerprint: string | null = null;

/** The key waiting on the fingerprint check, held rather than sent. */
let pendingKey: string | null = null;

/** The key the current connection is pairing with, kept so it can be stored. */
let activeKey: string | null = null;

let helloTimer: number | null = null;

/**
 * Whether the pairing key has actually gone out on *this* connection.
 *
 * Without it the certificate check guarded nothing that mattered. It lives in
 * the `hello` branch, so a server that simply never greets skips it entirely —
 * and every other branch acted on whatever arrived. An `auth_ok` sent by
 * something that was never asked anything flipped the page to the panel, and a
 * `pin_required` behind it put the disarm PIN dialog on screen, so the digits
 * guarding the alarm were typed straight into whatever had answered.
 */
let authSent = false;

/**
 * Whether this connection has been authenticated — an `auth_ok` that answers a
 * key this page sent, rather than one that arrived unbidden.
 *
 * Separate from `hasToken()`, which stays true across a dropped socket so the
 * reconnect can tell itself apart from a fresh pairing. During that reconnect
 * the new socket has proved nothing yet, and until it does it is treated like
 * any other stranger.
 */
let sessionLive = false;

function clearHelloTimer() {
    if (helloTimer !== null) {
        window.clearTimeout(helloTimer);
        helloTimer = null;
    }
}

/** Sends the held key, if there is one still waiting. */
function sendPendingKey() {
    if (pendingKey === null) return;
    const key = pendingKey;
    pendingKey = null;
    authSent = true;
    send({ type: 'auth', key });
}

export function App() {
    const [counting, setCounting] = useState<number | null>(null);
    const [autoKey, setAutoKey] = useState('');

    // Reflect the armed state onto the root element. Every colour in the sheet
    // hangs off this one attribute, so arming shifts the whole page at once
    // rather than repainting a badge.
    useEffect(() => {
        document.documentElement.dataset.armed = String(armed.value);
    }, [armed.value]);

    useEffect(() => {
        loadLog();

        // A scanned QR code lands here with the key in the fragment, and — when
        // the laptop is serving HTTPS — the fingerprint of the certificate it
        // is serving. The fragment is where they belong: the browser never
        // sends it to the server, so the key is ours to hold back until the
        // certificate has been checked.
        //
        // The query string is still read, because a code printed by an older
        // build puts them there and refusing it would only strand the user —
        // that request already carried the key, and declining to pair now does
        // not call it back. Fresh codes use the fragment.
        const params = new URLSearchParams(
            window.location.hash.startsWith('#') ? window.location.hash.slice(1) : window.location.search,
        );
        const fromQr = params.get('key');
        const fp = params.get('fp');
        // A fingerprint that does not survive normalisation is not a reason to
        // carry on without one. Left as an empty string it reads as "nothing to
        // check" further down, and the key would go out to a server nobody
        // verified — a damaged scan silently downgrading the very check it was
        // there to perform. Say so and let the user rescan instead.
        const fpDamaged = fp !== null && normalizeFingerprint(fp).length !== SHA256_HEX_LENGTH;
        if (fp && !fpDamaged) expectedFingerprint = normalizeFingerprint(fp);
        if (fromQr || fp) {
            window.history.replaceState({}, document.title, '/');
        }
        if (fpDamaged) {
            pairError.value =
                'That code did not scan cleanly — the certificate fingerprint in it is damaged, ' +
                'so the pairing key was not sent. Scan the code again from the laptop itself.';
            return;
        }
        if (fromQr) {
            setAutoKey(fromQr);
            window.setTimeout(() => pair(fromQr.replace(/\D/g, ''), 'websocket'), 400);
            return;
        }

        // No code in the address bar means this is a return visit — most often
        // because the phone locked its screen and threw the page away. Pick the
        // pairing back up rather than asking for the QR code again.
        const stored = loadSession();
        if (stored) {
            expectedFingerprint = stored.fingerprint;
            window.setTimeout(() => pair(stored.key, 'websocket'), 100);
        }
    }, []);

    function handle(msg: ServerMessage) {
        // Nothing but the handshake is acted on until this connection has
        // proved itself. The three that get through are the handshake: the
        // greeting that carries the certificate, the refusal that answers a key
        // we sent, and the acceptance — and that last one only if there was a
        // key to accept.
        //
        // Everything after the switch assumes a laptop on the other end. Before
        // this guard existed, anything that could answer the phone's socket
        // could open the panel, sound a spoofed alarm on the lock screen, and
        // raise the PIN dialog to collect the code that guards disarming — all
        // without knowing the pairing key, and without ever sending the
        // greeting the fingerprint check reads.
        if (msg.type === 'auth_ok' && !authSent) return;
        if (msg.type !== 'hello' && msg.type !== 'auth_fail' && msg.type !== 'auth_ok' && !sessionLive) {
            return;
        }

        switch (msg.type) {
            case 'hello': {
                clearHelloTimer();
                serverFingerprint.value = msg.cert_fp ?? null;

                const verdict = checkFingerprint(expectedFingerprint, msg.cert_fp);
                fingerprintVerdict.value = verdict;

                if (verdict === 'mismatch') {
                    // The code was printed for a different certificate. Whatever
                    // is on the other end of this socket, it is not the laptop
                    // the user scanned — so it does not get the key.
                    pendingKey = null;
                    pairing.value = false;
                    pairError.value =
                        'This is not the laptop your code came from: it presented a different ' +
                        'certificate. The pairing key was not sent. Scan the code again from the ' +
                        'laptop itself.';
                    closeTransport();
                    return;
                }

                sendPendingKey();
                break;
            }

            case 'auth_ok':
                sessionLive = true;
                setToken(msg.token ?? null);
                serverVersion.value = msg.version ?? null;
                sensors.value = msg.sensors ?? [];
                if (typeof msg.armed === 'boolean') {
                    armed.value = msg.armed;
                    // The laptop's own clock, so a page that was discarded and
                    // reopened resumes the counter instead of restarting it. An
                    // older laptop sends no time, and now is the only honest
                    // answer left.
                    if (!msg.armed) {
                        armedSince.value = null;
                    } else if (msg.armed_since) {
                        armedSince.value = msg.armed_since * 1000;
                    } else {
                        armedSince.value = Date.now();
                    }
                }
                if (activeKey) saveSession(activeKey, expectedFingerprint);
                pairing.value = false;
                pairError.value = null;
                screen.value = 'panel';
                link.value = 'live';
                afterPairing();
                break;

            case 'auth_fail':
                pairing.value = false;
                if (!hasToken()) {
                    // The stored key no longer opens this laptop — rotated, or
                    // from a different one. Keeping it would retry forever.
                    clearSession();
                    const left = msg.remaining_attempts;
                    pairError.value = `${msg.reason ?? 'That key was refused.'}${
                        left ? ` ${left} attempt${left === 1 ? '' : 's'} left.` : ''
                    }`;
                } else {
                    showToast(msg.reason ?? 'Refused');
                }
                break;

            case 'status':
                if (typeof msg.armed === 'boolean') {
                    if (msg.armed && !armed.value) armedSince.value = Date.now();
                    if (!msg.armed) armedSince.value = null;
                    armed.value = msg.armed;
                }
                if (msg.sensor_states) {
                    const states = msg.sensor_states;
                    sensors.value = sensors.value.map((s) => {
                        const next = states[s.name];
                        return next ? { ...s, enabled: next.enabled, status: next.status } : s;
                    });
                }
                link.value = 'live';
                break;

            case 'alert':
                if (msg.alert) {
                    tripSensor(msg.alert.sensor);
                    appendLog({
                        message: msg.alert.message,
                        level: msg.alert.level,
                        sensor: msg.alert.sensor,
                        at: msg.ts ? msg.ts * 1000 : Date.now(),
                    });
                    alarm.value = { message: msg.alert.message, sensor: msg.alert.sensor };
                    startSiren(msg.alert.message);
                }
                break;

            case 'alarm_active':
                if (msg.alert) {
                    tripSensor(msg.alert.sensor);
                    alarm.value = {
                        message: msg.alert.message || 'Something touched your laptop.',
                        sensor: msg.alert.sensor,
                    };
                    startSiren(alarm.value.message);
                }
                break;

            case 'alarm_cleared':
                // Someone else called it off — the laptop's terminal, or another
                // paired phone. This one has no other way to find out.
                alarm.value = null;
                stopSiren();
                break;

            case 'config_data':
                if (msg.config) config.value = msg.config;
                break;

            case 'location':
                if (msg.location) position.value = msg.location;
                break;

            case 'update_available':
                // Deliberately quiet: it marks the footer and nothing else. A
                // laptop that needs upgrading is not an emergency, and the panel
                // belongs to the sensors.
                if (msg.update) updateAvailable.value = msg.update;
                break;

            case 'pin_required':
                pinPrompt.value = true;
                break;

            case 'pong':
                link.value = 'live';
                break;

            default:
                break;
        }
    }

    function afterPairing() {
        send({ type: 'get_location' });

        // Guarded: this runs again on every reconnect, and an unguarded
        // setInterval here would stack up a new ping loop each time.
        if (pingTimer === null) {
            pingTimer = window.setInterval(() => send({ type: 'ping' }), PING_MS);
        }

        if ('Notification' in window && Notification.permission === 'default') {
            void Notification.requestPermission();
        }
        if ('serviceWorker' in navigator) {
            navigator.serviceWorker.register('/sw.js').catch((err: Error) => {
                // Browsers refuse to register a worker on an origin with a
                // certificate error, which is exactly what the laptop's
                // self-signed certificate produces. Swallowing this made a
                // notification path that never ran look like one that did.
                console.warn('LeaveSafe: notifications are unavailable —', err.message);
            });
        }
    }

    function pair(key: string, over: 'websocket' | 'bluetooth') {
        pairing.value = true;
        pairError.value = null;
        pendingKey = key;
        activeKey = key;
        // A new socket has proved nothing, including the one that replaces a
        // dropped connection. Reconnecting is exactly when a different machine
        // can answer at the same address, so the reconnect starts from the same
        // suspicion a first connection does.
        authSent = false;
        sessionLive = false;

        const handlers = {
            onMessage: handle,
            onOpen: () => {
                // With a fingerprint to check, the key waits for the server to
                // say which certificate it is serving. Sending it first and
                // checking afterwards would mean the check protects nothing.
                if (!expectedFingerprint) {
                    sendPendingKey();
                    return;
                }
                helloTimer = window.setTimeout(() => {
                    helloTimer = null;
                    // Measured against this connection, not against holding a
                    // token from an earlier one. A forged auth_ok used to set a
                    // token and so cancel this bail-out, which is the wrong way
                    // round: a server that never greets is precisely what this
                    // timer is here to give up on.
                    if (!sessionLive) {
                        pendingKey = null;
                        pairing.value = false;
                        pairError.value =
                            'The laptop did not identify itself, so the pairing key was not sent. ' +
                            'It may be running an older version — rescan the code from a laptop ' +
                            'running this one.';
                        closeTransport();
                    }
                }, HELLO_TIMEOUT_MS);
            },
            onClose: () => {
                clearHelloTimer();
                if (hasToken()) {
                    link.value = 'lost';
                    warnDisconnected();
                    window.setTimeout(() => pair(key, over), RECONNECT_MS);
                } else {
                    pairing.value = false;
                }
            },
            onError: (reason: string) => {
                clearHelloTimer();
                pendingKey = null;
                pairing.value = false;
                if (!hasToken()) pairError.value = reason;
            },
        };

        if (over === 'bluetooth') {
            connectBluetooth(handlers)
                .then((t) => {
                    // Register first, then announce. The other way round sends
                    // the pairing key before there is anything to send it over.
                    setTransport(t);
                    handlers.onOpen();
                })
                .catch((err: Error) => {
                    pairing.value = false;
                    pairError.value = err.message;
                });
            return;
        }

        closeTransport();
        setTransport(connectWebSocket(handlers));
    }

    if (screen.value === 'pair') {
        return <PairScreen onPair={pair} initialKey={autoKey} />;
    }

    return (
        <>
            <div class="shell">
                <div class="topbar">
                    <div class="wordmark">
                        Leave<span>Safe</span>
                    </div>
                    <button
                        type="button"
                        class="icon-btn"
                        aria-label="Check the connection"
                        onClick={() => {
                            link.value = 'checking';
                            send({ type: 'ping' });
                            send({ type: 'get_location' });
                            if (armed.value) captureAnchor();
                            window.setTimeout(() => {
                                if (link.value === 'checking') link.value = 'lost';
                            }, 3000);
                        }}
                    >
                        <RefreshIcon />
                    </button>
                    <button
                        type="button"
                        class="icon-btn"
                        aria-label="Settings"
                        onClick={() => {
                            settingsOpen.value = true;
                        }}
                    >
                        <GearIcon />
                    </button>
                </div>

                <StateHeader counting={counting} />
                <Annunciator />
                <Position />
                <Log />

                {updateAvailable.value ? (
                    <button
                        type="button"
                        class="foot readout foot-update"
                        aria-label={`Version ${updateAvailable.value.latest} is available — open update settings`}
                        onClick={() => {
                            settingsOpen.value = true;
                        }}
                    >
                        LeaveSafe {serverVersion.value ?? ''} <span class="foot-dot" aria-hidden="true" />{' '}
                        update available
                    </button>
                ) : (
                    <footer class="foot readout">LeaveSafe {serverVersion.value ?? ''} · no cloud</footer>
                )}
            </div>

            <ArmControl counting={counting} onCountdown={setCounting} />
            <SettingsSheet />
            <PinDialog />
            <AlarmOverlay />
            {toast.value && <div class="toast rise">{toast.value}</div>}
        </>
    );
}

function RefreshIcon() {
    return (
        <svg
            aria-hidden="true"
            width="15"
            height="15"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2.2"
            stroke-linecap="round"
            stroke-linejoin="round"
        >
            <path d="M21 12a9 9 0 1 1-2.64-6.36" />
            <path d="M21 3v6h-6" />
        </svg>
    );
}

function GearIcon() {
    return (
        <svg
            aria-hidden="true"
            width="15"
            height="15"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            stroke-linecap="round"
            stroke-linejoin="round"
        >
            <circle cx="12" cy="12" r="3" />
            <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.6a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" />
        </svg>
    );
}
