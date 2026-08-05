// @vitest-environment jsdom

import { h, render } from 'preact';
import { act } from 'preact/test-utils';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { App } from '../src/app';
import { fingerprintVerdict, pairError, serverFingerprint } from '../src/lib/store';

// The greeting, and what the phone does with the certificate in it.
//
// This is the one check standing between a scanned QR code and the pairing key
// leaving the phone. The key waits for the greeting, the greeting says which
// certificate the server is serving, and the key goes out only if that is the
// certificate the code was printed for. Getting it wrong in the safe direction
// strands the user; getting it wrong in the other hands the key — and with it
// the laptop's alarm — to whatever answered the socket.

const FINGERPRINT = 'aa'.repeat(32);
const OTHER = 'bb'.repeat(32);

const stub = vi.hoisted(() => ({
    handlers: null as Record<string, (arg?: unknown) => void> | null,
    sent: [] as Record<string, unknown>[],
    closed: 0,
}));

vi.mock('../src/lib/transport', () => ({
    connectWebSocket: (handlers: Record<string, (arg?: unknown) => void>) => {
        stub.handlers = handlers;
        return {
            kind: 'websocket',
            send: (msg: Record<string, unknown>) => {
                stub.sent.push(msg);
            },
            close: () => {
                stub.closed++;
            },
            isOpen: () => true,
        };
    },
    connectBluetooth: vi.fn(),
    bluetoothSupported: () => false,
}));

// A stored session that names a certificate. This is the return-visit path with
// the check switched on: the key is held until the greeting arrives.
vi.mock('../src/lib/session', () => ({
    loadSession: () => ({ key: '1111111111111116', fingerprint: FINGERPRINT }),
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

    vi.useFakeTimers();
    stub.handlers = null;
    stub.sent = [];
    stub.closed = 0;
    pairError.value = null;
    serverFingerprint.value = null;
    fingerprintVerdict.value = 'unverified';

    host = document.createElement('div');
    document.body.appendChild(host);
});

afterEach(() => {
    render(null, host);
    host.remove();
    vi.useRealTimers();
    vi.unstubAllGlobals();
});

/** Mounts the app, lets it open a socket, and hands the socket back. */
async function connectedApp() {
    await act(async () => {
        render(h(App, {}), host);
    });
    await vi.advanceTimersByTimeAsync(200);

    const handlers = stub.handlers;
    if (!handlers) throw new Error('the app never opened a transport');
    handlers.onOpen();

    // With a fingerprint to check, opening the socket must not be enough for the
    // key to go out. Sending it first and checking afterwards would mean the
    // check protects nothing.
    expect(stub.sent).toEqual([]);

    // Pairing closes whatever transport was registered before it, which here is
    // the one an earlier test left behind. Counting from zero now makes the
    // assertions below about what the greeting caused, and nothing else.
    stub.closed = 0;

    return handlers;
}

it('sends the held key once the certificate matches the one the code named', async () => {
    const handlers = await connectedApp();

    handlers.onMessage({ type: 'hello', cert_fp: FINGERPRINT });

    expect(fingerprintVerdict.value).toBe('match');
    expect(serverFingerprint.value).toBe(FINGERPRINT);
    expect(stub.sent).toEqual([{ type: 'auth', key: '1111111111111116' }]);
    expect(pairError.value).toBeNull();
});

// Whatever is on the other end of this socket, it is not the laptop the code was
// printed for — so it does not get the key, and the connection is dropped rather
// than left open for a second try at the same trick.
it('holds the key back and says why when the certificate is a different one', async () => {
    const handlers = await connectedApp();

    handlers.onMessage({ type: 'hello', cert_fp: OTHER });

    expect(fingerprintVerdict.value).toBe('mismatch');
    expect(stub.sent).toEqual([]);
    expect(pairError.value).toContain('not the laptop your code came from');
    expect(stub.closed).toBe(1);
});

// A greeting with no certificate in it is a mismatch, not an absence: the code
// promised one. Reading it as "nothing to check" would let a plain-HTTP
// impostor turn the check off simply by staying quiet about it.
it('treats a greeting with no certificate as a mismatch', async () => {
    const handlers = await connectedApp();

    handlers.onMessage({ type: 'hello' });

    expect(fingerprintVerdict.value).toBe('mismatch');
    expect(serverFingerprint.value).toBeNull();
    expect(stub.sent).toEqual([]);
    expect(stub.closed).toBe(1);
});

// The key is held back until the greeting arrives, so a server that never sends
// one leaves the phone waiting rather than talking. That is the safe way round,
// but it has to end in a message instead of a spinner.
it('gives up on a laptop that never identifies itself', async () => {
    await connectedApp();

    await vi.advanceTimersByTimeAsync(6000);

    expect(stub.sent).toEqual([]);
    expect(pairError.value).toContain('did not identify itself');
    expect(stub.closed).toBe(1);
});
