import { beforeEach, expect, it, vi } from 'vitest';
import { clearSession, loadSession, saveSession } from '../src/lib/session';

const STORAGE_KEY = 'leavesafe_session_v1';
const FP = 'a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90';
const KEY = '1234567890123456';

interface FakeStorage {
    getItem(k: string): string | null;
    setItem(k: string, v: string): void;
    removeItem(k: string): void;
}

// stubBrowser stands in for the two pieces of the phone's browser this module
// touches: where the session is kept, and whether the page arrived over TLS.
function stubBrowser(options: { stored?: string | null; protocol?: string; refuseWrites?: boolean } = {}) {
    const entries = new Map<string, string>();
    if (options.stored != null) entries.set(STORAGE_KEY, options.stored);

    const localStorage: FakeStorage = {
        getItem: (k) => entries.get(k) ?? null,
        setItem: (k, v) => {
            // Private browsing refuses writes outright.
            if (options.refuseWrites) throw new DOMException('QuotaExceededError');
            entries.set(k, v);
        },
        removeItem: (k) => {
            if (options.refuseWrites) throw new DOMException('SecurityError');
            entries.delete(k);
        },
    };

    vi.stubGlobal('window', {
        localStorage,
        location: { protocol: options.protocol ?? 'http:' },
    });
    return entries;
}

beforeEach(() => {
    vi.unstubAllGlobals();
});

it('resumes a session that was stored with its fingerprint', () => {
    stubBrowser({ stored: JSON.stringify({ key: KEY, fingerprint: FP }), protocol: 'https:' });

    expect(loadSession()).toEqual({ key: KEY, fingerprint: FP });
});

it('has no session to resume when nothing was stored', () => {
    stubBrowser();

    expect(loadSession()).toBeNull();
});

// This is the point of the whole module's care. A page served over HTTPS came
// from a certificate, so there is always one to check. Resuming without a
// recorded fingerprint would hand the pairing key to whichever machine holds
// this address today.
it('refuses to resume over HTTPS when no fingerprint was recorded', () => {
    stubBrowser({ stored: JSON.stringify({ key: KEY, fingerprint: null }), protocol: 'https:' });

    expect(loadSession()).toBeNull();
});

// On the plain local path there genuinely is no certificate, and nothing is
// given up by carrying on.
it('resumes without a fingerprint over plain HTTP', () => {
    stubBrowser({ stored: JSON.stringify({ key: KEY, fingerprint: null }), protocol: 'http:' });

    expect(loadSession()).toEqual({ key: KEY, fingerprint: null });
});

// A damaged or hand-edited fingerprint used to normalise to empty downstream,
// which reads as "there was no certificate to check" — so the key would go out
// to whatever answered, silently, on every visit from then on.
it('rejects a fingerprint that is not a full SHA-256', () => {
    for (const bad of ['a1b2c3', `${FP}ff`, '', 'zzzz', 123, null]) {
        stubBrowser({ stored: JSON.stringify({ key: KEY, fingerprint: bad }), protocol: 'https:' });
        expect(loadSession(), `fingerprint ${JSON.stringify(bad)} was accepted`).toBeNull();
    }
});

it('accepts a stored fingerprint written with separators', () => {
    const separated = (FP.match(/.{2}/g) ?? []).join(':');
    stubBrowser({ stored: JSON.stringify({ key: KEY, fingerprint: separated }), protocol: 'https:' });

    expect(loadSession()).toEqual({ key: KEY, fingerprint: separated });
});

it('has no session when the stored entry carries no key', () => {
    for (const bad of [{ fingerprint: FP }, { key: '', fingerprint: FP }, { key: 42, fingerprint: FP }]) {
        stubBrowser({ stored: JSON.stringify(bad), protocol: 'http:' });
        expect(loadSession(), `entry ${JSON.stringify(bad)} was accepted`).toBeNull();
    }
});

// A corrupt entry means pairing by hand, which is where a phone that never had
// one starts anyway.
it('falls back to pairing by hand when the stored entry is not JSON', () => {
    stubBrowser({ stored: 'not json at all' });

    expect(loadSession()).toBeNull();
});

it('stores the key and fingerprint together', () => {
    const entries = stubBrowser();

    saveSession(KEY, FP);

    expect(JSON.parse(entries.get(STORAGE_KEY) ?? '{}')).toEqual({ key: KEY, fingerprint: FP });
});

it('forgets the phone when the session is cleared', () => {
    const entries = stubBrowser({ stored: JSON.stringify({ key: KEY, fingerprint: FP }) });

    clearSession();

    expect(entries.has(STORAGE_KEY)).toBe(false);
});

// A storage that refuses writes must not take the page down with it. The
// session still works until the page goes away, which is the behaviour this
// module exists to improve on rather than depend on.
it('survives a browser that refuses to store anything', () => {
    stubBrowser({ refuseWrites: true });

    expect(() => saveSession(KEY, FP)).not.toThrow();
    expect(() => clearSession()).not.toThrow();
});
