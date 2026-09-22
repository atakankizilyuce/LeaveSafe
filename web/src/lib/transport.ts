import type { ClientMessage, ServerMessage } from './protocol';

// One interface over the way a phone reaches the laptop. It covered two once —
// the WebSocket and a Bluetooth link — and `send` branched on a module-level
// global before this boundary existed. Bluetooth is gone; the boundary is kept,
// because what it costs is a type alias and what it buys is a UI that never
// learned which transport it was talking over.

export type TransportKind = 'websocket';

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

function deadTransport(kind: TransportKind): Transport {
    return {
        kind,
        isOpen: () => false,
        send() {},
        close() {},
    };
}
