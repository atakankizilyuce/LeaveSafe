// @vitest-environment jsdom

import { h, render } from 'preact';
import { act } from 'preact/test-utils';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { App } from '../src/app';
import { pairError, pairing, screen, setToken } from '../src/lib/store';

// Pairing over Bluetooth instead of Wi-Fi.
//
// It is the answer to a laptop and a phone that cannot see each other on the
// network — a guest network that isolates its clients, a hotel, a conference.
// The order of the two steps is the part worth pinning: the transport has to be
// registered before the key is announced, or the key is sent before there is
// anything to send it over and the pairing simply never happens.

const stub = vi.hoisted(() => ({
    connectBluetooth: vi.fn(),
    sent: [] as Record<string, unknown>[],
    websockets: 0,
}));

vi.mock('../src/lib/transport', () => ({
    connectWebSocket: () => {
        stub.websockets++;
        return { kind: 'websocket', send: () => {}, close: () => {}, isOpen: () => true };
    },
    connectBluetooth: (handlers: Record<string, (arg?: unknown) => void>) => stub.connectBluetooth(handlers),
    bluetoothSupported: () => true,
}));

// No stored session, so the app stays on the pairing screen and waits to be
// typed into — which is the only way to reach the Bluetooth button at all.
vi.mock('../src/lib/session', () => ({
    loadSession: () => null,
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
    stub.connectBluetooth.mockReset();
    stub.sent = [];
    stub.websockets = 0;

    setToken(null);
    pairError.value = null;
    pairing.value = false;
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

/** A stand-in for a paired Bluetooth link, recording what is sent over it. */
function bluetoothTransport() {
    return {
        kind: 'bluetooth',
        send: (msg: Record<string, unknown>) => {
            stub.sent.push(msg);
        },
        close: () => {},
        isOpen: () => true,
    };
}

/** Types a key on the pairing screen and asks for it to go over Bluetooth. */
async function pairOverBluetooth(key = '1111-1111-1111-1116') {
    await act(async () => {
        render(h(App, {}), host);
    });

    const field = host.querySelector<HTMLInputElement>('#key');
    if (!field) throw new Error('the pairing screen has no key field');
    await act(async () => {
        field.value = key;
        field.dispatchEvent(new Event('input', { bubbles: true }));
    });

    const button = host.querySelector<HTMLButtonElement>('.pair-alt');
    if (!button) throw new Error('the pairing screen offers no Bluetooth button');
    await act(async () => {
        button.click();
    });
}

it('sends the key only once the Bluetooth link is registered', async () => {
    stub.connectBluetooth.mockImplementation(() => Promise.resolve(bluetoothTransport()));

    await pairOverBluetooth();
    await vi.advanceTimersByTimeAsync(0);

    expect(stub.connectBluetooth).toHaveBeenCalledTimes(1);
    // Registered first, then announced. The other way round sends the pairing
    // key before there is anything to send it over.
    expect(stub.sent).toEqual([{ type: 'auth', key: '1111111111111116' }]);
    // And nothing went over the network: the whole point of this path is a
    // network the two machines cannot reach each other on.
    expect(stub.websockets).toBe(0);
});

// A phone whose Bluetooth is switched off, or an owner who dismissed the
// browser's device chooser. Either way the reason belongs on screen: the button
// silently doing nothing is the version of this that gets retried forever.
it('puts a refused Bluetooth connection on screen', async () => {
    stub.connectBluetooth.mockImplementation(() =>
        Promise.reject(new Error('Bluetooth is switched off on this phone.')),
    );

    await pairOverBluetooth();
    await vi.advanceTimersByTimeAsync(0);

    expect(pairing.value).toBe(false);
    expect(pairError.value).toBe('Bluetooth is switched off on this phone.');
    expect(stub.sent).toEqual([]);
});
