// @vitest-environment jsdom

import { h, render } from 'preact';
import { act } from 'preact/test-utils';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { App } from '../src/app';
import { link, pairError, pairing, pinPrompt, screen, setToken } from '../src/lib/store';

// What the phone does when the socket goes away.
//
// This is the ordinary case, not an edge one: a phone locks its screen, walks
// out of Wi-Fi range, or the laptop is picked up and carried off. The reconnect
// is what makes the panel come back by itself — and it is also the moment
// something else can answer at the same address, which is why every attempt
// starts from the same suspicion a first connection does.

const stub = vi.hoisted(() => ({
    handlers: null as Record<string, (arg?: unknown) => void> | null,
    connections: 0,
    closed: 0,
    warned: 0,
}));

vi.mock('../src/lib/transport', () => ({
    connectWebSocket: (handlers: Record<string, (arg?: unknown) => void>) => {
        stub.handlers = handlers;
        stub.connections++;
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

vi.mock('../src/lib/session', () => ({
    loadSession: () => ({ key: '1111111111111116', fingerprint: '' }),
    saveSession: vi.fn(),
    clearSession: vi.fn(),
}));

vi.mock('../src/lib/siren', () => ({
    startSiren: vi.fn(),
    stopSiren: vi.fn(),
    warnDisconnected: () => {
        stub.warned++;
    },
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
    stub.connections = 0;
    stub.closed = 0;
    stub.warned = 0;

    // The token outlives a mount, and whether one is held is the whole of what
    // these paths branch on — so it is set explicitly rather than inherited.
    setToken(null);
    pairError.value = null;
    pairing.value = false;
    pinPrompt.value = false;
    link.value = 'live';
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

/** Mounts the app and hands back the socket it opened. */
async function connectedApp() {
    await act(async () => {
        render(h(App, {}), host);
    });
    await vi.advanceTimersByTimeAsync(200);

    const handlers = stub.handlers;
    if (!handlers) throw new Error('the app never opened a transport');
    return handlers;
}

/** Drives the app to a connection the laptop has accepted. */
async function pairedApp() {
    const handlers = await connectedApp();
    handlers.onOpen();
    handlers.onMessage({ type: 'auth_ok', token: 'a-token', sensors: [] });
    return handlers;
}

// A paired phone tries again, because the laptop it is watching is still out
// there and the panel has to come back on its own. Three seconds is short
// enough that a walk between rooms is not a re-pair.
it('reconnects a paired session and says the link is lost meanwhile', async () => {
    await pairedApp();
    const before = stub.connections;

    stub.handlers?.onClose();

    expect(link.value).toBe('lost');
    expect(stub.warned).toBe(1);

    await vi.advanceTimersByTimeAsync(3000);

    expect(stub.connections).toBe(before + 1);
});

// A dialog the laptop asked for must not outlive the laptop. Left standing
// across a reconnect, the digits typed into it afterwards were submitted on a
// socket that had proved nothing — so the PIN guarding the alarm went to
// whatever had answered. It also stops the user typing into something whose
// Disarm button has quietly stopped working.
it('closes the PIN dialog when the socket it belongs to goes away', async () => {
    await pairedApp();
    pinPrompt.value = true;

    stub.handlers?.onClose();

    expect(pinPrompt.value).toBe(false);
});

// The reconnect starts from the same suspicion a first connection does, so an
// acceptance that arrives before this attempt has offered anything is still
// ignored. Reconnecting is exactly when a different machine can be answering at
// the same address.
it('trusts nothing on the socket that replaces a dropped one', async () => {
    await pairedApp();

    stub.handlers?.onClose();
    await vi.advanceTimersByTimeAsync(3000);

    screen.value = 'pair';
    stub.handlers?.onMessage({ type: 'auth_ok', token: 'someone-elses' });

    expect(screen.value).toBe('pair');
});

// Nothing has been paired yet, so there is nothing to come back to: the socket
// dropping means the attempt failed, and the screen has to stop saying
// "Connecting…" and let the user try again.
it('gives up rather than retrying when nothing was ever paired', async () => {
    await connectedApp();
    const before = stub.connections;

    stub.handlers?.onClose();

    expect(pairing.value).toBe(false);
    await vi.advanceTimersByTimeAsync(5000);
    expect(stub.connections).toBe(before);
});

it('puts a failed first connection on screen', async () => {
    await connectedApp();

    stub.handlers?.onError('The laptop is not answering on that address.');

    expect(pairing.value).toBe(false);
    expect(pairError.value).toBe('The laptop is not answering on that address.');
});

// A paired phone that loses its connection is not looking at the pairing screen,
// so an error written there is a message nobody reads — and it would still be
// sitting on the screen the next time somebody does pair. The reconnect is what
// answers this case.
it('keeps a transport error off the pairing screen of a paired phone', async () => {
    await pairedApp();

    stub.handlers?.onError('connection reset');

    expect(pairError.value).toBeNull();
});
