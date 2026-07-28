import { computed, signal } from '@preact/signals';
import type { AlertLevel, AppConfig, ClientMessage, LocationPayload, SensorInfo } from './protocol';
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

export const activeSensorCount = computed(() => sensors.value.filter((s) => s.enabled && s.available).length);

export const visibleLog = computed(() => {
    const entries =
        logFilter.value === 'all' ? log.value : log.value.filter((e) => e.level === logFilter.value);
    return logNewestFirst.value ? entries : [...entries].reverse();
});

// ---- transport ---------------------------------------------------------

let transport: Transport | null = null;
let token: string | null = null;

export function setTransport(t: Transport | null) {
    transport = t;
}

export function setToken(value: string | null) {
    token = value;
}

export function hasToken(): boolean {
    return token !== null;
}

export function send(msg: ClientMessage) {
    if (!transport) return;
    transport.send(token ? { ...msg, token } : msg);
}

export function closeTransport() {
    transport?.close();
    transport = null;
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
