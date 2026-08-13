// @vitest-environment jsdom

import { h, render } from 'preact';
import { act } from 'preact/test-utils';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { App } from '../src/app';
import {
    alarm,
    armed,
    armedSince,
    config,
    link,
    log,
    pinPrompt,
    location as position,
    screen,
    sensors,
    toast,
    updateAvailable,
} from '../src/lib/store';

// Every kind of message the laptop sends, and what the phone does about it.
//
// The handler is the whole phone-side protocol: one message decides whether the
// panel is on screen, one decides whether the siren sounds, one decides whether
// the PIN dialog is raised. Each of those was reachable only by reading the
// switch and believing it, which is a poor way to hold a security boundary.
//
// These drive the real handler through a stand-in transport, the same way
// app.test.ts does — the decisions live inside it and nowhere else.

const stub = vi.hoisted(() => ({
    handlers: null as Record<string, (arg?: unknown) => void> | null,
    stopSiren: vi.fn(),
    startSiren: vi.fn(),
    closed: 0,
}));

vi.mock('../src/lib/transport', () => ({
    connectWebSocket: (handlers: Record<string, (arg?: unknown) => void>) => {
        stub.handlers = handlers;
        return {
            kind: 'websocket',
            send: () => {},
            close: () => {
                stub.closed++;
            },
            isOpen: () => true,
        };
    },
    connectBluetooth: vi.fn(),
    bluetoothSupported: () => false,
}));

// The return-visit path: a stored key, so the key goes out
// as soon as the socket opens. That is the shortest honest route to a connection
// the handler will listen to at all.
vi.mock('../src/lib/session', () => ({
    loadSession: () => ({ key: '1111111111111116' }),
    saveSession: vi.fn(),
    clearSession: vi.fn(),
}));

vi.mock('../src/lib/siren', () => ({
    startSiren: (message: string) => stub.startSiren(message),
    stopSiren: () => stub.stopSiren(),
    warnDisconnected: vi.fn(),
    primeSiren: vi.fn(),
}));

vi.mock('../src/lib/geo', () => ({ captureAnchor: vi.fn() }));

let host: HTMLDivElement;

beforeEach(() => {
    vi.stubGlobal('matchMedia', (query: string) => ({
        matches: false,
        media: query,
        addEventListener: () => {},
        removeEventListener: () => {},
    }));

    vi.useFakeTimers();
    stub.handlers = null;
    stub.closed = 0;
    stub.startSiren.mockClear();
    stub.stopSiren.mockClear();

    // The signals outlive a single mount, so a value left by an earlier test
    // would be indistinguishable from one this test produced.
    toast.value = null;
    alarm.value = null;
    armed.value = false;
    armedSince.value = null;
    sensors.value = [];
    config.value = null;
    position.value = null;
    updateAvailable.value = null;
    pinPrompt.value = false;
    link.value = 'live';
    // The alert history is persisted and read back on mount, so clearing the
    // signal alone would leave an earlier test's alerts to reappear.
    window.localStorage.clear();
    log.value = [];
    screen.value = 'pair';

    host = document.createElement('div');
    document.body.appendChild(host);
});

afterEach(() => {
    render(null, host);
    host.remove();
    vi.useRealTimers();
    vi.unstubAllGlobals();
});

/** Mounts the app and drives it to a connection the laptop has been accepted on. */
async function pairedApp(accept: Record<string, unknown> = {}) {
    await act(async () => {
        render(h(App, {}), host);
    });
    await vi.advanceTimersByTimeAsync(200);

    const handlers = stub.handlers;
    if (!handlers) throw new Error('the app never opened a transport');

    // Opening the socket sends the held key, which is what lets the acceptance
    // below be believed rather than ignored as unbidden.
    handlers.onOpen();
    handlers.onMessage({ type: 'auth_ok', token: 'a-token', sensors: [], ...accept });

    return handlers;
}

// A page thrown away by a locked phone comes back and asks again. Restarting the
// counter from zero would tell the owner the laptop had been armed for a few
// seconds when it had been armed for an hour, which is the number they read
// before deciding whether to walk back to it.
it('resumes the armed counter from the laptop clock', async () => {
    await pairedApp({ armed: true, armed_since: 1_700_000_000 });

    expect(armed.value).toBe(true);
    expect(armedSince.value).toBe(1_700_000_000_000);
});

// An older laptop reports that it is armed but not since when. Now is the only
// honest answer left, and it is better than leaving the readout blank.
it('falls back to now when the laptop reports no arming time', async () => {
    await pairedApp({ armed: true });

    expect(armed.value).toBe(true);
    expect(armedSince.value).toBe(Date.now());
});

it('clears the counter when the laptop reports itself unarmed', async () => {
    armedSince.value = 123;
    await pairedApp({ armed: false });

    expect(armed.value).toBe(false);
    expect(armedSince.value).toBeNull();
});

// A laptop too old to send the field at all must not be read as unarmed — that
// would put STANDBY on the screen of a phone watching an armed machine.
it('leaves the armed state alone when the laptop does not report one', async () => {
    armed.value = true;
    armedSince.value = 123;
    await pairedApp();

    expect(armed.value).toBe(true);
    expect(armedSince.value).toBe(123);
});

it('starts the counter when a status update says the laptop has just armed', async () => {
    const handlers = await pairedApp();

    handlers.onMessage({ type: 'status', armed: true });

    expect(armed.value).toBe(true);
    expect(armedSince.value).toBe(Date.now());
});

it('clears the counter when a status update says the laptop has disarmed', async () => {
    const handlers = await pairedApp({ armed: true, armed_since: 1_700_000_000 });

    handlers.onMessage({ type: 'status', armed: false });

    expect(armed.value).toBe(false);
    expect(armedSince.value).toBeNull();
});

// A status update carries the state of every sensor, and the panel has to fold
// it onto the list it already has rather than replace it — the display names and
// availability came with the acceptance and are not repeated here.
it('folds sensor states onto the list without losing what came with them', async () => {
    const handlers = await pairedApp({
        sensors: [
            { name: 'power', display_name: 'Power', available: true, enabled: true },
            { name: 'lid', display_name: 'Lid', available: true, enabled: true },
        ],
    });

    handlers.onMessage({
        type: 'status',
        sensor_states: {
            power: { enabled: false, status: 'ok' },
            // No entry for the lid: it must be left exactly as it was.
        },
    });

    expect(sensors.value).toEqual([
        {
            name: 'power',
            display_name: 'Power',
            available: true,
            enabled: false,
            status: 'ok',
            failure: undefined,
        },
        { name: 'lid', display_name: 'Lid', available: true, enabled: true },
    ]);
    expect(link.value).toBe('live');
});

// A sensor whose driver has failed is not watching, however it is configured.
// The panel counts it as covered without this.
it('carries a sensor failure through to the panel', async () => {
    const handlers = await pairedApp({
        sensors: [{ name: 'usb', display_name: 'USB', available: true, enabled: true }],
    });

    handlers.onMessage({
        type: 'status',
        sensor_states: { usb: { enabled: true, status: 'failed', failure: 'device gone' } },
    });

    expect(sensors.value[0].failure).toBe('device gone');
    expect(sensors.value[0].status).toBe('failed');
});

// An older laptop sends an acceptance with nothing attached to it. The panel
// still has to open: refusing would strand a phone whose laptop has simply not
// been upgraded, on the say-so of two optional fields.
it('opens the panel for a bare acceptance from an older laptop', async () => {
    await act(async () => {
        render(h(App, {}), host);
    });
    await vi.advanceTimersByTimeAsync(200);

    const handlers = stub.handlers;
    if (!handlers) throw new Error('the app never opened a transport');
    handlers.onOpen();
    handlers.onMessage({ type: 'auth_ok' });

    expect(screen.value).toBe('panel');
    expect(sensors.value).toEqual([]);
});

// The laptop stamps an alert with its own clock, and that is the time the log
// has to carry. The phone may have been asleep when it happened, so the moment
// the message arrived says when the phone woke up, not when the machine was
// touched — and the second is the only one worth reading afterwards.
it('logs an alert at the time the laptop stamped it', async () => {
    const handlers = await pairedApp();

    handlers.onMessage({
        type: 'alert',
        ts: 1_700_000_000,
        alert: { sensor: 'power', level: 'critical', message: 'Charger disconnected' },
    });

    expect(log.value[0].at).toBe(1_700_000_000_000);
});

// An older laptop sends no stamp, and the moment it arrived is the only honest
// answer left.
it('logs an unstamped alert at the time it arrived', async () => {
    const handlers = await pairedApp();

    handlers.onMessage({
        type: 'alert',
        alert: { sensor: 'power', level: 'critical', message: 'Charger disconnected' },
    });

    expect(log.value[0].at).toBe(Date.now());
});

it('ignores an alert message carrying no alert', async () => {
    const handlers = await pairedApp();

    handlers.onMessage({ type: 'alert' });

    expect(log.value).toEqual([]);
    expect(alarm.value).toBeNull();
    expect(stub.startSiren).not.toHaveBeenCalled();
});

// The phone reconnects to an alarm already sounding — it was locked, or it
// dropped the socket while the laptop was being carried off. The overlay has to
// come back up on its own.
it('raises the overlay for an alarm that was already sounding', async () => {
    const handlers = await pairedApp();

    handlers.onMessage({
        type: 'alarm_active',
        alert: { sensor: 'lid', level: 'critical', message: 'Lid opened' },
    });

    expect(alarm.value).toEqual({ message: 'Lid opened', sensor: 'lid' });
    expect(stub.startSiren).toHaveBeenCalledWith('Lid opened');
});

// An alarm with nothing written on it still has to say something. A blank
// full-screen overlay is the one version of this the user cannot act on.
it('gives a wordless alarm something to say', async () => {
    const handlers = await pairedApp();

    handlers.onMessage({ type: 'alarm_active', alert: { sensor: 'lid', level: 'critical', message: '' } });

    expect(alarm.value?.message).toBe('Something touched your laptop.');
});

it('ignores an alarm message carrying no alarm', async () => {
    const handlers = await pairedApp();

    handlers.onMessage({ type: 'alarm_active' });

    expect(alarm.value).toBeNull();
    expect(stub.startSiren).not.toHaveBeenCalled();
});

// Someone else called it off — the laptop's own terminal, or another paired
// phone. This one has no other way to find out, and a siren nobody can silence
// from here is worse than no siren at all.
it('takes the overlay down when the alarm is cleared elsewhere', async () => {
    const handlers = await pairedApp();
    handlers.onMessage({
        type: 'alarm_active',
        alert: { sensor: 'lid', level: 'critical', message: 'Lid opened' },
    });

    handlers.onMessage({ type: 'alarm_cleared' });

    expect(alarm.value).toBeNull();
    expect(stub.stopSiren).toHaveBeenCalled();
});

it('takes the settings the laptop sends', async () => {
    const handlers = await pairedApp();

    handlers.onMessage({ type: 'config_data', config: { port: 8443 } });

    expect(config.value).toEqual({ port: 8443 });
});

it('ignores a settings message carrying no settings', async () => {
    const handlers = await pairedApp();

    handlers.onMessage({ type: 'config_data' });

    expect(config.value).toBeNull();
});

it('takes the position the laptop reports', async () => {
    const handlers = await pairedApp();

    handlers.onMessage({
        type: 'location',
        location: { enabled: true, available: true, fix: { lat: 51.5, lon: -0.1, accuracy_m: 30 } },
    });

    expect(position.value?.fix?.lat).toBe(51.5);
});

// Deliberately quiet: a laptop that needs upgrading is not an emergency, and the
// panel belongs to the sensors.
it('marks an available update without interrupting anything', async () => {
    const handlers = await pairedApp();

    handlers.onMessage({
        type: 'update_available',
        update: { running: '1.0.0', latest: '1.1.0', url: 'https://example.invalid', channel: 'stable' },
    });

    expect(updateAvailable.value?.latest).toBe('1.1.0');
    expect(alarm.value).toBeNull();
    expect(toast.value).toBeNull();
});

it('raises the PIN dialog when the laptop asks for one', async () => {
    const handlers = await pairedApp();

    handlers.onMessage({ type: 'pin_required' });

    expect(pinPrompt.value).toBe(true);
});

it('counts a pong as the connection being live', async () => {
    const handlers = await pairedApp();
    link.value = 'checking';

    handlers.onMessage({ type: 'pong' });

    expect(link.value).toBe('live');
});

// A laptop running a newer build can send something this phone has no branch
// for. Ignoring it is the only safe answer; the alternative is a phone that
// breaks whenever the laptop learns a new word.
it('ignores a message it has no branch for', async () => {
    const handlers = await pairedApp();

    handlers.onMessage({ type: 'something-this-build-never-heard-of' });

    expect(alarm.value).toBeNull();
    expect(pinPrompt.value).toBe(false);
    expect(toast.value).toBeNull();
});

// A refusal arriving on a connection that already holds a session is a refusal
// of something else — a PIN, a setting — not of the pairing. Throwing the stored
// key away here would unpair a phone from a laptop that is working fine.
it('answers a refusal on a live session with a toast, not by unpairing', async () => {
    const handlers = await pairedApp();

    handlers.onMessage({ type: 'auth_fail', reason: 'Wrong PIN' });

    expect(toast.value).toBe('Wrong PIN');
});

it('says something even when a refusal on a live session gives no reason', async () => {
    const handlers = await pairedApp();

    handlers.onMessage({ type: 'auth_fail' });

    expect(toast.value).toBe('Refused');
});

// Nothing but the handshake is acted on before the laptop has been accepted.
// Without this guard, anything that could answer the phone's socket could raise
// the PIN dialog and collect the code that guards disarming — no pairing key
// needed, and nothing on screen to suggest it had happened.
it('ignores everything from a connection that has proved nothing', async () => {
    await act(async () => {
        render(h(App, {}), host);
    });
    await vi.advanceTimersByTimeAsync(200);

    const handlers = stub.handlers;
    if (!handlers) throw new Error('the app never opened a transport');

    handlers.onMessage({ type: 'pin_required' });
    handlers.onMessage({
        type: 'alarm_active',
        alert: { sensor: 'lid', level: 'critical', message: 'Lid opened' },
    });
    handlers.onMessage({ type: 'status', armed: true });

    expect(pinPrompt.value).toBe(false);
    expect(alarm.value).toBeNull();
    expect(stub.startSiren).not.toHaveBeenCalled();
    expect(armed.value).toBe(false);
});
