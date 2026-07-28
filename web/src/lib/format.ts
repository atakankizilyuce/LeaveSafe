/** Human-readable distance. Metres below a kilometre, then kilometres. */
export function distance(meters: number): string {
    if (!Number.isFinite(meters)) return '—';
    if (meters < 1000) return `${Math.round(meters)} m`;
    return `${(meters / 1000).toFixed(meters < 10000 ? 1 : 0)} km`;
}

/** How long ago, from a unix timestamp in seconds. */
export function ago(unixSeconds: number | undefined): string {
    if (!unixSeconds) return '—';
    return elapsed(Date.now() - unixSeconds * 1000);
}

/** How long ago, from a millisecond timestamp. */
export function since(ms: number | null): string {
    if (ms === null) return '—';
    return elapsed(Date.now() - ms);
}

function elapsed(deltaMs: number): string {
    const secs = Math.max(0, Math.floor(deltaMs / 1000));
    if (secs < 10) return 'just now';
    if (secs < 60) return `${secs}s`;
    if (secs < 3600) return `${Math.floor(secs / 60)}m`;
    const hours = Math.floor(secs / 3600);
    if (hours < 24) return `${hours}h ${Math.floor((secs % 3600) / 60)}m`;
    return `${Math.floor(hours / 24)}d ${hours % 24}h`;
}

/** Wall clock time for the log. */
export function clock(ms: number): string {
    return new Date(ms).toLocaleTimeString([], {
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
        hour12: false,
    });
}

/** Group a pairing key into XXXX-XXXX-XXXX-XXXX as it is typed. */
export function groupKey(raw: string): string {
    const digits = raw.replace(/\D/g, '').slice(0, 16);
    return digits.replace(/(.{4})(?=.)/g, '$1-');
}
