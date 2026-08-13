// @vitest-environment jsdom

import { h, render } from 'preact';
import { act } from 'preact/test-utils';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { App } from '../src/app';

// What the phone arranges for itself the moment a laptop accepts it: the
// heartbeat, and the two things that decide whether an alert can reach a locked
// screen at all. Both of the latter are asked of the browser and can be
// refused, and a refusal that goes unreported is a notification path that looks
// like it works and does not.

const stub = vi.hoisted(() => ({
    handlers: null as Record<string, (arg?: unknown) => void> | null,
    sent: [] as Array<Record<string, unknown>>,
    requestPermission: vi.fn(() => Promise.resolve('granted')),
    register: vi.fn(() => Promise.resolve()),
}));

vi.mock('../src/lib/transport', () => ({
    connectWebSocket: (handlers: Record<string, (arg?: unknown) => void>) => {
        stub.handlers = handlers;
        return {
            kind: 'websocket',
            send: (msg: Record<string, unknown>) => stub.sent.push(msg),
            close: () => {},
            isOpen: () => true,
        };
    },
    connectBluetooth: vi.fn(),
    bluetoothSupported: () => false,
}));

vi.mock('../src/lib/session', () => ({
    loadSession: () => ({ key: '1111111111111116' }),
    saveSession: vi.fn(),
    clearSession: vi.fn(),
}));

vi.mock('../src/lib/siren', () => ({
    startSiren: vi.fn(),
    stopSiren: vi.fn(),
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

    // jsdom has neither of these, and the app asks whether they exist before it
    // uses them — so without standing them up, both paths are skipped and the
    // one that reports a refusal never runs.
    vi.stubGlobal('Notification', {
        permission: 'default',
        requestPermission: stub.requestPermission,
    });
    Object.defineProperty(navigator, 'serviceWorker', {
        value: { register: stub.register },
        configurable: true,
    });

    vi.useFakeTimers();
    stub.handlers = null;
    stub.sent = [];
    stub.requestPermission.mockClear();
    stub.register.mockClear();
    host = document.createElement('div');
    document.body.appendChild(host);
});

afterEach(() => {
    render(null, host);
    host.remove();
    Reflect.deleteProperty(navigator, 'serviceWorker');
    vi.useRealTimers();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
});

/** Mounts the app and drives it to the point where a laptop has accepted it. */
async function paired() {
    await act(async () => {
        render(h(App, {}), host);
    });
    await vi.advanceTimersByTimeAsync(200);

    const handlers = stub.handlers;
    if (!handlers) throw new Error('the app never opened a transport');
    handlers.onOpen();
    handlers.onMessage({ type: 'auth_ok', token: 'a-token', sensors: [] });
    return handlers;
}

it('asks for everything an alert needs to reach a locked screen', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    stub.register.mockImplementation(() => Promise.reject(new Error('SSL certificate error')));

    const handlers = await paired();

    // Where the phone is matters as much as that something happened, so it is
    // asked for straight away rather than when the alarm goes off.
    expect(stub.sent.some((msg) => msg.type === 'get_location')).toBe(true);
    expect(stub.requestPermission).toHaveBeenCalledTimes(1);
    expect(stub.register).toHaveBeenCalledWith('/sw.js');

    // Browsers refuse to register a worker on an origin with a certificate
    // error, which is exactly what the laptop's self-signed certificate
    // produces. Swallowed, that left a notification path that never ran looking
    // like one that did.
    await vi.advanceTimersByTimeAsync(0);
    expect(warn.mock.calls[0]?.join(' ')).toContain('notifications are unavailable');

    // The heartbeat keeps the connection honest, and it only starts here.
    stub.sent.length = 0;
    await vi.advanceTimersByTimeAsync(15000);
    expect(stub.sent.some((msg) => msg.type === 'ping')).toBe(true);

    // Reconnecting runs all of this again, and an unguarded interval would lay
    // a second ping loop over the first every time the phone woke up.
    handlers.onMessage({ type: 'auth_ok', token: 'a-token', sensors: [] });
    stub.sent.length = 0;
    await vi.advanceTimersByTimeAsync(15000);
    expect(stub.sent.filter((msg) => msg.type === 'ping')).toHaveLength(1);
});
