import { computed, signal } from '@preact/signals';
import type {
    AlertLevel,
    AppConfig,
    ClientMessage,
    LocationPayload,
    SensorInfo,
    UpdatePayload,
} from './protocol';
import type { Transport } from './transport';

export type Screen = 'pair' | 'panel';
export type Link = 'live' | 'checking' | 'lost';

export interface LogEntry {
    id: number;
    message: string;
    level: AlertLevel;
    sensor: string;
    at: number;
}

export const screen = signal<Screen>('pair');
export const link = signal<Link>('live');
export const armed = signal(false);
export const serverVersion = signal<string | null>(null);
export const pairError = signal<string | null>(null);
export const pairing = signal(false);

export const sensors = signal<SensorInfo[]>([]);
export const config = signal<AppConfig | null>(null);
export const location = signal<LocationPayload | null>(null);

/**
 * A newer release, once the laptop has found one. Null is the normal state: no
 * check has reported yet, or this copy is current.
 *
 * Nothing about this is urgent, so it never interrupts. It marks the footer and
 * fills in a section of the settings sheet, and the alarm keeps the whole screen
 * to itself.
 */
export const updateAvailable = signal<UpdatePayload | null>(null);

/** Sensors currently lit on the annunciator panel, with when they tripped. */
export const tripped = signal<Record<string, number>>({});

export const alarm = signal<{ message: string; sensor: string } | null>(null);
export const pinPrompt = signal(false);
export const settingsOpen = signal(false);
export const toast = signal<string | null>(null);

export const log = signal<LogEntry[]>([]);
export const logFilter = signal<'all' | AlertLevel>('all');
export const logNewestFirst = signal(true);

export const armedSince = signal<number | null>(null);

// A sensor whose driver has failed is not active, however it is configured.
// This is the number the user reads before walking away from the laptop.
export const activeSensorCount = computed(
    () => sensors.value.filter((s) => s.enabled && s.available && !s.failure).length,
);

export const visibleLog = computed(() => {
    const entries =
        logFilter.value === 'all' ? log.value : log.value.filter((e) => e.level === logFilter.value);
    return logNewestFirst.value ? entries : [...entries].reverse();
});

// ---- transport ---------------------------------------------------------

let transport: Transport | null = null;
let token: string | null = null;

/**
 * Whether the socket currently registered has proved it is the laptop.
 *
 * Inbound messages were already held to this, and outbound ones were not, which
 * left the check guarding the wrong direction. The phone reconnects to
 * `location.host` every three seconds, forever, so anything that can answer at
 * that address once the laptop has gone quiet — the sleeping-or-stolen laptop
 * this product is about, or a machine that took its address on the local
 * network — was handed the ping (with the session token on it), the reply to
 * "check the connection" (which includes the phone's own precise position), and
 * the digits typed into a disarm dialog that outlived the socket that raised it.
 *
 * None of that needed the pairing key, and none of it produced anything on
 * screen to suggest it had happened.
 */
let verified = false;

export function setTransport(t: Transport | null) {
    transport = t;
    // A new socket has proved nothing, including one that replaces a socket
    // that had. Reconnecting is exactly when something else can answer at the
    // same address.
    verified = false;
}

/**
 * Records that this connection answered the pairing key this page sent. Called
 * from the one place that can know it: the `auth_ok` branch, behind the check
 * that a key actually went out on this connection.
 */
export function markVerified() {
    verified = true;
}

export function setToken(value: string | null) {
    token = value;
}

export function hasToken(): boolean {
    return token !== null;
}

export function send(msg: ClientMessage) {
    if (!transport) return;
    // The pairing key is the one thing that necessarily goes out before the
    // connection has proved anything — proving it is what the key is for.
    if (!verified && msg.type !== 'auth') return;
    transport.send(token ? { ...msg, token } : msg);
}

export function closeTransport() {
    transport?.close();
    transport = null;
    verified = false;
}

// ---- log persistence ---------------------------------------------------

const LOG_KEY = 'leavesafe_alerts';
const LOG_LIMIT = 200;
let nextId = 1;

export function loadLog() {
    try {
        const raw = window.localStorage.getItem(LOG_KEY);
        if (!raw) return;
        const parsed: unknown = JSON.parse(raw);
        if (!Array.isArray(parsed)) return;
        log.value = parsed.slice(0, LOG_LIMIT).map((entry) => {
            const e = entry as Partial<LogEntry>;
            return {
                id: nextId++,
                message: String(e.message ?? ''),
                level: e.level === 'critical' ? 'critical' : 'warning',
                sensor: String(e.sensor ?? ''),
                at: Number(e.at) || Date.now(),
            };
        });
    } catch {
        // A corrupt history is not worth blocking the panel over.
    }
}

export function appendLog(entry: Omit<LogEntry, 'id'>) {
    const next = [{ ...entry, id: nextId++ }, ...log.value].slice(0, LOG_LIMIT);
    log.value = next;
    persistLog(next);
}

export function clearLog() {
    log.value = [];
    persistLog([]);
}

function persistLog(entries: LogEntry[]) {
    try {
        window.localStorage.setItem(
            LOG_KEY,
            JSON.stringify(entries.map(({ message, level, sensor, at }) => ({ message, level, sensor, at }))),
        );
    } catch {
        // Private browsing refuses writes. The panel still works in memory.
    }
}

export function showToast(message: string) {
    toast.value = message;
    window.setTimeout(() => {
        if (toast.value === message) toast.value = null;
    }, 2600);
}

/**
 * Flip a sensor's switch locally, the moment the user asks for it.
 *
 * The laptop broadcasts its own status straight after it handles the change, so
 * this only ever stands in for the round trip — and is corrected by it if the
 * laptop refuses. It exists because the tile is now the switch: one that does
 * not move until the network answers reads as a tap that missed, and the answer
 * to that is to tap it again, which puts the sensor back where it started.
 */
export function setSensorEnabled(name: string, enabled: boolean) {
    sensors.value = sensors.value.map((s) => (s.name === name ? { ...s, enabled } : s));
}

/** Light an annunciator caption and let it decay after a while. */
export function tripSensor(name: string) {
    if (!name) return;
    tripped.value = { ...tripped.value, [name]: Date.now() };
    window.setTimeout(() => {
        const next = { ...tripped.value };
        delete next[name];
        tripped.value = next;
    }, 20000);
}
