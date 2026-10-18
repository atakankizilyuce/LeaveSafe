import { intlLocale, number, t } from './i18n';
/** Human-readable distance. Metres below a kilometre, then kilometres. */
export function distance(meters: number): string {
    if (!Number.isFinite(meters)) return '—';
    if (meters < 1000) return t('{distance} m', { distance: number(Math.round(meters)) });
    return t('{distance} km', {
        distance: number(meters / 1000, {
            minimumFractionDigits: meters < 10000 ? 1 : 0,
            maximumFractionDigits: meters < 10000 ? 1 : 0,
        }),
    });
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
    if (secs < 10) return t('just now');
    if (secs < 60) return t('{count}s', { count: secs });
    if (secs < 3600) return t('{count}m', { count: Math.floor(secs / 60) });
    const hours = Math.floor(secs / 3600);
    if (hours < 24)
        return t('{count}h {minutes}m', { count: hours, minutes: Math.floor((secs % 3600) / 60) });
    return t('{count}d {hours}h', { count: Math.floor(hours / 24), hours: hours % 24 });
}

/** Wall clock time for the log. */
export function clock(ms: number): string {
    return new Date(ms).toLocaleTimeString(intlLocale(), {
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
