// @vitest-environment jsdom

import { h, render } from 'preact';
import { act } from 'preact/test-utils';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { App } from '../src/app';
import { pairError } from '../src/lib/store';

// What the phone says when a pairing key is refused. The count of attempts left
// is the only thing telling the user whether the next try is worth making, and
// it is the difference between a typo and a laptop that has rotated its key.

const stub = vi.hoisted(() => ({
    handlers: null as Record<string, (arg?: unknown) => void> | null,
}));

vi.mock('../src/lib/transport', () => ({
    connectWebSocket: (handlers: Record<string, (arg?: unknown) => void>) => {
        stub.handlers = handlers;
        return { kind: 'websocket', send: () => {}, close: () => {}, isOpen: () => true };
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
    pairError.value = null;
    host = document.createElement('div');
    document.body.appendChild(host);
});

afterEach(() => {
    render(null, host);
    host.remove();
    vi.useRealTimers();
    vi.unstubAllGlobals();
});

/** Refuses the key and hands back what the phone put on screen because of it. */
function refuse(alert: Record<string, unknown>): string | null {
    stub.handlers?.onMessage({ type: 'auth_fail', ...alert });
    return pairError.value;
}

// One mount, then every refusal in turn. The app is mounted exactly once in
// production, and each of these is just another message arriving on the socket
// it already has.
it('says how many tries are left when a key is refused', async () => {
    await act(async () => {
        render(h(App, {}), host);
    });
    await vi.advanceTimersByTimeAsync(200);

    const handlers = stub.handlers;
    if (!handlers) throw new Error('the app never opened a transport');

    // A refusal is only a refusal of something asked. Before the key has gone
    // out there is nothing to refuse, and acting on this one would throw the
    // stored pairing away on the say-so of whatever answered the socket.
    expect(refuse({ reason: 'Refused', remaining_attempts: 2 })).toBeNull();

    handlers.onOpen();

    expect(refuse({ reason: 'That key is wrong.', remaining_attempts: 3 })).toBe(
        'That key is wrong. 3 attempts left.',
    );

    // The last attempt is written in the singular, because "1 attempts left" is
    // the sort of thing that makes a user doubt the rest of the sentence.
    expect(refuse({ reason: 'That key is wrong.', remaining_attempts: 1 })).toBe(
        'That key is wrong. 1 attempt left.',
    );

    // An older laptop sends no count. Saying nothing about attempts is better
    // than guessing at a number the user would then act on.
    expect(refuse({ reason: 'That key is wrong.' })).toBe('That key is wrong.');

    // No count and no reason either: the phone still has to say something, and
    // what it knows for certain is that the key did not open the laptop.
    expect(refuse({})).toBe('That key was refused.');
});
