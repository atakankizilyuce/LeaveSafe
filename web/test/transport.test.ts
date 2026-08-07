// @vitest-environment jsdom

import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { bluetoothSupported, connectBluetooth, connectWebSocket } from '../src/lib/transport';

// This module is the phone's link to the laptop, and until now every test in
// this directory replaced it with a stand-in — so the thing all of them were
// pretending to be was the one thing nothing checked.
//
// Two of the checks below are about what the link is rather than whether it
// works. The scheme has to follow the page's: a phone loaded over https that
// opened a plain ws:// would be sending the pairing key across the network in
// the clear, and it would look identical on screen to one that did not.

interface Handlers {
    onMessage: ReturnType<typeof vi.fn>;
    onOpen: ReturnType<typeof vi.fn>;
    onClose: ReturnType<typeof vi.fn>;
    onError: ReturnType<typeof vi.fn>;
}

/** Fresh spies for the four things a transport can tell its caller. */
function handlers(): Handlers {
    return { onMessage: vi.fn(), onOpen: vi.fn(), onClose: vi.fn(), onError: vi.fn() };
}

/**
 * Just enough WebSocket for the module to drive, and to record what it was
 * driven with. jsdom has a real one, but it would try to reach the network.
 */
class FakeSocket {
    static CONNECTING = 0;
    static OPEN = 1;
    static CLOSED = 3;

    static last: FakeSocket | null = null;

    readyState = FakeSocket.CONNECTING;
    sent: string[] = [];
    closed = 0;

    onopen: (() => void) | null = null;
    onmessage: ((e: { data: unknown }) => void) | null = null;
    onclose: (() => void) | null = null;
    onerror: (() => void) | null = null;

    constructor(readonly url: string) {
        FakeSocket.last = this;
    }

    send(data: string) {
        this.sent.push(data);
    }

    close() {
        this.closed += 1;
        this.readyState = FakeSocket.CLOSED;
    }

    /** Puts the socket in the state a completed handshake leaves it in. */
    open() {
        this.readyState = FakeSocket.OPEN;
        this.onopen?.();
    }

    /**
     * What a link going away looks like from here: the browser marks the socket
     * closed first and only then reports it, which is the order that decides
     * what any timer still pending makes of the state it finds.
     */
    drop() {
        this.readyState = FakeSocket.CLOSED;
        this.onclose?.();
    }
}

/** Points the page at a scheme and host, the way a real load would. */
function pageAt(protocol: string, host: string) {
    vi.stubGlobal('location', { protocol, host });
}

beforeEach(() => {
    vi.useFakeTimers();
    FakeSocket.last = null;
    vi.stubGlobal('WebSocket', FakeSocket);
    pageAt('http:', 'laptop.local:9443');
});

afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
});

// The scheme is not cosmetic. A page served over https that opened ws:// would
// carry the pairing key in the clear, and nothing on screen would say so.
it('opens a secure socket when the page itself is secure', () => {
    pageAt('https:', 'laptop.local:9443');

    connectWebSocket(handlers());

    expect(FakeSocket.last?.url).toBe('wss://laptop.local:9443/ws');
});

it('opens a plain socket when the page is plain', () => {
    connectWebSocket(handlers());

    expect(FakeSocket.last?.url).toBe('ws://laptop.local:9443/ws');
});

// A browser that refuses to construct the socket at all — a blocked mixed-content
// load, a disallowed scheme — must not take the caller down with it.
it('reports a socket it could not even open, and hands back something inert', () => {
    vi.stubGlobal('WebSocket', function Blocked() {
        throw new Error('blocked');
    });
    const h = handlers();

    const transport = connectWebSocket(h);

    expect(h.onError).toHaveBeenCalledTimes(1);
    expect(transport.isOpen()).toBe(false);
    // Inert rather than broken: the caller may still call these.
    expect(() => transport.send({ type: 'ping' } as never)).not.toThrow();
    expect(() => transport.close()).not.toThrow();
});

it('passes a message through once the socket is open', () => {
    const h = handlers();
    connectWebSocket(h);

    FakeSocket.last?.onmessage?.({ data: '{"type":"state","armed":true}' });

    expect(h.onMessage).toHaveBeenCalledWith({ type: 'state', armed: true });
});

// The laptop is the only thing that should be on this socket, but a truncated
// frame is enough to produce text that is not JSON. Throwing out of the handler
// would tear down the link over a single bad line.
it('ignores a frame that is not JSON rather than failing on it', () => {
    const h = handlers();
    connectWebSocket(h);

    expect(() => FakeSocket.last?.onmessage?.({ data: 'not json at all' })).not.toThrow();
    expect(h.onMessage).not.toHaveBeenCalled();
});

it('tells the caller when the socket opens and when it closes', () => {
    const h = handlers();
    connectWebSocket(h);

    FakeSocket.last?.open();
    expect(h.onOpen).toHaveBeenCalledTimes(1);

    FakeSocket.last?.drop();
    expect(h.onClose).toHaveBeenCalledTimes(1);
});

it('closes the socket when the caller lets the transport go', () => {
    const transport = connectWebSocket(handlers());
    FakeSocket.last?.open();

    transport.close();

    expect(FakeSocket.last?.closed).toBe(1);
    expect(transport.isOpen()).toBe(false);
});

it('sends as JSON only while the socket is open', () => {
    const transport = connectWebSocket(handlers());

    // Still connecting: the message has nowhere to go, and queueing it would be
    // a promise this layer cannot keep.
    transport.send({ type: 'arm' } as never);
    expect(FakeSocket.last?.sent).toEqual([]);

    FakeSocket.last?.open();
    transport.send({ type: 'arm' } as never);
    expect(FakeSocket.last?.sent).toEqual(['{"type":"arm"}']);
    expect(transport.isOpen()).toBe(true);
});

// The timeout is what turns "wrong network" from a spinner into a sentence.
it('gives up on a socket that never opens, and says why', () => {
    const h = handlers();
    connectWebSocket(h);

    vi.advanceTimersByTime(8000);

    expect(FakeSocket.last?.closed).toBe(1);
    expect(h.onError).toHaveBeenCalledTimes(1);
    expect(String(h.onError.mock.calls[0][0])).toContain('same network');
});

// A socket that opened and was later closed by the laptop must not have the
// connect timeout fire on top of it: the user would be told the network is
// wrong when it never was.
it('does not report a timeout on a socket that did open', () => {
    const h = handlers();
    connectWebSocket(h);
    FakeSocket.last?.open();

    vi.advanceTimersByTime(60000);

    expect(h.onError).not.toHaveBeenCalled();
});

// A link that worked and then dropped is a different story from one that never
// reached the laptop, and the phone must not tell the second story about the
// first: "check that you are on the same network" sends someone hunting a
// network fault when the laptop simply went to sleep.
it('reports a dropped connection as a drop, not as a wrong network', () => {
    const h = handlers();
    connectWebSocket(h);
    FakeSocket.last?.open();

    FakeSocket.last?.drop();
    vi.advanceTimersByTime(60000);

    expect(h.onClose).toHaveBeenCalledTimes(1);
    expect(h.onError).not.toHaveBeenCalled();
});

it('reports a socket error', () => {
    const h = handlers();
    connectWebSocket(h);

    FakeSocket.last?.onerror?.();

    expect(h.onError).toHaveBeenCalledTimes(1);
});

// ── Bluetooth ────────────────────────────────────────────────────────────────

it('knows whether this browser offers Bluetooth at all', () => {
    expect(bluetoothSupported()).toBe(false);

    vi.stubGlobal('navigator', { bluetooth: {} });
    expect(bluetoothSupported()).toBe(true);
});

it('refuses Bluetooth on a browser that has none, with a name of one that does', async () => {
    await expect(connectBluetooth(handlers())).rejects.toThrow(/Chrome and Edge/);
});

/**
 * A stand-in for the Web Bluetooth stack, wired the way a real pairing leaves
 * it: a device with a GATT server, one service, and the two characteristics the
 * laptop advertises.
 */
function fakeBluetooth() {
    const listeners: Record<string, (e: unknown) => void> = {};
    const written: string[] = [];
    let writeFails = false;

    const tx = {
        startNotifications: vi.fn().mockResolvedValue(undefined),
        addEventListener: (type: string, fn: (e: unknown) => void) => {
            listeners[type] = fn;
        },
    };
    const rx = {
        writeValue: vi.fn((data: Uint8Array) => {
            if (writeFails) return Promise.reject(new Error('no'));
            written.push(new TextDecoder().decode(data));
            return Promise.resolve();
        }),
    };
    const gatt = {
        connected: true,
        connect: vi.fn().mockResolvedValue({
            getPrimaryService: vi.fn().mockResolvedValue({
                getCharacteristic: vi.fn((uuid: string) => Promise.resolve(uuid.endsWith('02') ? tx : rx)),
            }),
        }),
        disconnect: vi.fn(() => {
            gatt.connected = false;
        }),
    };
    const device = {
        gatt,
        addEventListener: (type: string, fn: (e: unknown) => void) => {
            listeners[type] = fn;
        },
    };

    vi.stubGlobal('navigator', {
        bluetooth: { requestDevice: vi.fn().mockResolvedValue(device) },
    });

    return {
        gatt,
        written,
        fire: (type: string, e: unknown) => listeners[type]?.(e),
        failWrites: () => {
            writeFails = true;
        },
    };
}

it('carries messages both ways over Bluetooth', async () => {
    const ble = fakeBluetooth();
    const h = handlers();

    const transport = await connectBluetooth(h);
    expect(transport.kind).toBe('bluetooth');
    expect(transport.isOpen()).toBe(true);

    transport.send({ type: 'arm' } as never);
    expect(ble.written).toEqual(['{"type":"arm"}']);

    ble.fire('characteristicvaluechanged', {
        target: { value: new TextEncoder().encode('{"type":"state","armed":false}') },
    });
    expect(h.onMessage).toHaveBeenCalledWith({ type: 'state', armed: false });
});

// A notification can arrive split across writes, which leaves the decoder
// holding half a message. The next one is complete, so a partial is dropped
// rather than reported.
it('drops a half-written Bluetooth notification instead of failing', async () => {
    const ble = fakeBluetooth();
    const h = handlers();
    await connectBluetooth(h);

    expect(() =>
        ble.fire('characteristicvaluechanged', {
            target: { value: new TextEncoder().encode('{"type":"sta') },
        }),
    ).not.toThrow();
    expect(h.onMessage).not.toHaveBeenCalled();
});

it('reports a Bluetooth write that did not land', async () => {
    const ble = fakeBluetooth();
    const h = handlers();
    const transport = await connectBluetooth(h);
    ble.failWrites();

    transport.send({ type: 'arm' } as never);
    await vi.waitFor(() => expect(h.onError).toHaveBeenCalledTimes(1));
});

it('tells the caller when the phone loses the device', async () => {
    const ble = fakeBluetooth();
    const h = handlers();
    const transport = await connectBluetooth(h);

    ble.fire('gattserverdisconnected', {});
    expect(h.onClose).toHaveBeenCalledTimes(1);

    transport.close();
    expect(ble.gatt.disconnect).toHaveBeenCalledTimes(1);
    expect(transport.isOpen()).toBe(false);
});
