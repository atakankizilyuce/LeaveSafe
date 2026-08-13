import { beforeEach, expect, it, vi } from 'vitest';
import { clearSession, loadSession, saveSession } from '../src/lib/session';

const STORAGE_KEY = 'leavesafe_session_v1';
const KEY = '1234567890123456';

interface FakeStorage {
    getItem(k: string): string | null;
    setItem(k: string, v: string): void;
    removeItem(k: string): void;
}

// stubBrowser stands in for the piece of the phone's browser this module
// touches: where the session is kept.
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

it('resumes a session that was stored', () => {
    stubBrowser({ stored: JSON.stringify({ key: KEY }) });

    expect(loadSession()).toEqual({ key: KEY });
});

it('has no session to resume when nothing was stored', () => {
    stubBrowser();

    expect(loadSession()).toBeNull();
});

// A stored entry written by an older build carries a fingerprint alongside the
// key. It is ignored rather than refused: the key in it is still this laptop's,
// and making the user rescan would be a cost with nothing bought.
it('ignores a fingerprint left behind by an older build', () => {
    stubBrowser({ stored: JSON.stringify({ key: KEY, fingerprint: 'a1b2c3' }) });

    expect(loadSession()).toEqual({ key: KEY });
});

it('has no session when the stored entry carries no key', () => {
    for (const bad of [{}, { key: '' }, { key: 42 }]) {
        stubBrowser({ stored: JSON.stringify(bad) });
        expect(loadSession(), `entry ${JSON.stringify(bad)} was accepted`).toBeNull();
    }
});

// A corrupt entry means pairing by hand, which is where a phone that never had
// one starts anyway.
it('falls back to pairing by hand when the stored entry is not JSON', () => {
    stubBrowser({ stored: 'not json at all' });

    expect(loadSession()).toBeNull();
});

it('stores the key', () => {
    const entries = stubBrowser();

    saveSession(KEY);

    expect(JSON.parse(entries.get(STORAGE_KEY) ?? '{}')).toEqual({ key: KEY });
});

it('forgets the phone when the session is cleared', () => {
    const entries = stubBrowser({ stored: JSON.stringify({ key: KEY }) });

    clearSession();

    expect(entries.has(STORAGE_KEY)).toBe(false);
});

// A storage that refuses writes must not take the page down with it. The
// session still works until the page goes away, which is the behaviour this
// module exists to improve on rather than depend on.
it('survives a browser that refuses to store anything', () => {
    stubBrowser({ refuseWrites: true });

    expect(() => saveSession(KEY)).not.toThrow();
    expect(() => clearSession()).not.toThrow();
});
