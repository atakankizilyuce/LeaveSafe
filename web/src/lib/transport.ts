import type { ClientMessage, ServerMessage } from './protocol';

// One interface over the two ways a phone can reach the laptop. In the previous
// build these were interleaved through the whole app and `send` branched on a
// module-level global; behind this boundary the UI stops caring which is live.

export type TransportKind = 'websocket' | 'bluetooth';

export interface TransportHandlers {
    onMessage(msg: ServerMessage): void;
    onOpen(): void;
    onClose(): void;
    onError(reason: string): void;
}

export interface Transport {
    readonly kind: TransportKind;
    send(msg: ClientMessage): void;
    close(): void;
    isOpen(): boolean;
}

const BLE_SERVICE_UUID = '4c454156-4553-4146-452d-424c45000001';
const BLE_TX_UUID = '4c454156-4553-4146-452d-424c45000002';
const BLE_RX_UUID = '4c454156-4553-4146-452d-424c45000003';

const CONNECT_TIMEOUT_MS = 8000;

export function connectWebSocket(handlers: TransportHandlers): Transport {
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    const url = `${proto}//${location.host}/ws`;

    let socket: WebSocket;
    try {
        socket = new WebSocket(url);
    } catch {
        handlers.onError('Could not open a connection. Check that you are on the same network.');
        return deadTransport('websocket');
    }

    const timeout = window.setTimeout(() => {
        if (socket.readyState !== WebSocket.OPEN) {
            socket.close();
            handlers.onError('Connection timed out. Check that you are on the same network.');
        }
    }, CONNECT_TIMEOUT_MS);

    socket.onopen = () => {
        window.clearTimeout(timeout);
        handlers.onOpen();
    };
    socket.onmessage = (event) => {
        let msg: ServerMessage;
        try {
            msg = JSON.parse(event.data as string) as ServerMessage;
        } catch {
            return;
        }
        handlers.onMessage(msg);
    };
    socket.onclose = () => {
        window.clearTimeout(timeout);
        handlers.onClose();
    };
    socket.onerror = () => {
        window.clearTimeout(timeout);
        handlers.onError('Connection error.');
    };

    return {
        kind: 'websocket',
        isOpen: () => socket.readyState === WebSocket.OPEN,
        send(msg) {
            if (socket.readyState === WebSocket.OPEN) {
                socket.send(JSON.stringify(msg));
            }
        },
        close() {
            socket.close();
        },
    };
}

export function bluetoothSupported(): boolean {
    return 'bluetooth' in navigator;
}

export async function connectBluetooth(handlers: TransportHandlers): Promise<Transport> {
    const bluetooth = (navigator as Navigator & { bluetooth?: BluetoothLike }).bluetooth;
    if (!bluetooth) {
        throw new Error('This browser has no Bluetooth support. Chrome and Edge do.');
    }

    const device = await bluetooth.requestDevice({ filters: [{ services: [BLE_SERVICE_UUID] }] });
    device.addEventListener('gattserverdisconnected', () => handlers.onClose());

    const server = await device.gatt.connect();
    const service = await server.getPrimaryService(BLE_SERVICE_UUID);
    const [tx, rx] = await Promise.all([
        service.getCharacteristic(BLE_TX_UUID),
        service.getCharacteristic(BLE_RX_UUID),
    ]);

    await tx.startNotifications();
    tx.addEventListener('characteristicvaluechanged', (event: Event) => {
        const target = event.target as unknown as { value: DataView };
        try {
            handlers.onMessage(JSON.parse(new TextDecoder().decode(target.value)) as ServerMessage);
        } catch {
            // A partial write is not worth surfacing; the next notification wins.
        }
    });

    // The device reference is held in the closure below on purpose: dropping it
    // lets the browser collect the device mid-session and silently kill the link.
    //
    // onOpen is deliberately not called here. The caller has to register this
    // transport before anything is sent over it, and calling onOpen from inside
    // would fire the auth message while the transport is still unregistered —
    // so the key would be written to nothing and pairing would hang.
    return {
        kind: 'bluetooth',
        isOpen: () => device.gatt.connected,
        send(msg) {
            rx.writeValue(new TextEncoder().encode(JSON.stringify(msg))).catch(() => {
                handlers.onError('Bluetooth write failed.');
            });
        },
        close() {
            device.gatt.disconnect();
        },
    };
}

function deadTransport(kind: TransportKind): Transport {
    return {
        kind,
        isOpen: () => false,
        send() {},
        close() {},
    };
}

// Minimal shape of the Web Bluetooth API. The full lib.dom types for it are
// still behind a flag in TypeScript, and only these members are used.
interface BluetoothLike {
    requestDevice(options: { filters: Array<{ services: string[] }> }): Promise<BluetoothDeviceLike>;
}

interface BluetoothDeviceLike {
    gatt: {
        connected: boolean;
        connect(): Promise<BluetoothServerLike>;
        disconnect(): void;
    };
    addEventListener(type: string, listener: () => void): void;
}

interface BluetoothServerLike {
    getPrimaryService(uuid: string): Promise<BluetoothServiceLike>;
}

interface BluetoothServiceLike {
    getCharacteristic(uuid: string): Promise<BluetoothCharacteristicLike>;
}

interface BluetoothCharacteristicLike {
    startNotifications(): Promise<unknown>;
    writeValue(value: BufferSource): Promise<void>;
    addEventListener(type: string, listener: (event: Event) => void): void;
}
