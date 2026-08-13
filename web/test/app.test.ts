// @vitest-environment jsdom

import { h, render } from 'preact';
import { act } from 'preact/test-utils';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { App } from '../src/app';
import { alarm, toast } from '../src/lib/store';

// The laptop says things about itself on the same channel it reports intrusions
// on, under the reserved sensor name "system": a setting that needs a restart, a
// geolocation endpoint it refused, a sensor change it would not make while
// armed. Those are notices. They were raising the full-screen alert instead —
// siren, vibration, lock-screen notification — so saving the settings sheet made
// the phone scream in the user's pocket while they sat next to the laptop.
//
// These tests drive the real message handler through a stand-in transport,
// because the decision lives inside it and nowhere else.

const stub = vi.hoisted(() => ({
    handlers: null as Record<string, (arg?: unknown) => void> | null,
    startSiren: vi.fn(),
}));

vi.mock('../src/lib/transport', () => ({
    connectWebSocket: (handlers: Record<string, (arg?: unknown) => void>) => {
        stub.handlers = handlers;
        return {
            kind: 'websocket',
            send: () => {},
            close: () => {},
            isOpen: () => true,
        };
    },
    connectBluetooth: vi.fn(),
    bluetoothSupported: () => false,
}));

// A stored session is the return-visit path: it is what makes the app pair
// itself on mount rather than waiting for a QR code. The stored key is left
// empty so the key goes out as soon as the socket opens, which is the shortest
// honest route to an authenticated connection.
vi.mock('../src/lib/session', () => ({
    loadSession: () => ({ key: '1111111111111116' }),
    saveSession: vi.fn(),
    clearSession: vi.fn(),
}));

vi.mock('../src/lib/siren', () => ({
    startSiren: (message: string) => stub.startSiren(message),
    stopSiren: vi.fn(),
    warnDisconnected: vi.fn(),
    primeSiren: vi.fn(),
}));

vi.mock('../src/lib/geo', () => ({
    captureAnchor: vi.fn(),
}));

let host: HTMLDivElement;

beforeEach(() => {
    // jsdom implements no media queries, and the activity trace asks whether
    // the user has requested reduced motion before it draws. Answering "no"
    // keeps the question from throwing inside an unrelated render.
    vi.stubGlobal('matchMedia', (query: string) => ({
        matches: false,
        media: query,
        addEventListener: () => {},
        removeEventListener: () => {},
    }));

    vi.useFakeTimers();
    stub.handlers = null;
    stub.startSiren.mockClear();
    // The signals outlive a single mount, so a value left by the previous test
    // would be indistinguishable from one this test produced.
    toast.value = null;
    alarm.value = null;
    host = document.createElement('div');
    document.body.appendChild(host);
});

afterEach(() => {
    render(null, host);
    host.remove();
    vi.useRealTimers();
    vi.unstubAllGlobals();
});

// pairedApp mounts the app, lets it pick its stored session back up, and drives
// it to the point where the laptop has been accepted — which is the only state
// in which an alert is acted on at all.
async function pairedApp() {
    // act flushes the mount effect. Without it the effect that picks the stored
    // session back up is still queued when the timers below are advanced, and
    // the app has not yet asked for a socket.
    await act(async () => {
        render(h(App, {}), host);
    });
    await vi.advanceTimersByTimeAsync(200);

    const handlers = stub.handlers;
    if (!handlers) throw new Error('the app never opened a transport');
    // Opening the socket sends the held key, which is what lets the acceptance
    // below be believed rather than ignored as unbidden.
    handlers.onOpen();
    handlers.onMessage({ type: 'auth_ok', token: 'a-token', sensors: [] });

    return handlers;
}

// Both alerts arrive over one connection, in the order a user would meet them:
// save a setting, then have the charger pulled. Contrasting them on the same
// socket is the point — the bug was that the first was treated as the second.
it('answers a laptop notice with a toast and a real sensor alert with the alarm', async () => {
    const handlers = await pairedApp();

    handlers.onMessage({
        type: 'alert',
        alert: { sensor: 'system', level: 'warning', message: 'Restart required' },
    });

    expect(toast.value).toBe('Restart required');
    expect(stub.startSiren).not.toHaveBeenCalled();
    // The full-screen overlay offers "pause this sensor" and "stop using this
    // sensor", and there is no sensor called system to do either to — so it
    // must not be raised at all.
    expect(alarm.value).toBeNull();

    handlers.onMessage({
        type: 'alert',
        alert: { sensor: 'power', level: 'critical', message: 'Charger disconnected' },
    });

    expect(stub.startSiren).toHaveBeenCalledWith('Charger disconnected');
    expect(alarm.value).toEqual({
        message: 'Charger disconnected',
        sensor: 'power',
    });
});
